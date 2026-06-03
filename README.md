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

### Docker

The exporter is also available as a Docker image.

```bash
docker run -d \
  --name tt-exporter \
  -p 9400:9400 \
  --pid host \
  --privileged \
  --device /dev/tenstorrent/0:/dev/tenstorrent/0 \
  --device /dev/tenstorrent/1:/dev/tenstorrent/1 \
  -v /usr/bin/tt-smi:/usr/bin/tt-smi \
  -v /usr/lib:/usr/lib \
  ghcr.io/shion1305/tt-exporter:latest
```

*Note: The container requires `--pid host` and `--privileged` (or root user) to correctly scan host processes for workload metrics and to interact with hardware drivers via `tt-smi`.*

## Implementation Details

- **Hardware Metrics**: Collected by parsing JSON output from `tt-smi -s`.
- **Workload Metrics**: Processes using Tenstorrent devices are identified by scanning `/proc/*/fd` for open file descriptors to `/dev/tenstorrent/*`.
- **Utilization**: Roughly estimated as `(Current Power / TDP Limit) * 100`.
