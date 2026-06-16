package collector

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Shion1305/tt-exporter/internal/model"
)

// smiTimeout bounds how long a single `tt-smi -s` invocation may run, so a hung
// tt-smi cannot wedge a scrape indefinitely.
const smiTimeout = 10 * time.Second

// GetSmiSnapshot runs `tt-smi -s` once and returns the parsed output. Callers
// that need both hardware metrics and device identities should reuse a single
// snapshot rather than exec'ing tt-smi twice per scrape.
func GetSmiSnapshot() (model.TTSmiOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), smiTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tt-smi", "-s").Output()
	if err != nil {
		return model.TTSmiOutput{}, err
	}
	var smiOut model.TTSmiOutput
	if err := json.Unmarshal(out, &smiOut); err != nil {
		return model.TTSmiOutput{}, err
	}
	return smiOut, nil
}

// DeviceMetricsFromSnapshot derives the per-device hardware metrics from a
// tt-smi snapshot.
func DeviceMetricsFromSnapshot(smiOut model.TTSmiOutput) []model.DeviceMetrics {
	var metrics []model.DeviceMetrics
	for _, dev := range smiOut.DeviceInfo {
		m := model.DeviceMetrics{
			ID: dev.BoardInfo.BusID,
		}

		m.Power, _ = strconv.ParseFloat(strings.TrimSpace(dev.Telemetry.Power), 64)
		m.Temperature, _ = strconv.ParseFloat(strings.TrimSpace(dev.Telemetry.ASICTemperature), 64)
		m.FanSpeed, _ = strconv.ParseFloat(strings.TrimSpace(dev.Telemetry.FanSpeed), 64)
		m.AICLK, _ = strconv.ParseFloat(strings.TrimSpace(dev.Telemetry.AICLK), 64)

		tdp, _ := strconv.ParseFloat(strings.TrimSpace(dev.Limits.TDPLimit), 64)
		if tdp > 0 {
			// Rough estimation of utilization based on power vs TDP
			// Idle power is usually some baseline, but for simplicity:
			m.Utilization = (m.Power / tdp) * 100
		}

		metrics = append(metrics, m)
	}

	return metrics
}

func GetHardwareMetrics() ([]model.DeviceMetrics, error) {
	smiOut, err := GetSmiSnapshot()
	if err != nil {
		return nil, err
	}
	return DeviceMetricsFromSnapshot(smiOut), nil
}
