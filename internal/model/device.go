package model

type TTSmiOutput struct {
	DeviceInfo []DeviceInfo `json:"device_info"`
}

type DeviceInfo struct {
	BoardInfo  BoardInfo  `json:"board_info"`
	SmbusTelem SmbusTelem `json:"smbus_telem"`
	Telemetry  Telemetry  `json:"telemetry"`
	Limits     Limits     `json:"limits"`
}

type BoardInfo struct {
	BusID     string `json:"bus_id"`
	BoardType string `json:"board_type"`
	BoardID   string `json:"board_id"`
}

// SmbusTelem holds the raw SMBus telemetry fields used to reconstruct a chip's
// UMD chip_unique_id (= the #37218 SHM file's asic_id) without opening a device.
// Values are tt-smi's hex strings (e.g. "0x403").
type SmbusTelem struct {
	BoardIDHigh  string `json:"BOARD_ID_HIGH"`
	BoardIDLow   string `json:"BOARD_ID_LOW"`
	ASICLocation string `json:"ASIC_LOCATION"`
	ASICIDHigh   string `json:"ASIC_ID_HIGH"`
	ASICIDLow    string `json:"ASIC_ID_LOW"`
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

// SHMChip is one chip_stats[] entry of a DeviceMemoryRegion. Index 0 is the
// local/gateway chip; 1..15 are remote chips reached through this gateway.
type SHMChip struct {
	ChipID      uint32
	IsRemote    bool
	DRAMUsed    uint64
	L1Used      uint64
	L1SmallUsed uint64
	TraceUsed   uint64
	CBUsed      uint64
}

// SHMProc is one processes[] entry. PID is in the *writer's* PID namespace
// (the model container), so it must not be resolved against host /proc.
type SHMProc struct {
	PID          int32
	Name         string
	DRAMUsed     uint64
	L1Used       uint64
	L1SmallUsed  uint64
	TraceUsed    uint64
	CBUsed       uint64
	LastUpdateNs uint64
}

// SHMRegion is a parsed tt-metal DeviceMemoryRegion (PR #37218), read from
// /dev/shm/tt_device_<chip_unique_id>_memory. All counters are point-in-time:
// the producer writes lock-free (relaxed atomics), so no cross-field snapshot
// consistency is guaranteed.
type SHMRegion struct {
	Path           string
	Version        uint32
	NumActiveProcs uint32
	LastUpdateNs   uint64
	ReferenceCount uint32
	ASICID         uint64 // chip_unique_id; equals the decimal filename stem (stable identity key)
	MetalDeviceID  uint32 // logical Metal device id; UNSTABLE — never use as the device_id label
	DRAMUsed       uint64
	L1Used         uint64
	L1SmallUsed    uint64
	TraceUsed      uint64
	CBUsed         uint64
	Chips          []SHMChip
	Procs          []SHMProc
}
