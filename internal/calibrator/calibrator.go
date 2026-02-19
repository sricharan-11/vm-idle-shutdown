// Package calibrator provides automatic CPU threshold calibration
// using statistical analysis of historical CPU usage samples.
package calibrator

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"idleshutdown/internal/monitor"

	"gopkg.in/ini.v1"
)

const (
	// InitialLookback is how long the agent accumulates data before first calibration.
	InitialLookback = 24 * time.Hour
	// WeeklyLookback is the window analyzed for weekly recalibration.
	WeeklyLookback = 72 * time.Hour
	// CalibrationInterval is how often the weekly re-calibration runs.
	CalibrationInterval = 7 * 24 * time.Hour
	// ThresholdBuffer is added to idle baseline when setting cpu_threshold.
	ThresholdBuffer = 3.0
	// MinThreshold ensures cpu_threshold never goes dangerously low.
	MinThreshold = 5.0
	// stddevTight is the first-pass standard deviation target.
	stddevTight = 1.0
	// stddevLoose is the fallback if no tight windows are found.
	stddevLoose = 2.0
	// windowDuration is the sliding window size for idle detection.
	windowDuration = 30 * time.Minute
	// minWindowSamples is the minimum samples needed in a sliding window.
	minWindowSamples = 5
	// minCalibSamples is the minimum total samples for a calibration run.
	minCalibSamples = 10
)

// state tracks calibration history persisted to disk.
type state struct {
	InitialDone   bool
	LastCalibTime time.Time
	StartTime     time.Time
}

// Calibrator manages automatic CPU threshold detection.
type Calibrator struct {
	configPath string
	statePath  string
	state      state
}

// New creates a new Calibrator, loading any persisted state.
func New(configPath, statePath string) *Calibrator {
	c := &Calibrator{
		configPath: configPath,
		statePath:  statePath,
	}
	c.loadState()
	if c.state.StartTime.IsZero() {
		c.state.StartTime = time.Now()
		if err := c.saveState(); err != nil {
			log.Printf("[Calibrator] Warning: could not persist initial state: %v", err)
		}
	}
	return c
}

// ShouldRunInitial returns true if 24h of data has been collected and
// initial calibration hasn't been done yet.
func (c *Calibrator) ShouldRunInitial() bool {
	return !c.state.InitialDone && time.Since(c.state.StartTime) >= InitialLookback
}

// ShouldRunWeekly returns true if a week has passed since last calibration.
func (c *Calibrator) ShouldRunWeekly() bool {
	return c.state.InitialDone && time.Since(c.state.LastCalibTime) >= CalibrationInterval
}

// Run performs calibration using the provided samples and the given lookback window.
// It updates the config file and returns the new threshold.
func (c *Calibrator) Run(samples []monitor.CPUSample, lookback time.Duration) (float64, error) {
	cutoff := time.Now().Add(-lookback)
	var window []monitor.CPUSample
	for _, s := range samples {
		if s.Timestamp.After(cutoff) {
			window = append(window, s)
		}
	}

	if len(window) < minCalibSamples {
		return 0, fmt.Errorf("insufficient samples (%d, need %d) for calibration", len(window), minCalibSamples)
	}

	log.Printf("[Calibrator] Calibrating on %d samples from last %s", len(window), lookback)

	idleBaseline, err := findIdleBaseline(window)
	if err != nil {
		return 0, fmt.Errorf("calibration failed: %w", err)
	}

	newThreshold := math.Max(idleBaseline+ThresholdBuffer, MinThreshold)
	rounded := math.Round(newThreshold)

	log.Printf("[Calibrator] Idle baseline=%.2f%%, New threshold=%.0f%%", idleBaseline, rounded)

	if err := c.updateConfigThreshold(rounded); err != nil {
		return 0, fmt.Errorf("failed to update config: %w", err)
	}

	c.state.InitialDone = true
	c.state.LastCalibTime = time.Now()
	if err := c.saveState(); err != nil {
		log.Printf("[Calibrator] Warning: could not persist state: %v", err)
	}

	return rounded, nil
}

// findIdleBaseline scans samples with a sliding window to find stable idle periods.
// Tries tight stddev first, then falls back to a looser threshold.
func findIdleBaseline(samples []monitor.CPUSample) (float64, error) {
	for _, maxStddev := range []float64{stddevTight, stddevLoose} {
		if baseline, found := slidingWindowMin(samples, maxStddev); found {
			log.Printf("[Calibrator] Found idle baseline=%.2f%% with stddev < %.1f%%", baseline, maxStddev)
			return baseline, nil
		}
		log.Printf("[Calibrator] No stable windows with stddev < %.1f%%, loosening...", maxStddev)
	}
	return 0, fmt.Errorf("no stable idle windows found (stddev always > %.1f%%)", stddevLoose)
}

// slidingWindowMin slides a 30-min window over samples and returns the minimum
// average from windows whose standard deviation is within the given limit.
func slidingWindowMin(samples []monitor.CPUSample, maxStddev float64) (float64, bool) {
	minAvg := math.MaxFloat64
	found := false

	for i := range samples {
		end := samples[i].Timestamp.Add(windowDuration)
		var winValues []float64
		for j := i; j < len(samples) && samples[j].Timestamp.Before(end); j++ {
			winValues = append(winValues, samples[j].Usage)
		}
		if len(winValues) < minWindowSamples {
			continue
		}

		avg := mean(winValues)
		if stddev(winValues, avg) < maxStddev && avg < minAvg {
			minAvg = avg
			found = true
		}
	}

	return minAvg, found
}

func mean(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stddev(vals []float64, avg float64) float64 {
	sum := 0.0
	for _, v := range vals {
		d := v - avg
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(vals)))
}

// updateConfigThreshold writes the new cpu_threshold to the INI file.
func (c *Calibrator) updateConfigThreshold(threshold float64) error {
	cfg, err := ini.Load(c.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg.Section("monitoring").Key("cpu_threshold").SetValue(fmt.Sprintf("%.0f", threshold))

	if err := cfg.SaveTo(c.configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	log.Printf("[Calibrator] Wrote cpu_threshold=%.0f%% to %s", threshold, c.configPath)
	return nil
}

// loadState reads calibration state from the state file.
func (c *Calibrator) loadState() {
	file, err := os.Open(c.statePath)
	if err != nil {
		return // File doesn't exist yet — first run
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

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

// saveState writes calibration state to the state file.
func (c *Calibrator) saveState() error {
	file, err := os.Create(c.statePath)
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}
	defer file.Close()

	lines := []string{
		fmt.Sprintf("initial_done=%s", strconv.FormatBool(c.state.InitialDone)),
		fmt.Sprintf("last_calib_time=%s", c.state.LastCalibTime.Format(time.RFC3339)),
		fmt.Sprintf("start_time=%s", c.state.StartTime.Format(time.RFC3339)),
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(file, l); err != nil {
			return fmt.Errorf("write state: %w", err)
		}
	}
	return nil
}
