// Package monitor provides CPU and user session monitoring capabilities
package monitor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPUMonitor tracks CPU usage over time using a rolling window
type CPUMonitor struct {
	mu       sync.RWMutex
	samples  []cpuSample
	interval time.Duration
}

// cpuSample represents a single CPU usage measurement
type cpuSample struct {
	timestamp time.Time
	usage     float64
}

// cpuStats holds raw CPU statistics from /proc/stat
type cpuStats struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
}

// NewCPUMonitor creates a new CPU monitor with the specified sampling interval
func NewCPUMonitor(samplingInterval time.Duration) *CPUMonitor {
	return &CPUMonitor{
		samples:  make([]cpuSample, 0),
		interval: samplingInterval,
	}
}

// Start begins CPU monitoring in a background goroutine
func (m *CPUMonitor) Start(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		// Take initial reading
		m.takeSample()

		for {
			select {
			case <-ticker.C:
				m.takeSample()
			case <-stopCh:
				return
			}
		}
	}()
}

// takeSample reads current CPU usage and adds it to the sample history
func (m *CPUMonitor) takeSample() {
	usage, err := m.getCurrentCPUUsage()
	if err != nil {
		fmt.Printf("Error reading CPU usage: %v\n", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.samples = append(m.samples, cpuSample{
		timestamp: time.Now(),
		usage:     usage,
	})

	// Keep only samples from the last 2 hours to avoid memory growth
	cutoff := time.Now().Add(-2 * time.Hour)
	filtered := make([]cpuSample, 0, len(m.samples))
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			filtered = append(filtered, s)
		}
	}
	m.samples = filtered
}

// getCurrentCPUUsage calculates current CPU usage percentage by comparing
// two readings of /proc/stat
func (m *CPUMonitor) getCurrentCPUUsage() (float64, error) {
	stats1, err := readCPUStats()
	if err != nil {
		return 0, err
	}

	time.Sleep(100 * time.Millisecond)

	stats2, err := readCPUStats()
	if err != nil {
		return 0, err
	}

	// Calculate deltas
	totalDelta := float64((stats2.user + stats2.nice + stats2.system + stats2.idle + stats2.iowait) -
		(stats1.user + stats1.nice + stats1.system + stats1.idle + stats1.iowait))
	idleDelta := float64((stats2.idle + stats2.iowait) - (stats1.idle + stats1.iowait))

	if totalDelta == 0 {
		return 0, nil
	}

	usage := ((totalDelta - idleDelta) / totalDelta) * 100
	return usage, nil
}

// readCPUStats reads CPU statistics from /proc/stat
func readCPUStats() (*cpuStats, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return nil, fmt.Errorf("unexpected /proc/stat format")
			}

			stats := &cpuStats{}
			stats.user, _ = strconv.ParseUint(fields[1], 10, 64)
			stats.nice, _ = strconv.ParseUint(fields[2], 10, 64)
			stats.system, _ = strconv.ParseUint(fields[3], 10, 64)
			stats.idle, _ = strconv.ParseUint(fields[4], 10, 64)
			if len(fields) > 5 {
				stats.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
			}
			return stats, nil
		}
	}

	return nil, fmt.Errorf("cpu line not found in /proc/stat")
}

// IsBelowThreshold checks if CPU usage has been below the threshold
// for the specified duration
func (m *CPUMonitor) IsBelowThreshold(threshold int, minutes int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	// Get samples within the time window
	var samplesInWindow []cpuSample
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			samplesInWindow = append(samplesInWindow, s)
		}
	}

	// Need at least some samples to make a decision
	// Minimum: 1 sample per minute for the duration
	minSamples := minutes / 2
	if minSamples < 1 {
		minSamples = 1
	}
	if len(samplesInWindow) < minSamples {
		fmt.Printf("CPU check: insufficient samples (%d/%d) for %d minute window\n",
			len(samplesInWindow), minSamples, minutes)
		return false
	}

	// Check if all samples are below threshold
	for _, s := range samplesInWindow {
		if s.usage >= float64(threshold) {
			fmt.Printf("CPU check: usage %.2f%% >= threshold %d%% at %s\n",
				s.usage, threshold, s.timestamp.Format(time.RFC3339))
			return false
		}
	}

	fmt.Printf("CPU check: all %d samples below %d%% for last %d minutes\n",
		len(samplesInWindow), threshold, minutes)
	return true
}

// GetCurrentUsage returns the most recent CPU usage reading
func (m *CPUMonitor) GetCurrentUsage() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.samples) == 0 {
		return 0
	}
	return m.samples[len(m.samples)-1].usage
}
