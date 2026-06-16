package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Shion1305/tt-exporter/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type sample struct {
	labels map[string]string
	value  float64
}

// captureMemory writes one region to a temp SHM dir and runs collectMemory,
// returning the emitted metrics keyed by metric name.
func captureMemory(t *testing.T, region []byte, asicMap map[uint64]string, workers map[string]int, reliable bool) map[string][]sample {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tt_device_141176416515936_memory"), region, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TT_EXPORTER_SHM_DIR", dir)

	c := NewTTCollector()
	name := map[*prometheus.Desc]string{
		c.dramUsedDesc: "dram_used", c.l1UsedDesc: "l1_used", c.chipDramUsedDesc: "chip_dram",
		c.refCountDesc: "refcount", c.phantomDesc: "phantom", c.unverifiableDesc: "unverifiable",
		c.shmVersionDesc: "version", c.shmInfoDesc: "info", c.lastUpdateDesc: "last_update",
		c.numActiveDesc: "num_active", c.parseErrDesc: "parse_errors", c.procDramUsedDesc: "proc_dram",
	}

	ch := make(chan prometheus.Metric, 256)
	c.collectMemory(ch, asicMap, workers, reliable)
	close(ch)

	out := map[string][]sample{}
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			t.Fatal(err)
		}
		n := name[m.Desc()]
		labels := map[string]string{}
		for _, lp := range d.GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		v := d.GetGauge().GetValue()
		if d.Gauge == nil {
			v = d.GetCounter().GetValue()
		}
		out[n] = append(out[n], sample{labels, v})
	}
	return out
}

const testASIC = uint64(0x806632283760)

func TestLivenessVerifiedLive(t *testing.T) {
	got := captureMemory(t, buildRegion(3, 1, testASIC, 1, 6<<30),
		map[uint64]string{testASIC: "0000:03:00.0"}, map[string]int{"0000:03:00.0": 1}, true)

	if len(got["dram_used"]) != 1 || got["dram_used"][0].value != float64(6<<30) {
		t.Errorf("dram_used = %+v, want one sample of 6 GiB", got["dram_used"])
	}
	if got["dram_used"][0].labels["device_id"] != "0000:03:00.0" {
		t.Errorf("device_id label = %q, want joined BusID", got["dram_used"][0].labels["device_id"])
	}
	if len(got["phantom"]) != 1 || got["phantom"][0].value != 0 {
		t.Errorf("phantom = %+v, want 0", got["phantom"])
	}
	if len(got["unverifiable"]) != 0 {
		t.Errorf("unverifiable should be absent when verified live, got %+v", got["unverifiable"])
	}
}

func TestLivenessVerifiedPhantom(t *testing.T) {
	// refcount>0, BusID resolved, scan reliable, but zero workers hold the fd.
	got := captureMemory(t, buildRegion(3, 1, testASIC, 1, 6<<30),
		map[uint64]string{testASIC: "0000:03:00.0"}, map[string]int{"0000:03:00.0": 0}, true)

	if len(got["dram_used"]) != 0 {
		t.Errorf("dram_used must be suppressed for a phantom, got %+v", got["dram_used"])
	}
	if len(got["phantom"]) != 1 || got["phantom"][0].value != 1 {
		t.Errorf("phantom = %+v, want 1", got["phantom"])
	}
}

func TestLivenessUnverifiableNoBusID(t *testing.T) {
	// asic_id does not resolve to a BusID -> cannot cross-check.
	got := captureMemory(t, buildRegion(3, 1, testASIC, 1, 6<<30),
		map[uint64]string{}, map[string]int{}, true)

	if len(got["dram_used"]) != 1 || got["dram_used"][0].value != float64(6<<30) {
		t.Errorf("dram_used should still be emitted (raw) when unverifiable, got %+v", got["dram_used"])
	}
	if got["dram_used"][0].labels["device_id"] != "" {
		t.Errorf("device_id should be empty when unresolved, got %q", got["dram_used"][0].labels["device_id"])
	}
	if len(got["unverifiable"]) != 1 || got["unverifiable"][0].value != 1 {
		t.Errorf("unverifiable = %+v, want 1", got["unverifiable"])
	}
	if len(got["phantom"]) != 0 {
		t.Errorf("phantom must be absent (not a misleading 0) when unverifiable, got %+v", got["phantom"])
	}
}

func TestLivenessUnverifiableScanUnreliable(t *testing.T) {
	// BusID resolves and key present, but the worker scan failed -> unreliable.
	got := captureMemory(t, buildRegion(3, 1, testASIC, 1, 6<<30),
		map[uint64]string{testASIC: "0000:03:00.0"}, map[string]int{"0000:03:00.0": 0}, false)

	if len(got["dram_used"]) != 1 {
		t.Errorf("dram_used should be emitted when scan is unreliable, got %+v", got["dram_used"])
	}
	if len(got["unverifiable"]) != 1 || got["unverifiable"][0].value != 1 {
		t.Errorf("unverifiable = %+v, want 1", got["unverifiable"])
	}
	if len(got["phantom"]) != 0 {
		t.Errorf("phantom must be absent when scan unreliable, got %+v", got["phantom"])
	}
}

func TestLivenessUnverifiableDeviceNotScanned(t *testing.T) {
	// BusID resolves but is not a key in the worker map (device wasn't enumerated).
	got := captureMemory(t, buildRegion(3, 1, testASIC, 1, 6<<30),
		map[uint64]string{testASIC: "0000:03:00.0"}, map[string]int{"0000:99:00.0": 1}, true)

	if len(got["dram_used"]) != 1 {
		t.Errorf("dram_used should be emitted when device not scanned, got %+v", got["dram_used"])
	}
	if len(got["unverifiable"]) != 1 {
		t.Errorf("unverifiable = %+v, want 1", got["unverifiable"])
	}
}

func TestLivenessRefcountZeroIsTrueZero(t *testing.T) {
	got := captureMemory(t, buildRegion(3, 0, testASIC, 1, 0),
		map[uint64]string{testASIC: "0000:03:00.0"}, map[string]int{"0000:03:00.0": 0}, true)

	if len(got["dram_used"]) != 1 || got["dram_used"][0].value != 0 {
		t.Errorf("dram_used = %+v, want a true 0 at refcount==0", got["dram_used"])
	}
	if len(got["phantom"]) != 1 || got["phantom"][0].value != 0 {
		t.Errorf("phantom = %+v, want 0", got["phantom"])
	}
	if len(got["unverifiable"]) != 0 {
		t.Errorf("unverifiable should be absent at refcount==0, got %+v", got["unverifiable"])
	}
}

func TestAsicToBusIDCollisionLeavesUnresolved(t *testing.T) {
	// Two devices, same board_id, asic_location missing on both -> both mangle to
	// the same key with different BusIDs. The key must be dropped, not misattributed.
	st := model.SmbusTelem{BoardIDHigh: "0x403", BoardIDLow: "0x31914116"}
	snap := model.TTSmiOutput{DeviceInfo: []model.DeviceInfo{
		{BoardInfo: model.BoardInfo{BusID: "0000:01:00.0"}, SmbusTelem: st},
		{BoardInfo: model.BoardInfo{BusID: "0000:03:00.0"}, SmbusTelem: st},
	}}
	m := asicToBusID(snap)
	key := mangleASICID(0x40331914116, 0)
	if bus, ok := m[key]; ok {
		t.Errorf("colliding mangled key should be unresolved, got %q", bus)
	}
}
