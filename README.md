# Tenstorrent Exporter

Prometheus exporter for Tenstorrent Blackhole devices.

## Metrics

- `tenstorrent_power_watts`: Device power consumption in watts.
- `tenstorrent_temperature_celsius`: Device ASIC temperature in celsius.
- `tenstorrent_fan_speed_percent`: Device fan speed percentage.
- `tenstorrent_aiclk_mhz`: Device AI clock frequency in MHz.
- `tenstorrent_utilization_percent`: Estimated utilization percentage of the device based on power.
- `tenstorrent_active_workers`: Number of processes actively using the Tenstorrent device.

## Usage

### Prerequisites

- `tt-smi` must be installed and available in the PATH.
- Tenstorrent driver and UMD must be installed.

### Build

```bash
go build -o tt-exporter cmd/tt-exporter/main.go
```

### Run

```bash
./tt-exporter
```

The exporter will start on port `9400`. You can access metrics at `http://localhost:9400/metrics`.

## Implementation Details

- **Hardware Metrics**: Collected by parsing JSON output from `tt-smi -s`.
- **Workload Metrics**: Processes using Tenstorrent devices are identified by scanning `/proc/*/fd` for open file descriptors to `/dev/tenstorrent/*`.
- **Utilization**: Roughly estimated as `(Current Power / TDP Limit) * 100`.
