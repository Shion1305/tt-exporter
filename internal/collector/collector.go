package collector

import (
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/Shion1305/tt-exporter/internal/model"

	"github.com/prometheus/client_golang/prometheus"
)

type TTCollector struct {
	powerDesc       *prometheus.Desc
	tempDesc        *prometheus.Desc
	fanDesc         *prometheus.Desc
	clkDesc         *prometheus.Desc
	utilizationDesc *prometheus.Desc
	workersDesc     *prometheus.Desc

	// #37218 SHM memory descriptors.
	dramUsedDesc     *prometheus.Desc
	l1UsedDesc       *prometheus.Desc
	chipDramUsedDesc *prometheus.Desc
	procDramUsedDesc *prometheus.Desc
	refCountDesc     *prometheus.Desc
	phantomDesc      *prometheus.Desc
	shmVersionDesc   *prometheus.Desc
	numActiveDesc    *prometheus.Desc
	lastUpdateDesc   *prometheus.Desc
	parseErrDesc     *prometheus.Desc
	shmInfoDesc      *prometheus.Desc

	mu             sync.Mutex
	parseErrTotals map[string]float64 // cumulative across scrapes, by reason
}

// memLabels are the labels shared by the memory gauges. "device_id" carries the
// PCI BusID (matching the existing tenstorrent_* metrics, enabling PromQL joins);
// it is empty when the SHM file's asic_id cannot be resolved to a BusID. The
// SHM struct's logical Metal device id is deliberately NOT placed here — it is
// exposed only as metal_device_id on tenstorrent_shm_info.
var memLabels = []string{"asic_id", "device_id"}

func NewTTCollector() *TTCollector {
	return &TTCollector{
		powerDesc: prometheus.NewDesc(
			"tenstorrent_power_watts",
			"Tenstorrent device power consumption in watts",
			[]string{"device_id"}, nil,
		),
		tempDesc: prometheus.NewDesc(
			"tenstorrent_temperature_celsius",
			"Tenstorrent device ASIC temperature in celsius",
			[]string{"device_id"}, nil,
		),
		fanDesc: prometheus.NewDesc(
			"tenstorrent_fan_speed_percent",
			"Tenstorrent device fan speed percentage",
			[]string{"device_id"}, nil,
		),
		clkDesc: prometheus.NewDesc(
			"tenstorrent_aiclk_mhz",
			"Tenstorrent device AI clock frequency in MHz",
			[]string{"device_id"}, nil,
		),
		utilizationDesc: prometheus.NewDesc(
			"tenstorrent_utilization_percent",
			"Estimated utilization percentage of the device based on power",
			[]string{"device_id"}, nil,
		),
		workersDesc: prometheus.NewDesc(
			"tenstorrent_active_workers",
			"Number of processes actively using the Tenstorrent device",
			[]string{"device_id"}, nil,
		),

		dramUsedDesc: prometheus.NewDesc(
			"tenstorrent_dram_used_bytes",
			"DRAM bytes allocated by tt-metal on the device (from #37218 SHM tracking). Absent when no live workload is tracking the device.",
			memLabels, nil,
		),
		l1UsedDesc: prometheus.NewDesc(
			"tenstorrent_l1_used_bytes",
			"L1 bytes allocated by tt-metal on the device (from #37218 SHM tracking).",
			memLabels, nil,
		),
		chipDramUsedDesc: prometheus.NewDesc(
			"tenstorrent_chip_dram_used_bytes",
			"DRAM bytes allocated per chip reached through this device (chip_stats[]); chip_id 0 is the local gateway.",
			[]string{"asic_id", "device_id", "chip_id", "is_remote"}, nil,
		),
		procDramUsedDesc: prometheus.NewDesc(
			"tenstorrent_proc_dram_used_bytes",
			"DRAM bytes allocated per process (per-PID; opt-in via TT_EXPORTER_SHM_PER_PID). PID is in the workload's namespace.",
			[]string{"asic_id", "device_id", "pid", "process_name"}, nil,
		),
		refCountDesc: prometheus.NewDesc(
			"tenstorrent_mem_reference_count",
			"Number of processes attached to the device's SHM region. Liveness signal, but not sufficient alone: an ungraceful exit leaves it stuck above zero (see tenstorrent_mem_phantom).",
			memLabels, nil,
		),
		phantomDesc: prometheus.NewDesc(
			"tenstorrent_mem_phantom",
			"1 when reference_count>0 but no host process holds the device fd, i.e. the SHM totals are stale/frozen from a crashed workload (used gauges are then suppressed).",
			memLabels, nil,
		),
		shmVersionDesc: prometheus.NewDesc(
			"tenstorrent_shm_version",
			"DeviceMemoryRegion structure version of the SHM file.",
			memLabels, nil,
		),
		numActiveDesc: prometheus.NewDesc(
			"tenstorrent_shm_num_active_processes",
			"num_active_processes field of the SHM region (diagnostic only; not a liveness gate).",
			memLabels, nil,
		),
		lastUpdateDesc: prometheus.NewDesc(
			"tenstorrent_shm_last_update_seconds",
			"Unix time of the SHM region's last update (writer wall clock). Advances only on alloc/dealloc, so a steady hold looks stale though live; use for freshness only.",
			memLabels, nil,
		),
		parseErrDesc: prometheus.NewDesc(
			"tenstorrent_shm_parse_errors_total",
			"Cumulative count of SHM files skipped, by reason (eacces, short_file, unknown_version, other).",
			[]string{"reason"}, nil,
		),
		shmInfoDesc: prometheus.NewDesc(
			"tenstorrent_shm_info",
			"Static info for a device's SHM region; value is always 1. metal_device_id is the logical (UNSTABLE) Metal id — do not join on it.",
			[]string{"asic_id", "device_id", "metal_device_id"}, nil,
		),

		parseErrTotals: map[string]float64{},
	}
}

func (c *TTCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.powerDesc
	ch <- c.tempDesc
	ch <- c.fanDesc
	ch <- c.clkDesc
	ch <- c.utilizationDesc
	ch <- c.workersDesc
	ch <- c.dramUsedDesc
	ch <- c.l1UsedDesc
	ch <- c.chipDramUsedDesc
	ch <- c.procDramUsedDesc
	ch <- c.refCountDesc
	ch <- c.phantomDesc
	ch <- c.shmVersionDesc
	ch <- c.numActiveDesc
	ch <- c.lastUpdateDesc
	ch <- c.parseErrDesc
	ch <- c.shmInfoDesc
}

func (c *TTCollector) Collect(ch chan<- prometheus.Metric) {
	smiOut, err := GetSmiSnapshot()
	if err != nil {
		log.Printf("Error collecting hardware metrics: %v", err)
		return
	}
	devices := DeviceMetricsFromSnapshot(smiOut)
	asicMap := asicToBusID(smiOut)

	workers, err := GetActiveWorkers()
	if err != nil {
		log.Printf("Error collecting worker metrics: %v", err)
	}

	var wg sync.WaitGroup
	for _, dev := range devices {
		wg.Add(1)
		go func(d model.DeviceMetrics) {
			defer wg.Done()
			ch <- prometheus.MustNewConstMetric(
				c.powerDesc, prometheus.GaugeValue, d.Power, d.ID,
			)
			ch <- prometheus.MustNewConstMetric(
				c.tempDesc, prometheus.GaugeValue, d.Temperature, d.ID,
			)
			ch <- prometheus.MustNewConstMetric(
				c.fanDesc, prometheus.GaugeValue, d.FanSpeed, d.ID,
			)
			ch <- prometheus.MustNewConstMetric(
				c.clkDesc, prometheus.GaugeValue, d.AICLK, d.ID,
			)
			ch <- prometheus.MustNewConstMetric(
				c.utilizationDesc, prometheus.GaugeValue, d.Utilization, d.ID,
			)
			ch <- prometheus.MustNewConstMetric(
				c.workersDesc, prometheus.GaugeValue, float64(workers[d.ID]), d.ID,
			)
		}(dev)
	}
	wg.Wait()

	// SHM memory metrics are collected separately and must never fail or skip
	// the hardware/worker metrics above.
	c.collectMemory(ch, asicMap, workers)
}

// collectMemory reads the #37218 SHM files and emits the memory metric family.
func (c *TTCollector) collectMemory(ch chan<- prometheus.Metric, asicMap map[uint64]string, workers map[string]int) {
	res := collectSHMRegions(shmDir())

	// Accumulate and emit cumulative parse-error counters.
	c.mu.Lock()
	for reason, n := range res.ErrReasons {
		c.parseErrTotals[reason] += float64(n)
	}
	totals := make(map[string]float64, len(c.parseErrTotals))
	for k, v := range c.parseErrTotals {
		totals[k] = v
	}
	c.mu.Unlock()
	for reason, v := range totals {
		ch <- prometheus.MustNewConstMetric(c.parseErrDesc, prometheus.CounterValue, v, reason)
	}

	perPID := shmPerPIDEnabled()
	for _, r := range res.Regions {
		busID := asicMap[r.ASICID] // "" when the asic_id can't be resolved to a BusID
		asic := fmt.Sprintf("0x%016x", r.ASICID)

		// Always-on diagnostics.
		ch <- prometheus.MustNewConstMetric(c.shmInfoDesc, prometheus.GaugeValue, 1,
			asic, busID, strconv.FormatUint(uint64(r.MetalDeviceID), 10))
		ch <- prometheus.MustNewConstMetric(c.refCountDesc, prometheus.GaugeValue, float64(r.ReferenceCount), asic, busID)
		ch <- prometheus.MustNewConstMetric(c.shmVersionDesc, prometheus.GaugeValue, float64(r.Version), asic, busID)
		ch <- prometheus.MustNewConstMetric(c.numActiveDesc, prometheus.GaugeValue, float64(r.NumActiveProcs), asic, busID)
		ch <- prometheus.MustNewConstMetric(c.lastUpdateDesc, prometheus.GaugeValue, float64(r.LastUpdateNs)/1e9, asic, busID)

		// Liveness: reference_count>0 is necessary but not sufficient. If the
		// region claims live processes but no host process holds the device fd,
		// the totals are a frozen phantom from a crashed workload (R-PHANTOM).
		// We can only assert this when the asic_id resolves to a known BusID.
		phantom := r.ReferenceCount > 0 && busID != "" && workers[busID] == 0
		phantomVal := 0.0
		if phantom {
			phantomVal = 1
		}
		ch <- prometheus.MustNewConstMetric(c.phantomDesc, prometheus.GaugeValue, phantomVal, asic, busID)

		switch {
		case r.ReferenceCount == 0:
			// Graceful detach resets totals to 0 — emit a true zero.
			ch <- prometheus.MustNewConstMetric(c.dramUsedDesc, prometheus.GaugeValue, 0, asic, busID)
			ch <- prometheus.MustNewConstMetric(c.l1UsedDesc, prometheus.GaugeValue, 0, asic, busID)
		case phantom:
			// Frozen totals are not trustworthy — suppress used gauges (they
			// become absent), leaving tenstorrent_mem_phantom=1 as the signal.
		default:
			ch <- prometheus.MustNewConstMetric(c.dramUsedDesc, prometheus.GaugeValue, float64(r.DRAMUsed), asic, busID)
			ch <- prometheus.MustNewConstMetric(c.l1UsedDesc, prometheus.GaugeValue, float64(r.L1Used), asic, busID)
			for _, chip := range r.Chips {
				ch <- prometheus.MustNewConstMetric(c.chipDramUsedDesc, prometheus.GaugeValue, float64(chip.DRAMUsed),
					asic, busID, strconv.FormatUint(uint64(chip.ChipID), 10), strconv.FormatBool(chip.IsRemote))
			}
			if perPID {
				for _, p := range r.Procs {
					ch <- prometheus.MustNewConstMetric(c.procDramUsedDesc, prometheus.GaugeValue, float64(p.DRAMUsed),
						asic, busID, strconv.Itoa(int(p.PID)), p.Name)
				}
			}
		}
	}
}
