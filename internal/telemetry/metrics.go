package telemetry

import (
	"bufio"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ContainerStat struct {
	Name    string `json:"name"`
	MemUsed string `json:"mem_used"` // e.g. "25.54MiB / 30.82GiB"
	MemPerc string `json:"mem_perc"` // e.g. "0.08%"
}

// MemoryDataPoint captures a single telemetry snapshot of system VM and Go runtime memory. a single telemetry snapshot of system VM and Go runtime memory.
type MemoryDataPoint struct {
	Timestamp     string          `json:"timestamp"`            // Local time "15:04:05"
	TimeUnix      int64           `json:"time_unix"`            // Unix timestamp in seconds
	SystemTotalMB float64         `json:"system_total_mb"`      // Total system/VM RAM in MB
	SystemUsedMB  float64         `json:"system_used_mb"`       // Used system/VM RAM in MB
	SystemFreeMB  float64         `json:"system_free_mb"`       // Free system/VM RAM in MB
	SystemPercent float64         `json:"system_percent"`       // System RAM usage % (0-100)
	AppAllocMB    float64         `json:"app_alloc_mb"`         // Go runtime heap allocated in MB
	AppSysMB      float64         `json:"app_sys_mb"`           // Go runtime OS memory obtained in MB
	NumGC         uint32          `json:"num_gc"`               // Total completed GC cycles
	Goroutines    int             `json:"goroutines"`           // Active goroutines count
	Containers    []ContainerStat `json:"containers,omitempty"` // Docker container memory stats
}

// CollectMemoryStats gathers host VM/system RAM and Go runtime memory metrics.
func CollectMemoryStats() MemoryDataPoint {
	now := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	appAllocMB := roundTo(float64(m.Alloc)/(1024*1024), 2)
	appSysMB := roundTo(float64(m.Sys)/(1024*1024), 2)
	numGC := m.NumGC
	goroutines := runtime.NumGoroutine()

	sysTotalMB, sysUsedMB, sysFreeMB, sysPercent := readSystemMemory()
	if sysTotalMB <= 0 {
		// Fallback when /proc/meminfo is inaccessible (e.g. non-Linux host or restricted sandbox)
		sysTotalMB = roundTo(float64(m.Sys)/(1024*1024)*2, 2)
		sysUsedMB = appSysMB
		sysFreeMB = roundTo(math.Max(0, sysTotalMB-sysUsedMB), 2)
		if sysTotalMB > 0 {
			sysPercent = roundTo((sysUsedMB/sysTotalMB)*100, 1)
		}
	}

	return MemoryDataPoint{
		Timestamp:     now.Format("15:04:05"),
		TimeUnix:      now.Unix(),
		SystemTotalMB: sysTotalMB,
		SystemUsedMB:  sysUsedMB,
		SystemFreeMB:  sysFreeMB,
		SystemPercent: sysPercent,
		AppAllocMB:    appAllocMB,
		AppSysMB:      appSysMB,
		NumGC:         numGC,
		Goroutines:    goroutines,
		Containers:    getContainerStats(),
	}
}

// readSystemMemory parses /proc/meminfo on Linux hosts.
func readSystemMemory() (totalMB, usedMB, freeMB, percent float64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0
	}
	defer file.Close()

	var memTotalKB, memFreeKB, memAvailKB, buffersKB, cachedKB float64
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}

		switch key {
		case "MemTotal":
			memTotalKB = val
		case "MemFree":
			memFreeKB = val
		case "MemAvailable":
			memAvailKB = val
		case "Buffers":
			buffersKB = val
		case "Cached":
			cachedKB = val
		}
	}

	if memTotalKB <= 0 {
		return 0, 0, 0, 0
	}

	totalMB = roundTo(memTotalKB/1024, 2)

	if memAvailKB > 0 {
		usedKB := memTotalKB - memAvailKB
		usedMB = roundTo(usedKB/1024, 2)
		freeMB = roundTo(memAvailKB/1024, 2)
	} else {
		usedKB := memTotalKB - (memFreeKB + buffersKB + cachedKB)
		usedMB = roundTo(math.Max(0, usedKB)/1024, 2)
		freeMB = roundTo(memFreeKB/1024, 2)
	}

	if totalMB > 0 {
		percent = roundTo((usedMB/totalMB)*100, 1)
	}

	return totalMB, usedMB, freeMB, percent
}

func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}

func getContainerStats() []ContainerStat {
	var stats []ContainerStat
	// Requesting stats without streaming, format as Name|MemUsage|MemPerc
	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.Name}}|{{.MemUsage}}|{{.MemPerc}}")
	out, err := cmd.Output()
	if err != nil {
		return stats
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 3 {
			name := parts[0]
			// Filter specifically for Kamal's generated container names
			if name == "hack-go-thon-db" || strings.HasPrefix(name, "hack-go-thon-web") {
				stats = append(stats, ContainerStat{
					Name:    name,
					MemUsed: strings.TrimSpace(parts[1]),
					MemPerc: strings.TrimSpace(parts[2]),
				})
			}
		}
	}
	return stats
}
