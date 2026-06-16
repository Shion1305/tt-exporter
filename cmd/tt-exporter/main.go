package main

import (
	"log"
	"net/http"

	"github.com/Shion1305/tt-exporter/internal/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	ttCollector := collector.NewTTCollector()
	prometheus.MustRegister(ttCollector)

	// Each scrape execs tt-smi, scans /proc and reads /dev/shm, so cap concurrent
	// scrapes to shed load (e.g. a hung tt-smi plus retrying scrapers) rather than
	// pile up open fds.
	handler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		MaxRequestsInFlight: 4,
	})
	http.Handle("/metrics", handler)
	log.Println("Starting tt-exporter on :9400")
	if err := http.ListenAndServe(":9400", nil); err != nil {
		log.Fatalf("Error starting HTTP server: %v", err)
	}
}
