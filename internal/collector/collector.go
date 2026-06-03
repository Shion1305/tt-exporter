package collector

import (
	"log"
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
}

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
	}
}

func (c *TTCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.powerDesc
	ch <- c.tempDesc
	ch <- c.fanDesc
	ch <- c.clkDesc
	ch <- c.utilizationDesc
	ch <- c.workersDesc
}

func (c *TTCollector) Collect(ch chan<- prometheus.Metric) {
	devices, err := GetHardwareMetrics()
	if err != nil {
		log.Printf("Error collecting hardware metrics: %v", err)
		return
	}

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
}
