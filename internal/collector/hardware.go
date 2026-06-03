package collector

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"github.com/Shion1305/tt-exporter/internal/model"
)

func GetHardwareMetrics() ([]model.DeviceMetrics, error) {
	out, err := exec.Command("tt-smi", "-s").Output()
	if err != nil {
		return nil, err
	}

	var smiOut model.TTSmiOutput
	if err := json.Unmarshal(out, &smiOut); err != nil {
		return nil, err
	}

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

	return metrics, nil
}
