package collector

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Shion1305/tt-exporter/internal/model"
)

// Byte layout of tt-metal's DeviceMemoryRegion (PR #37218),
// `struct ... __attribute__((aligned(64)))`, little-endian, x86-64.
// Offsets verified against memory_stats_shm.hpp and a real 8576-byte file.
// See tt-metrics-dev: tt-exporter-shm-integration-requirements.md §2.4/§5.2.
const (
	shmRegionSize = 8576       // sizeof(DeviceMemoryRegion)
	shmMaxChips   = 16         // MAX_CHIPS_PER_DEVICE
	shmMaxProcs   = 64         // MAX_PROCESSES
	shmChipBase   = 88         // offsetof(chip_stats)
	shmChipStride = 48         // sizeof(ChipStats)
	shmProcBase   = 856        // offsetof(processes)
	shmProcStride = 120        // sizeof(ProcessStats)
	shmChipUnused = 0xFFFFFFFF // CHIP_STATS_UNUSED sentinel

	offVersion        = 0
	offNumActiveProcs = 4
	offLastUpdate     = 8
	offRefCount       = 16
	offASICID         = 32
	offDeviceID       = 40
	offTotalDRAM      = 48
	offTotalL1        = 56
	offTotalL1Small   = 64
	offTotalTrace     = 72
	offTotalCB        = 80
)

// shmGlobPattern matches the per-device SHM files tt-metal #37218 writes.
const shmGlobPattern = "tt_device_*_memory"

var errShortRegion = errors.New("shm region shorter than 8576 bytes")

// shmVersionSupported reports whether a DeviceMemoryRegion version uses the
// layout this parser understands. v2 and v3 share the same field offsets
// (std::atomic<T> has the same size/alignment as T on x86-64); only atomicity
// changed. Any other version may have moved fields, so it must not be parsed
// with these offsets.
func shmVersionSupported(v uint32) bool { return v == 2 || v == 3 }

// parseSHMRegion decodes a DeviceMemoryRegion from a raw byte snapshot. It
// validates length and version *before* trusting any offset.
func parseSHMRegion(buf []byte) (model.SHMRegion, error) {
	var r model.SHMRegion
	if len(buf) < shmRegionSize {
		return r, errShortRegion
	}
	r.Version = binary.LittleEndian.Uint32(buf[offVersion:])
	if !shmVersionSupported(r.Version) {
		return r, fmt.Errorf("unsupported shm version %d", r.Version)
	}
	r.NumActiveProcs = binary.LittleEndian.Uint32(buf[offNumActiveProcs:])
	r.LastUpdateNs = binary.LittleEndian.Uint64(buf[offLastUpdate:])
	r.ReferenceCount = binary.LittleEndian.Uint32(buf[offRefCount:])
	r.ASICID = binary.LittleEndian.Uint64(buf[offASICID:])
	r.MetalDeviceID = binary.LittleEndian.Uint32(buf[offDeviceID:])
	r.DRAMUsed = binary.LittleEndian.Uint64(buf[offTotalDRAM:])
	r.L1Used = binary.LittleEndian.Uint64(buf[offTotalL1:])
	r.L1SmallUsed = binary.LittleEndian.Uint64(buf[offTotalL1Small:])
	r.TraceUsed = binary.LittleEndian.Uint64(buf[offTotalTrace:])
	r.CBUsed = binary.LittleEndian.Uint64(buf[offTotalCB:])

	for i := 0; i < shmMaxChips; i++ {
		b := shmChipBase + i*shmChipStride
		chipID := binary.LittleEndian.Uint32(buf[b:])
		if chipID == shmChipUnused {
			continue
		}
		r.Chips = append(r.Chips, model.SHMChip{
			ChipID:      chipID,
			IsRemote:    binary.LittleEndian.Uint32(buf[b+4:]) != 0,
			DRAMUsed:    binary.LittleEndian.Uint64(buf[b+8:]),
			L1Used:      binary.LittleEndian.Uint64(buf[b+16:]),
			L1SmallUsed: binary.LittleEndian.Uint64(buf[b+24:]),
			TraceUsed:   binary.LittleEndian.Uint64(buf[b+32:]),
			CBUsed:      binary.LittleEndian.Uint64(buf[b+40:]),
		})
	}

	for i := 0; i < shmMaxProcs; i++ {
		b := shmProcBase + i*shmProcStride
		pid := int32(binary.LittleEndian.Uint32(buf[b:]))
		if pid <= 0 { // 0 = unused slot
			continue
		}
		r.Procs = append(r.Procs, model.SHMProc{
			PID:          pid,
			DRAMUsed:     binary.LittleEndian.Uint64(buf[b+8:]),
			L1Used:       binary.LittleEndian.Uint64(buf[b+16:]),
			L1SmallUsed:  binary.LittleEndian.Uint64(buf[b+24:]),
			TraceUsed:    binary.LittleEndian.Uint64(buf[b+32:]),
			CBUsed:       binary.LittleEndian.Uint64(buf[b+40:]),
			LastUpdateNs: binary.LittleEndian.Uint64(buf[b+48:]),
			Name:         cString(buf[b+56 : b+56+64]),
		})
	}
	return r, nil
}

// cString returns the NUL-terminated string from a fixed-size buffer, sanitised
// to valid UTF-8 so it is safe as a Prometheus label value.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.ToValidUTF8(string(b), "")
}

// readSHMRegion reads and parses one SHM file with a plain read() — never mmap.
// mmap of a file that the producer created with O_CREAT|O_EXCL and only later
// ftruncate()s can fault with SIGBUS (uncatchable in Go) inside that window; a
// read() of a short file simply returns a short-read error we handle.
func readSHMRegion(path string) (model.SHMRegion, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return model.SHMRegion{}, err
	}
	defer f.Close()

	buf := make([]byte, shmRegionSize)
	if _, err := io.ReadFull(f, buf); err != nil {
		return model.SHMRegion{}, errShortRegion
	}
	r, err := parseSHMRegion(buf)
	if err != nil {
		return model.SHMRegion{}, err
	}
	r.Path = path
	return r, nil
}

// shmCollectResult is the outcome of one scrape of the SHM directory.
type shmCollectResult struct {
	Regions    []model.SHMRegion
	ErrReasons map[string]int // reason -> count seen this scrape
}

// collectSHMRegions globs dir for tt-metal SHM files and parses each one
// independently. A failure on any single file is logged (deduplicated) and
// tallied, but never suppresses the others.
func collectSHMRegions(dir string) shmCollectResult {
	res := shmCollectResult{ErrReasons: map[string]int{}}
	matches, _ := filepath.Glob(filepath.Join(dir, shmGlobPattern))
	for _, path := range matches {
		r, err := readSHMRegion(path)
		if err != nil {
			reason := shmErrReason(err)
			res.ErrReasons[reason]++
			logSHMError(path, reason, err)
			continue
		}
		res.Regions = append(res.Regions, r)
	}
	return res
}

func shmErrReason(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "eacces"
	case errors.Is(err, errShortRegion):
		return "short_file"
	case strings.Contains(err.Error(), "unsupported shm version"):
		return "unknown_version"
	default:
		return "other"
	}
}

var (
	shmLogMu   sync.Mutex
	shmLogSeen = map[string]string{} // path -> last reason logged
)

// logSHMError logs at most once per (path, reason) until the reason changes, so
// a persistently unreadable file does not spam the log every scrape.
func logSHMError(path, reason string, err error) {
	shmLogMu.Lock()
	defer shmLogMu.Unlock()
	if shmLogSeen[path] == reason {
		return
	}
	shmLogSeen[path] = reason
	switch reason {
	case "eacces":
		log.Printf("tt-exporter: cannot read SHM %s: %v — this 0600 file is owned by the workload's uid; run the exporter as root (--privileged) or a matching uid, and ensure host /dev/shm is shared (see README)", path, err)
	case "unknown_version":
		log.Printf("tt-exporter: skipping SHM %s: %v — a stale file from an older tt-metal image may be shadowing a live workload's tracking; if no workload uses it, 'rm %s'", path, err, path)
	default:
		log.Printf("tt-exporter: skipping SHM %s: %v", path, err)
	}
}

// shmDir is the directory scanned for SHM files (override with TT_EXPORTER_SHM_DIR;
// useful for tests and non-default mounts). The producer always writes to /dev/shm.
func shmDir() string {
	if d := strings.TrimSpace(os.Getenv("TT_EXPORTER_SHM_DIR")); d != "" {
		return d
	}
	return "/dev/shm"
}

// shmPerPIDEnabled reports whether per-PID memory series should be exported.
// Off by default: PIDs are in the workload container's namespace (not the
// host's) and the series add cardinality, so they are opt-in for debugging.
func shmPerPIDEnabled() bool {
	v := strings.TrimSpace(os.Getenv("TT_EXPORTER_SHM_PER_PID"))
	return v == "1" || strings.EqualFold(v, "true")
}
