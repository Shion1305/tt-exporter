package collector

import (
	"strconv"
	"strings"

	"github.com/Shion1305/tt-exporter/internal/model"
)

// mangleASICID reproduces UMD's chip_unique_id for a single-ASIC PCIe Blackhole
// gateway chip:
//
//	mangle_asic_id(board_id, asic_location) = (board_id << 5) | (asic_location & 0x1F)
//
// This equals the #37218 SHM file's asic_id field (and its decimal filename
// stem) on Blackhole non-6u parts whose Ethernet cores are trained. Verified on
// hardware: board_id 0x403319141bb, asic_location 0 -> 0x806632283760
// (= /dev/shm/tt_device_141176416515936_memory). See the requirements doc
// (tt-metrics-dev: tt-exporter-shm-integration-requirements.md, I-1/I-3/I-6).
func mangleASICID(boardID uint64, asicLocation uint32) uint64 {
	return (boardID << 5) | uint64(asicLocation&0x1F)
}

// asicToBusID maps candidate chip_unique_id values to PCI BusID, derived purely
// from `tt-smi -s` (no device is opened). For each device it registers BOTH the
// mangled candidate (trained-ETH path) and the raw ASIC_ID candidate
// (unconnected/ETH-untrained path), so a SHM file joins regardless of which the
// runtime produced. The authoritative key remains the SHM file's own asic_id;
// this table only resolves it to a BusID when a match exists.
func asicToBusID(snap model.TTSmiOutput) map[uint64]string {
	m := make(map[uint64]string)
	ambiguous := make(map[uint64]struct{}) // keys claimed by >1 distinct BusID
	// register maps a candidate chip id to a BusID. If two devices ever claim
	// the same id with different BusIDs (e.g. a multi-ASIC board where
	// asic_location is missing and both mangle to (board_id<<5)|0), the id is
	// poisoned and left unresolved rather than silently misattributed.
	register := func(key uint64, bus string) {
		if _, bad := ambiguous[key]; bad {
			return
		}
		if existing, ok := m[key]; ok && existing != bus {
			delete(m, key)
			ambiguous[key] = struct{}{}
			return
		}
		m[key] = bus
	}
	for _, d := range snap.DeviceInfo {
		bus := strings.TrimSpace(d.BoardInfo.BusID)
		if bus == "" {
			continue
		}
		if boardID, ok := boardIDOf(d); ok {
			var loc uint64
			if l, ok := parseHexOrDec(d.SmbusTelem.ASICLocation); ok {
				loc = l
			}
			register(mangleASICID(boardID, uint32(loc)), bus)
		}
		if raw, ok := rawASICID(d); ok {
			register(raw, bus)
		}
	}
	return m
}

// boardIDOf returns the 64-bit board_id, preferring the explicit
// BOARD_ID_HIGH/LOW SMBus fields and falling back to board_info.board_id, which
// tt-smi reports as a (usually unprefixed) hex string.
func boardIDOf(d model.DeviceInfo) (uint64, bool) {
	if hi, okH := parseHexOrDec(d.SmbusTelem.BoardIDHigh); okH {
		if lo, okL := parseHexOrDec(d.SmbusTelem.BoardIDLow); okL {
			return (hi << 32) | (lo & 0xFFFFFFFF), true
		}
	}
	if s := strings.TrimSpace(d.BoardInfo.BoardID); s != "" {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
		if v, err := strconv.ParseUint(s, 16, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// rawASICID returns (ASIC_ID_HIGH << 32) | ASIC_ID_LOW, the chip id used on the
// ETH-untrained path. tt-smi exposes these even when the combined ASIC_ID field
// is null.
func rawASICID(d model.DeviceInfo) (uint64, bool) {
	if hi, okH := parseHexOrDec(d.SmbusTelem.ASICIDHigh); okH {
		if lo, okL := parseHexOrDec(d.SmbusTelem.ASICIDLow); okL {
			return (hi << 32) | (lo & 0xFFFFFFFF), true
		}
	}
	return 0, false
}

// parseHexOrDec parses a tt-smi numeric string that is either "0x"-prefixed hex
// or plain decimal.
func parseHexOrDec(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		return v, err == nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}
