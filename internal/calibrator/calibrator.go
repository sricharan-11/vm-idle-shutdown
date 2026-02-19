// Package calibrator provides automatic CPU threshold calibration
// using statistical analysis of historical CPU usage samples.
package calibrator

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"idleshutdown/internal/monitor"
	"gopkg.in/ini.v1"
)

const (
	// InitialLookback is how long the agent accumulates data before first calibration
	InitialLookback = 24 * time.Hour
	// WeeklyLookback is the window analyzed for weekly recalibration
	WeeklyLookback = 72 * time.Hour
	// CalibrationInterval is how often the weekly re-calibration runs
	CalibrationInterval = 7 * 24 * time.Hour
	// ThresholdBuffer is added to idle baseline when setting cpu_threshold
	ThresholdBuffer = 3.0
	// MinThreshold ensures cpu_threshold never goes dangerously low
	MinThreshold = 5.0
	// StddevTightWindow is the first-pass standard deviation target
	StddevTightWindow = 1.0
	// StddevLooseWindow is the fallback if no tight windows are found
	StddevLooseWindow = 2.0
)

// State tracks calibration history
type State struct {
	InitialDone   bool
	LastCalibTime time.Time
	StartTime     time.Time
}

// Calibrator manages automatic CPU threshold detection
type Calibrator struct {
	configPath string
	statePath  string
	state      State
}

// New creates a new Calibrator
func New(configPath, statePath string) *Calibrator {
	c := &Calibrator{
		configPath: configPath,
		statePath:  statePath,
	}
	c.loadState()
	if c.state.StartTime.IsZero() {
		c.state.StartTime = time.Now()
		c.saveState()
	}
	return c
}

// ShouldRunInitial returns true if 24h of data has been collected and initial calibration hasn't been done
func (c *Calibrator) ShouldRunInitial() bool {
	if c.state.InitialDone {
		return false
	}
	return time.Since(c.state.StartTime) >= InitialLookback
}

// ShouldRunWeekly returns true if a week has passed since last calibration
func (c *Calibrator) ShouldRunWeekly() bool {
	if !c.state.InitialDone {
		return false
	}
	return time.Since(c.state.LastCalibTime) >= CalibrationInterval
}

// Run performs calibration using the provided samples and the given lookback window.
// It updates the config threshold and returns the new threshold.
func (c *Calibrator) Run(samples []monitor.CPUSample, lookback time.Duration) (float64, error) {
	cutoff := time.Now().Add(-lookback)
	var window []monitor.CPUSample
	for _, s := range samples {
		if s.Timestamp.After(cutoff) {
			window = append(window, s)
		}
	}

	if len(window) < 10 {
		return 0, fmt.Errorf("insufficient samples (%d) for calibration", len(window))
	}

	fmt.Printf("[Calibrator] Running calibration on %d samples from last %s\n", len(window), lookback)

	idleBaseline, err := findIdleBaseline(window)
	if err != nil {
		return 0, fmt.Errorf("calibration failed: %w", err)
	}

	newThreshold := idleBaseline + ThresholdBuffer
	if newThreshold < MinThreshold {
		newThreshold = MinThreshold
	}

	fmt.Printf("[Calibrator] Idle baseline: %.2f%%, New cpu_threshold: %.2f%%\n", idleBaseline, newThreshold)

	if err := c.updateConfigThreshold(newThreshold); err != nil {
		return 0, fmt.Errorf("failed to update config: %w", err)
	}

	c.state.InitialDone = true
	c.state.LastCalibTime = time.Now()
	c.saveState()

	return newThreshold, nil
}

// findIdleBaseline scans the samples using a sliding window to find stable idle periods.
// Returns the minimum average CPU across all qualifying windows.
func findIdleBaseline(samples []monitor.CPUSample) (float64, error) {
	for _, maxStddev := range []float64{StddevTightWindow, StddevLooseWindow} {
		baseline, found := slidingWindowMin(samples, maxStddev)
		if found {
			fmt.Printf("[Calibrator] Found idle windows with stddev < %.1f%%: baseline=%.2f%%\n", maxStddev, baseline)
			return baseline, nil
		}
		fmt.Printf("[Calibrator] No stable windows with stddev < %.1f%%, loosening...\n", maxStddev)
	}
	return 0, fmt.Errorf("no stable idle windows found even with stddev < %.1f%%", StddevLooseWindow)
}

// slidingWindowMin slides a 30-minute window over the samples, computes stddev for each,
// and returns the minimum average from windows within the stddev limit.
func slidingWindowMin(samples []monitor.CPUSample, maxStddev float64) (float64, bool) {
	windowDuration := 30 * time.Minute
	minAvg := math.MaxFloat64
	found := false

	for i := 0; i < len(samples); i++ {
		end := samples[i].Timestamp.Add(windowDuration)
		var winSamples []float64
		for j := i; j < len(samples) && samples[j].Timestamp.Before(end); j++ {
			winSamples = append(winSamples, samples[j].Usage)
		}
		if len(winSamples) < 5 {
			continue
		}

		avg := mean(winSamples)
		std := stddev(winSamples, avg)

		if std < maxStddev {
			found = true
			if avg < minAvg {
				minAvg = avg
			}
		}
	}

	if !found {
		return 0, false
	}
	return minAvg, true
}

// mean computes the arithmetic mean of a slice
func mean(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// stddev computes the standard deviation of a slice given its mean
func stddev(vals []float64, avg float64) float64 {
	sum := 0.0
	for _, v := range vals {
		diff := v - avg
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(vals)))
}

// updateConfigThreshold writes the new cpu_threshold to the INI file
func (c *Calibrator) updateConfigThreshold(threshold float64) error {
	cfg, err := ini.Load(c.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	section := cfg.Section("monitoring")
	section.Key("cpu_threshold").SetValue(fmt.Sprintf("%.0f", math.Round(threshold)))

	if err := cfg.SaveTo(c.configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("[Calibrator] Updated cpu_threshold to %.0f%% in %s\n", math.Round(threshold), c.configPath)
	return nil
}

// loadState reads calibration state from the state file
func (c *Calibrator) loadState() {
	file, err := os.Open(c.statePath)
	if err != nil {
		return // State file doesn't exist yet, use defaults
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "initial_done":
			c.state.InitialDone = val == "true"
		case "last_calib_time":
			if t, err := time.Parse(time.RFC3339, val); err == nil {
				c.state.LastCalibTime = t
			}
		case "start_time":
			if t, err := time.Parse(time.RFC3339, val); err == nil {
				c.state.StartTime = t
			}
		}
	}
}

// saveState writes calibration state to the state file
func (c *Calibrator) saveState() {
	file, err := os.Create(c.statePath)
	if err != nil {
		fmt.Printf("[Calibrator] Warning: could not save state: %v\n", err)
		return
	}
	defer file.Close()

	lines := []string{
		fmt.Sprintf("initial_done=%s", strconv.FormatBool(c.state.InitialDone)),
		fmt.Sprintf("last_calib_time=%s", c.state.LastCalibTime.Format(time.RFC3339)),
		fmt.Sprintf("start_time=%s", c.state.StartTime.Format(time.RFC3339)),
	}
	for _, l := range lines {
		fmt.Fprintln(file, l)
	}
}
