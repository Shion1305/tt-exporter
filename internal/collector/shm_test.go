package collector

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildRegion constructs a synthetic DeviceMemoryRegion matching the #37218
// byte layout, with one populated chip slot and one populated process slot.
func buildRegion(version uint32, refCount uint32, asicID uint64, deviceID uint32, dram uint64) []byte {
	buf := make([]byte, shmRegionSize)
	binary.LittleEndian.PutUint32(buf[offVersion:], version)
	binary.LittleEndian.PutUint32(buf[offNumActiveProcs:], 1)
	binary.LittleEndian.PutUint64(buf[offLastUpdate:], 1_700_000_000_000_000_000)
	binary.LittleEndian.PutUint32(buf[offRefCount:], refCount)
	binary.LittleEndian.PutUint64(buf[offASICID:], asicID)
	binary.LittleEndian.PutUint32(buf[offDeviceID:], deviceID)
	binary.LittleEndian.PutUint64(buf[offTotalDRAM:], dram)
	binary.LittleEndian.PutUint64(buf[offTotalL1:], 64<<20)

	// All chip slots start as the CHIP_STATS_UNUSED sentinel (as initialize_region does).
	for i := 0; i < shmMaxChips; i++ {
		binary.LittleEndian.PutUint32(buf[shmChipBase+i*shmChipStride:], shmChipUnused)
	}
	// chip_stats[0]: local gateway chip == deviceID.
	c0 := shmChipBase
	binary.LittleEndian.PutUint32(buf[c0:], deviceID)
	binary.LittleEndian.PutUint32(buf[c0+4:], 0) // is_remote=false
	binary.LittleEndian.PutUint64(buf[c0+8:], dram)

	// processes[0]: pid 4242, name "vllm-worker".
	p0 := shmProcBase
	binary.LittleEndian.PutUint32(buf[p0:], 4242)
	binary.LittleEndian.PutUint64(buf[p0+8:], dram)
	copy(buf[p0+56:p0+56+64], "vllm-worker\x00")

	return buf
}

func TestParseSHMRegion(t *testing.T) {
	const asic = uint64(0x806632283760)
	buf := buildRegion(3, 1, asic, 1, 6<<30)

	r, err := parseSHMRegion(buf)
	if err != nil {
		t.Fatalf("parseSHMRegion: %v", err)
	}
	if r.Version != 3 {
		t.Errorf("version = %d, want 3", r.Version)
	}
	if r.ReferenceCount != 1 {
		t.Errorf("reference_count = %d, want 1", r.ReferenceCount)
	}
	if r.ASICID != asic {
		t.Errorf("asic_id = %#x, want %#x", r.ASICID, asic)
	}
	if r.MetalDeviceID != 1 {
		t.Errorf("metal_device_id = %d, want 1", r.MetalDeviceID)
	}
	if r.DRAMUsed != 6<<30 {
		t.Errorf("dram_used = %d, want %d", r.DRAMUsed, 6<<30)
	}
	if r.L1Used != 64<<20 {
		t.Errorf("l1_used = %d, want %d", r.L1Used, 64<<20)
	}
	if len(r.Chips) != 1 {
		t.Fatalf("chips = %d, want 1 (sentinel slot must be skipped)", len(r.Chips))
	}
	if r.Chips[0].ChipID != 1 || r.Chips[0].IsRemote || r.Chips[0].DRAMUsed != 6<<30 {
		t.Errorf("chip[0] = %+v", r.Chips[0])
	}
	if len(r.Procs) != 1 {
		t.Fatalf("procs = %d, want 1", len(r.Procs))
	}
	if r.Procs[0].PID != 4242 || r.Procs[0].Name != "vllm-worker" || r.Procs[0].DRAMUsed != 6<<30 {
		t.Errorf("proc[0] = %+v", r.Procs[0])
	}
}

func TestParseSHMRegionRejectsShort(t *testing.T) {
	if _, err := parseSHMRegion(make([]byte, shmRegionSize-1)); err == nil {
		t.Fatal("expected error for short region")
	}
}

func TestParseSHMRegionRejectsUnknownVersion(t *testing.T) {
	buf := buildRegion(99, 1, 0x1234, 0, 1<<30)
	if _, err := parseSHMRegion(buf); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestParseSHMRegionAcceptsV2(t *testing.T) {
	buf := buildRegion(2, 1, 0x1234, 0, 1<<30)
	if _, err := parseSHMRegion(buf); err != nil {
		t.Fatalf("v2 should parse with the same layout: %v", err)
	}
}

func TestCollectSHMRegions(t *testing.T) {
	dir := t.TempDir()
	// Good file.
	if err := os.WriteFile(filepath.Join(dir, "tt_device_141176416515936_memory"),
		buildRegion(3, 1, 0x806632283760, 1, 6<<30), 0o600); err != nil {
		t.Fatal(err)
	}
	// Bad version (skipped, tallied).
	if err := os.WriteFile(filepath.Join(dir, "tt_device_2_memory"),
		buildRegion(99, 1, 2, 0, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	// Too short (skipped, tallied).
	if err := os.WriteFile(filepath.Join(dir, "tt_device_3_memory"),
		make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-matching name (ignored entirely).
	if err := os.WriteFile(filepath.Join(dir, "unrelated"), buildRegion(3, 1, 9, 9, 0), 0o600); err != nil {
		t.Fatal(err)
	}

	res := collectSHMRegions(dir)
	if len(res.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(res.Regions))
	}
	if res.Regions[0].ASICID != 0x806632283760 {
		t.Errorf("asic_id = %#x", res.Regions[0].ASICID)
	}
	if res.ErrReasons["unknown_version"] != 1 {
		t.Errorf("unknown_version errors = %d, want 1", res.ErrReasons["unknown_version"])
	}
	if res.ErrReasons["short_file"] != 1 {
		t.Errorf("short_file errors = %d, want 1", res.ErrReasons["short_file"])
	}
}

func TestCStringTruncatesAtNUL(t *testing.T) {
	b := make([]byte, 64)
	copy(b, "python3\x00garbage")
	if got := cString(b); got != "python3" {
		t.Errorf("cString = %q, want python3", got)
	}
}
