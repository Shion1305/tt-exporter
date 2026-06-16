# Tenstorrent Exporter

Prometheus exporter for Tenstorrent Blackhole devices.

## Metrics

- `tenstorrent_power_watts`: Device power consumption in watts.
- `tenstorrent_temperature_celsius`: Device ASIC temperature in celsius.
- `tenstorrent_fan_speed_percent`: Device fan speed percentage.
- `tenstorrent_aiclk_mhz`: Device AI clock frequency in MHz.
- `tenstorrent_utilization_percent`: Estimated utilization percentage of the device based on power.
- `tenstorrent_active_workers`: Number of processes actively using the Tenstorrent device.

### Device memory metrics (tt-metal #37218 SHM)

When a workload runs on a tt-metal build that includes the SHM memory tracking from
[tt-metal#37218](https://github.com/tenstorrent/tt-metal/pull/37218), the exporter reads the
per-device shared-memory regions it publishes (`/dev/shm/tt_device_*_memory`) and exposes the
allocator's live DRAM usage. These series are **absent** unless such a workload is running and the
SHM is visible (see [Device memory requirements](#device-memory-requirements)).

- `tenstorrent_dram_used_bytes`: DRAM bytes allocated by tt-metal on the device.
- `tenstorrent_l1_used_bytes`: L1 bytes allocated by tt-metal on the device.
- `tenstorrent_chip_dram_used_bytes`: DRAM used per chip reached through a device (`chip_id` 0 = local gateway; remote chips via `is_remote`).
- `tenstorrent_proc_dram_used_bytes`: DRAM used per process (opt-in via `TT_EXPORTER_SHM_PER_PID=1`; PID is in the workload's namespace).
- `tenstorrent_mem_reference_count`: Processes attached to the device's SHM region (liveness signal).
- `tenstorrent_mem_phantom`: `1` when `reference_count>0` but no host process holds the device fd — the totals are stale/frozen from a crashed workload, so the used gauges are suppressed; `0` when liveness was verified. Emitted only when the cross-check could run (see below); a crashed workload's SHM file persists until you `rm /dev/shm/tt_device_*`.
- `tenstorrent_shm_liveness_unverifiable`: `1` when `reference_count>0` but liveness could not be cross-checked (the `asic_id` did not resolve to a BusID, or the `/proc` fd scan was unavailable). The used gauges are still emitted (the device may be live), and `tenstorrent_mem_phantom` is absent rather than a misleading `0`. Asserting phantom/liveness requires the `asic_id` to resolve to a BusID **and** the exporter to share the workload's PID namespace (`--pid host`).
- `tenstorrent_shm_version`, `tenstorrent_shm_num_active_processes`, `tenstorrent_shm_last_update_seconds`: SHM diagnostics.
- `tenstorrent_shm_info`: Static info series (value `1`); exposes the **unstable** logical `metal_device_id`. Do not join on it.
- `tenstorrent_shm_parse_errors_total`: Count of SHM files skipped, by `reason` (`eacces`, `short_file`, `unknown_version`, `other`).

Memory series are labelled by `asic_id` (the stable chip unique id, also the SHM filename) and, when it
can be resolved from `tt-smi`, `device_id` (the PCI BusID) so they join the hardware metrics above with
e.g. `tenstorrent_dram_used_bytes * on(device_id) group_left() tenstorrent_power_watts`.

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
  -v /dev/shm:/dev/shm:ro \
  --device /dev/tenstorrent/0:/dev/tenstorrent/0 \
  --device /dev/tenstorrent/1:/dev/tenstorrent/1 \
  -v /usr/bin/tt-smi:/usr/bin/tt-smi \
  -v /usr/lib:/usr/lib \
  ghcr.io/shion1305/tt-exporter:latest
```

*Note: The container requires `--pid host` and `--privileged` (or root user) to correctly scan host processes for workload metrics and to interact with hardware drivers via `tt-smi`. The `-v /dev/shm:/dev/shm:ro` mount is required only for the [device memory metrics](#device-memory-metrics-tt-metal-37218-shm); the read-only bind is sufficient.*

### Device memory requirements

The DRAM/L1 memory metrics are read from the SHM regions that tt-metal #37218 publishes. They appear only when **all** of the following hold:

1. **The workload's tt-metal includes #37218** (merged 2026-04-19). For `tt-inference-server` images this means **0.14.0 or newer** (0.10.0/0.11.1/0.12.0 do not ship it). Verify by checking that `/dev/shm/tt_device_*_memory` appears once a model is loaded.
2. **`TT_METAL_SHM_TRACKING_DISABLED` is not set** in the workload. It is a kill switch for the whole provider — when set, no SHM file is created at all.
3. **The SHM is visible to the exporter.** tt-metal writes to `/dev/shm`. The `tt-inference-server` launcher runs the model container with `--ipc host`, which places the files on the **host** `/dev/shm`; the exporter then needs `-v /dev/shm:/dev/shm:ro` (or `--ipc host`). If the workload uses a private IPC namespace instead, share it directly (`--ipc=container:<workload>`), or co-locate both in one Kubernetes Pod with a shared `emptyDir{medium: Memory}` mounted at `/dev/shm`.
4. **The exporter can read the `0600` files.** They are owned by the workload's uid; the exporter must run as root (the `--privileged` above already does) or a matching uid. Under rootless/userns-remapped runtimes the owner is the remapped uid — run the exporter in the host user namespace.

When these are not met the exporter degrades gracefully: the memory series are simply absent and all other `tenstorrent_*` metrics keep working. `TT_EXPORTER_SHM_DIR` overrides the scanned directory (default `/dev/shm`); `TT_EXPORTER_SHM_PER_PID=1` enables per-process series.

## Implementation Details

- **Hardware Metrics**: Collected by parsing JSON output from `tt-smi -s`.
- **Workload Metrics**: Processes using Tenstorrent devices are identified by scanning `/proc/*/fd` for open file descriptors to `/dev/tenstorrent/*`.
- **Utilization**: Roughly estimated as `(Current Power / TDP Limit) * 100`.
- **Device Memory**: Parsed from the tt-metal #37218 `DeviceMemoryRegion` shared-memory files (`/dev/shm/tt_device_*_memory`) with a plain `read()` (never `mmap`, to avoid SIGBUS on a partially-initialised region). The SHM `asic_id` is joined to a PCI BusID via `tt-smi` SMBus telemetry (`mangle_asic_id(board_id, asic_location)`), and liveness is cross-checked against the `/proc` fd scan so stale totals from a crashed workload are reported as `tenstorrent_mem_phantom` rather than real usage.
