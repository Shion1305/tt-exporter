package collector

import (
	"testing"
)

func TestGetActiveWorkers(t *testing.T) {
	workers, err := GetActiveWorkers()
	if err != nil {
		t.Fatalf("Failed to get active workers: %v", err)
	}

	t.Logf("Active workers: %v", workers)
}
