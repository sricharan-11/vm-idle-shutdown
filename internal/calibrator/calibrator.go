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

	"idleshutdown/internal/config"
	"idleshutdown/internal/monitor"
)

const (
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

	// Banner markers for config.ini
	bannerStart = "# ┌──────────────────────────────────────────────────────────┐"
	bannerEnd   = "# └──────────────────────────────────────────────────────────┘"
)

// State tracks calibration history persisted to disk.
type State struct {
	InitialDone      bool
	LastCalibTime    time.Time
	StartTime        time.Time
	CurrentThreshold float64
	IdleBaseline     float64
}

// Calibrator manages automatic CPU threshold detection.
type Calibrator struct {
	configPath string
	statePath  string
	calibCfg   *config.CalibrationConfig
	state      State
}

// New creates a new Calibrator with configurable timings.
func New(configPath, statePath string, calibCfg *config.CalibrationConfig) *Calibrator {
	c := &Calibrator{
		configPath: configPath,
		statePath:  statePath,
		calibCfg:   calibCfg,
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

// IsInLearningPhase returns true if we're still collecting initial data.
func (c *Calibrator) IsInLearningPhase() bool {
	return !c.state.InitialDone
}

// LearningTimeRemaining returns how much time is left in the learning phase.
func (c *Calibrator) LearningTimeRemaining() time.Duration {
	elapsed := time.Since(c.state.StartTime)
	remaining := c.calibCfg.InitialLookback() - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CurrentThreshold returns the last calibrated threshold, or 0 if not yet calibrated.
func (c *Calibrator) CurrentThreshold() int {
	return int(math.Round(c.state.CurrentThreshold))
}

// ShouldRunInitial returns true if the initial tracking time has elapsed.
func (c *Calibrator) ShouldRunInitial() bool {
	return !c.state.InitialDone && time.Since(c.state.StartTime) >= c.calibCfg.InitialLookback()
}

// ShouldRunWeekly returns true if the recalibration interval has elapsed.
func (c *Calibrator) ShouldRunWeekly() bool {
	return c.state.InitialDone && time.Since(c.state.LastCalibTime) >= c.calibCfg.RecalibrationInterval()
}

// Run performs calibration and returns the new threshold.
func (c *Calibrator) Run(samples []monitor.CPUSample, lookback time.Duration) (float64, error) {
	cutoff := time.Now().Add(-lookback)
	var window []monitor.CPUSample
	for _, s := range samples {
		if s.Timestamp.After(cutoff) {
			window = append(window, s)
		}
	}

	if len(window) < minCalibSamples {
		return 0, fmt.Errorf("insufficient samples (%d, need %d)", len(window), minCalibSamples)
	}

	log.Printf("[Calibrator] Calibrating on %d samples from last %s", len(window), lookback)

	idleBaseline, err := findIdleBaseline(window)
	if err != nil {
		return 0, fmt.Errorf("calibration failed: %w", err)
	}

	newThreshold := math.Max(idleBaseline+ThresholdBuffer, MinThreshold)
	rounded := math.Round(newThreshold)

	log.Printf("[Calibrator] Idle baseline=%.2f%%, New threshold=%.0f%%", idleBaseline, rounded)

	// Update state
	c.state.InitialDone = true
	c.state.LastCalibTime = time.Now()
	c.state.CurrentThreshold = rounded
	c.state.IdleBaseline = idleBaseline
	if err := c.saveState(); err != nil {
		log.Printf("[Calibrator] Warning: could not persist state: %v", err)
	}

	// Write banner to config.ini
	c.WriteCalibratedBanner()

	return rounded, nil
}

// WriteLearningBanner writes a learning-phase banner into config.ini.
func (c *Calibrator) WriteLearningBanner() {
	remaining := c.LearningTimeRemaining()
	hours := int(remaining.Hours())
	mins := int(remaining.Minutes()) % 60

	banner := []string{
		bannerStart,
		fmt.Sprintf("# │  🔍 LEARNING — collecting CPU data (%dh %dm remaining)%s│",
			hours, mins, padTo(52, fmt.Sprintf("🔍 LEARNING — collecting CPU data (%dh %dm remaining)", hours, mins))),
		"# │  Shutdown evaluation is PAUSED until learning completes  │",
		"# │  To set manually, uncomment cpu_threshold below          │",
		bannerEnd,
	}
	c.writeBannerToConfig(banner)
}

// WriteCalibratedBanner writes the auto-managed banner with calibration metadata.
func (c *Calibrator) WriteCalibratedBanner() {
	nextCalib := c.state.LastCalibTime.Add(c.calibCfg.RecalibrationInterval())

	banner := []string{
		bannerStart,
		"# │  ⚡ AUTO-MANAGED — to set manually, uncomment below      │",
		fmt.Sprintf("# │  Last calibrated : %-38s│", c.state.LastCalibTime.Format("2006-01-02 15:04 UTC")),
		fmt.Sprintf("# │  Idle baseline   : %-38s│", fmt.Sprintf("%.1f%%", c.state.IdleBaseline)),
		fmt.Sprintf("# │  Current value   : %-38s│", fmt.Sprintf("%.0f%% (active)", c.state.CurrentThreshold)),
		fmt.Sprintf("# │  Next calibration: %-38s│", "~"+nextCalib.Format("2006-01-02")),
		bannerEnd,
	}
	c.writeBannerToConfig(banner)
}

// StripBanner removes any existing banner from config.ini (used when switching to manual).
func StripBanner(configPath string) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	// Normalize line endings to \n
	text := string(content)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	
	var result []string
	inBanner := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == bannerStart {
			inBanner = true
			continue
		}
		if inBanner {
			if trimmed == bannerEnd {
				inBanner = false
				continue
			}
			continue // skip banner content
		}
		result = append(result, line)
	}

	// Write back using LF only
	cleaned := strings.Join(result, "\n")
	if err := os.WriteFile(configPath, []byte(cleaned), 0644); err != nil {
		log.Printf("[Calibrator] Warning: could not strip banner: %v", err)
	} else {
		log.Println("[Calibrator] Stripped auto-mode banner (manual mode active)")
	}
}

// writeBannerToConfig replaces any existing banner in config.ini or inserts one
// before the cpu_threshold line.
func (c *Calibrator) writeBannerToConfig(banner []string) {
	content, err := os.ReadFile(c.configPath)
	if err != nil {
		log.Printf("[Calibrator] Warning: could not read config for banner: %v", err)
		return
	}

	// Normalize line endings to \n
	text := string(content)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	var result []string
	inBanner := false
	bannerInserted := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip existing banner
		if trimmed == bannerStart {
			inBanner = true
			continue
		}
		if inBanner {
			if trimmed == bannerEnd {
				inBanner = false
			}
			continue
		}

		// Insert new banner before the cpu_threshold line (commented or not)
		if !bannerInserted && (trimmed == "# cpu_threshold" ||
			strings.HasPrefix(trimmed, "# cpu_threshold =") ||
			strings.HasPrefix(trimmed, "# cpu_threshold=") ||
			strings.HasPrefix(trimmed, "cpu_threshold")) {
			result = append(result, banner...)
			bannerInserted = true
		}

		result = append(result, line)
	}

	// If no cpu_threshold line found, append banner at end
	if !bannerInserted {
		result = append(result, banner...)
		result = append(result, "# cpu_threshold = 25")
	}

	// Write back using LF only
	output := strings.Join(result, "\n")
	if err := os.WriteFile(c.configPath, []byte(output), 0644); err != nil {
		log.Printf("[Calibrator] Warning: could not write banner: %v", err)
	}
}

// padTo returns padding spaces to align banner content to a fixed width.
func padTo(width int, content string) string {
	// This is used for the learning banner dynamic line
	pad := width - len(content)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad)
}

// --- Statistical helpers ---

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

// --- State persistence ---

func (c *Calibrator) loadState() {
	file, err := os.Open(c.statePath)
	if err != nil {
		return
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
		case "current_threshold":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				c.state.CurrentThreshold = v
			}
		case "idle_baseline":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				c.state.IdleBaseline = v
			}
		}
	}
}

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
		fmt.Sprintf("current_threshold=%.0f", c.state.CurrentThreshold),
		fmt.Sprintf("idle_baseline=%.2f", c.state.IdleBaseline),
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(file, l); err != nil {
			return fmt.Errorf("write state: %w", err)
		}
	}
	return nil
}
