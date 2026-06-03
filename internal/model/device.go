package model

type TTSmiOutput struct {
	DeviceInfo []DeviceInfo `json:"device_info"`
}

type DeviceInfo struct {
	BoardInfo BoardInfo `json:"board_info"`
	Telemetry Telemetry `json:"telemetry"`
	Limits    Limits    `json:"limits"`
}

type BoardInfo struct {
	BusID     string `json:"bus_id"`
	BoardType string `json:"board_type"`
}

type Telemetry struct {
	Voltage         string `json:"voltage"`
	Current         string `json:"current"`
	Power           string `json:"power"`
	AICLK           string `json:"aiclk"`
	ASICTemperature string `json:"asic_temperature"`
	FanSpeed        string `json:"fan_speed"`
}

type Limits struct {
	TDPLimit string `json:"tdp_limit"`
}

type DeviceMetrics struct {
	ID          string
	Power       float64
	Temperature float64
	FanSpeed    float64
	AICLK       float64
	Utilization float64
	Workers     int
}
