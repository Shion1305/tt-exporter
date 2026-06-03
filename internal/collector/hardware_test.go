package collector

import (
	"testing"
)

func TestGetHardwareMetrics(t *testing.T) {
	// Note: This test requires tt-smi to be present in the environment
	metrics, err := GetHardwareMetrics()
	if err != nil {
		t.Fatalf("Failed to get hardware metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Log("No devices found (expected if no hardware is present, but tt-smi -s should still output something)")
	}

	for _, m := range metrics {
		t.Logf("Device %s: Power=%.2fW, Temp=%.2fC, Fan=%.2f%%, AICLK=%.2fMHz, Util=%.2f%%",
			m.ID, m.Power, m.Temperature, m.FanSpeed, m.AICLK, m.Utilization)
		if m.ID == "" {
			t.Error("Device ID should not be empty")
		}
	}
}
