// Package monitor provides CPU and user session monitoring capabilities.
package monitor

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxSampleRetention is how far back CPU samples are kept in memory.
// This is 72h to support weekly calibration lookback.
const maxSampleRetention = 72 * time.Hour

// CPUMonitor tracks CPU usage over time using a rolling window.
type CPUMonitor struct {
	mu       sync.RWMutex
	samples  []cpuSample
	interval time.Duration
}

// CPUSample is the exported form of a CPU usage reading.
type CPUSample struct {
	Timestamp time.Time
	Usage     float64
}

// cpuSample represents a single CPU usage measurement (internal).
type cpuSample struct {
	timestamp time.Time
	usage     float64
}

// cpuStats holds raw CPU counters from /proc/stat.
type cpuStats struct {
	user    uint64
	nice    uint64
	system  uint64
	idle    uint64
	iowait  uint64
	irq     uint64
	softirq uint64
	steal   uint64
}

// NewCPUMonitor creates a new CPU monitor with the specified sampling interval.
func NewCPUMonitor(samplingInterval time.Duration) *CPUMonitor {
	return &CPUMonitor{
		samples:  make([]cpuSample, 0, 256),
		interval: samplingInterval,
	}
}

// Start begins CPU monitoring in a background goroutine.
func (m *CPUMonitor) Start(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		m.takeSample() // initial reading

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

// takeSample reads current CPU usage and appends it to the rolling buffer.
func (m *CPUMonitor) takeSample() {
	usage, err := m.getCurrentCPUUsage()
	if err != nil {
		log.Printf("Error reading CPU usage: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.samples = append(m.samples, cpuSample{timestamp: now, usage: usage})

	// Prune samples older than retention window
	cutoff := now.Add(-maxSampleRetention)
	start := 0
	for start < len(m.samples) && !m.samples[start].timestamp.After(cutoff) {
		start++
	}
	if start > 0 {
		m.samples = m.samples[start:]
	}
}

// getCurrentCPUUsage calculates current CPU usage percentage by comparing
// two readings of /proc/stat separated by a short interval.
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

	// Calculate deltas — include all non-idle activity
	idle1 := stats1.idle + stats1.iowait
	idle2 := stats2.idle + stats2.iowait

	total1 := stats1.user + stats1.nice + stats1.system + idle1 +
		stats1.irq + stats1.softirq + stats1.steal
	total2 := stats2.user + stats2.nice + stats2.system + idle2 +
		stats2.irq + stats2.softirq + stats2.steal

	totalDelta := float64(total2 - total1)
	idleDelta := float64(idle2 - idle1)

	if totalDelta == 0 {
		return 0, nil
	}

	return ((totalDelta - idleDelta) / totalDelta) * 100, nil
}

// readCPUStats reads CPU statistics from /proc/stat.
func readCPUStats() (*cpuStats, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil, fmt.Errorf("unexpected /proc/stat format: only %d fields", len(fields))
		}

		parse := func(idx int) uint64 {
			if idx >= len(fields) {
				return 0
			}
			v, err := strconv.ParseUint(fields[idx], 10, 64)
			if err != nil {
				log.Printf("Warning: failed to parse /proc/stat field[%d]=%q: %v", idx, fields[idx], err)
				return 0
			}
			return v
		}

		return &cpuStats{
			user:    parse(1),
			nice:    parse(2),
			system:  parse(3),
			idle:    parse(4),
			iowait:  parse(5),
			irq:     parse(6),
			softirq: parse(7),
			steal:   parse(8),
		}, nil
	}

	return nil, fmt.Errorf("cpu line not found in /proc/stat")
}

// IsBelowThreshold checks if CPU usage has been below the threshold
// for the specified duration.
func (m *CPUMonitor) IsBelowThreshold(threshold int, minutes int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	var samplesInWindow []cpuSample
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			samplesInWindow = append(samplesInWindow, s)
		}
	}

	// Require at least 1 sample per 2 minutes of the window
	minSamples := minutes / 2
	if minSamples < 1 {
		minSamples = 1
	}
	if len(samplesInWindow) < minSamples {
		log.Printf("CPU check: insufficient samples (%d/%d) for %d-min window",
			len(samplesInWindow), minSamples, minutes)
		return false
	}

	for _, s := range samplesInWindow {
		if s.usage >= float64(threshold) {
			log.Printf("CPU check: %.2f%% >= %d%% at %s — not idle",
				s.usage, threshold, s.timestamp.Format(time.RFC3339))
			return false
		}
	}

	log.Printf("CPU check: all %d samples below %d%% for last %d min ✓",
		len(samplesInWindow), threshold, minutes)
	return true
}

// GetCurrentUsage returns the most recent CPU usage reading.
func (m *CPUMonitor) GetCurrentUsage() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.samples) == 0 {
		return 0
	}
	return m.samples[len(m.samples)-1].usage
}

// GetSamples returns a snapshot of all retained CPU samples as exported types.
// Used by the auto-calibrator for baseline analysis.
func (m *CPUMonitor) GetSamples() []CPUSample {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]CPUSample, len(m.samples))
	for i, s := range m.samples {
		result[i] = CPUSample{Timestamp: s.timestamp, Usage: s.usage}
	}
	return result
}
