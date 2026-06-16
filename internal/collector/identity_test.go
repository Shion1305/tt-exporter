package collector

import (
	"testing"

	"github.com/Shion1305/tt-exporter/internal/model"
)

// TestMangleASICIDRealHardware pins the join formula against a value verified on
// real hardware: board_id 0x403319141bb, asic_location 0 must produce the
// asic_id 0x806632283760 (= /dev/shm/tt_device_141176416515936_memory).
func TestMangleASICIDRealHardware(t *testing.T) {
	got := mangleASICID(0x403319141bb, 0)
	const want = uint64(0x806632283760)
	if got != want {
		t.Fatalf("mangleASICID = %#x (%d), want %#x (%d)", got, got, want, want)
	}
	if want != 141176416515936 {
		t.Fatalf("sanity: want decimal should be the observed filename stem")
	}
}

func TestMangleASICIDUsesLow5Bits(t *testing.T) {
	// asic_location is masked to 5 bits: bit 5 (0x20) is dropped, so it behaves
	// the same as location 0 -> board_id << 5.
	if got := mangleASICID(1, 0x20); got != 1<<5 {
		t.Errorf("mangleASICID(1,0x20) = %d, want %d", got, 1<<5)
	}
	// The low bits do land in the result.
	if got := mangleASICID(1, 0x01); got != (1<<5)|1 {
		t.Errorf("mangleASICID(1,1) = %#x, want %#x", got, (1<<5)|1)
	}
}

func TestParseHexOrDec(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"0x403", 0x403, true},
		{"0X31914116", 0x31914116, true},
		{" 1027 ", 1027, true},
		{"", 0, false},
		{"zzz", 0, false},
	}
	for _, c := range cases {
		got, ok := parseHexOrDec(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseHexOrDec(%q) = (%#x,%v), want (%#x,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestASICToBusID(t *testing.T) {
	snap := model.TTSmiOutput{
		DeviceInfo: []model.DeviceInfo{
			{
				BoardInfo: model.BoardInfo{BusID: "0000:03:00.0"},
				SmbusTelem: model.SmbusTelem{
					BoardIDHigh:  "0x403",
					BoardIDLow:   "0x319141bb",
					ASICLocation: "0x0",
					ASICIDHigh:   "0xe86b7bf2",
					ASICIDLow:    "0x61da9faa",
				},
			},
		},
	}
	m := asicToBusID(snap)

	// Mangled candidate resolves to the BusID (the live path).
	if bus := m[0x806632283760]; bus != "0000:03:00.0" {
		t.Errorf("mangled asic_id -> %q, want 0000:03:00.0", bus)
	}
	// Raw ASIC_ID candidate also resolves (ETH-untrained fallback path).
	if bus := m[0xe86b7bf261da9faa]; bus != "0000:03:00.0" {
		t.Errorf("raw asic_id -> %q, want 0000:03:00.0", bus)
	}
}

func TestBoardIDFallbackToBoardInfo(t *testing.T) {
	// When SMBus high/low are absent, board_info.board_id (unprefixed hex) is used.
	d := model.DeviceInfo{BoardInfo: model.BoardInfo{BoardID: "0000040331914116"}}
	got, ok := boardIDOf(d)
	if !ok || got != 0x40331914116 {
		t.Errorf("boardIDOf fallback = (%#x,%v), want (0x40331914116,true)", got, ok)
	}
}
