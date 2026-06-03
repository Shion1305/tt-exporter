package collector

import (
	"os"
	"path/filepath"
	"strings"
)

// GetActiveWorkers returns a map of BusID to the number of processes using that device.
func GetActiveWorkers() (map[string]int, error) {
	busIDToWorkers := make(map[string]int)

	// Map /dev/tenstorrent/X to BusID
	devToBusID := make(map[string]string)
	sysEntries, err := os.ReadDir("/sys/class/tenstorrent")
	if err == nil {
		for _, entry := range sysEntries {
			name := entry.Name() // e.g., "tenstorrent!0"
			if !strings.HasPrefix(name, "tenstorrent!") {
				continue
			}
			parts := strings.Split(name, "!")
			if len(parts) < 2 {
				continue
			}
			devIdx := parts[1]
			devPath := "/dev/tenstorrent/" + devIdx

			link, err := os.Readlink(filepath.Join("/sys/class/tenstorrent", name, "device"))
			if err != nil {
				continue
			}
			// link is like "../../../0000:01:00.0"
			busID := filepath.Base(link)
			devToBusID[devPath] = busID
			busIDToWorkers[busID] = 0
		}
	}

	// Scan /proc to find processes using these devices
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		return busIDToWorkers, err
	}

	for _, entry := range procEntries {
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if pid[0] < '0' || pid[0] > '9' {
			continue
		}

		fdPath := filepath.Join("/proc", pid, "fd")
		fds, err := os.ReadDir(fdPath)
		if err != nil {
			continue
		}

		processBusIDs := make(map[string]struct{})
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdPath, fd.Name()))
			if err != nil {
				continue
			}

			if busID, ok := devToBusID[link]; ok {
				processBusIDs[busID] = struct{}{}
			}
		}

		for busID := range processBusIDs {
			busIDToWorkers[busID]++
		}
	}

	return busIDToWorkers, nil
}
