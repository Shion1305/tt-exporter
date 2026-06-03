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

	http.Handle("/metrics", promhttp.Handler())
	log.Println("Starting tt-exporter on :9400")
	if err := http.ListenAndServe(":9400", nil); err != nil {
		log.Fatalf("Error starting HTTP server: %v", err)
	}
}
