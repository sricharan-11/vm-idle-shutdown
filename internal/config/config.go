// Package config provides configuration loading from INI files.
package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"gopkg.in/ini.v1"
)

// Default configuration values
const (
	DefaultCPUCheckMinutes  = 60
	DefaultUserCheckMinutes = 60
	DefaultCPUThreshold     = 25
	DefaultConfigPath       = "/etc/idleshutdown/config.ini"
	DefaultDefaultsPath     = "/etc/idleshutdown/default.ini"
	DefaultStatePath        = "/etc/idleshutdown/calibration.state"
)

// Config holds the agent configuration parameters.
type Config struct {
	CPUCheckMinutes  int
	UserCheckMinutes int

	// CPUThreshold is the CPU usage percentage threshold.
	// In manual mode it comes from config.ini.
	// In auto mode it comes from calibration.state (set by calibrator).
	CPUThreshold int

	// AutoMode is true when cpu_threshold is commented out or absent in config.ini,
	// meaning the agent self-calibrates the threshold.
	AutoMode bool
}

// CalibrationConfig holds the calibration timing parameters from default.ini.
type CalibrationConfig struct {
	// Use float64 to allow fractional hours (e.g. 0.5 hours for testing)
	InitialTrackingHours       float64
	RecalibrationIntervalDays  float64
	RecalibrationTrackingHours float64
}

// InitialLookback returns the initial tracking duration.
func (c *CalibrationConfig) InitialLookback() time.Duration {
	return time.Duration(c.InitialTrackingHours * float64(time.Hour))
}

// RecalibrationInterval returns how often recalibration happens.
func (c *CalibrationConfig) RecalibrationInterval() time.Duration {
	return time.Duration(c.RecalibrationIntervalDays * 24 * float64(time.Hour))
}

// RecalibrationLookback returns the data window for recalibration.
func (c *CalibrationConfig) RecalibrationLookback() time.Duration {
	return time.Duration(c.RecalibrationTrackingHours * float64(time.Hour))
}

// Load reads configuration from the INI file at the specified path.
// If cpu_threshold key is absent or commented out → AutoMode = true.
func Load(path string) (*Config, error) {
	cfg := &Config{
		CPUCheckMinutes:  DefaultCPUCheckMinutes,
		UserCheckMinutes: DefaultUserCheckMinutes,
		CPUThreshold:     DefaultCPUThreshold,
		AutoMode:         true, // Default: auto mode (threshold absent)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("Config file not found at %s, using defaults (auto mode)", path)
		return cfg, nil
	}

	iniFile, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	section := iniFile.Section("monitoring")

	if key, err := section.GetKey("cpu_check_minutes"); err == nil {
		if val, err := key.Int(); err == nil && val > 0 {
			cfg.CPUCheckMinutes = val
		}
	}

	if key, err := section.GetKey("user_check_minutes"); err == nil {
		if val, err := key.Int(); err == nil && val > 0 {
			cfg.UserCheckMinutes = val
		}
	}

	// The key insight: if cpu_threshold exists (uncommented) → manual mode.
	// If it's absent (commented out with #) → auto mode.
	if key, err := section.GetKey("cpu_threshold"); err == nil {
		if val, err := key.Int(); err == nil && val >= 0 && val <= 100 {
			cfg.CPUThreshold = val
			cfg.AutoMode = false // Uncommented = manual
		}
	}

	return cfg, nil
}

// LoadDefaults reads calibration timing parameters from default.ini.
func LoadDefaults(path string) (*CalibrationConfig, error) {
	defaults := &CalibrationConfig{
		InitialTrackingHours:       24.0,
		RecalibrationIntervalDays:  7.0,
		RecalibrationTrackingHours: 72.0,
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("Defaults file not found at %s, using built-in defaults", path)
		return defaults, nil
	}

	iniFile, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load defaults file: %w", err)
	}

	section := iniFile.Section("calibration")

	if key, err := section.GetKey("initial_tracking_hours"); err == nil {
		if val, err := key.Float64(); err == nil && val > 0 {
			defaults.InitialTrackingHours = val
		}
	}

	if key, err := section.GetKey("recalibration_interval_days"); err == nil {
		if val, err := key.Float64(); err == nil && val > 0 {
			defaults.RecalibrationIntervalDays = val
		}
	}

	if key, err := section.GetKey("recalibration_tracking_hours"); err == nil {
		if val, err := key.Float64(); err == nil && val > 0 {
			defaults.RecalibrationTrackingHours = val
		}
	}

	return defaults, nil
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	mode := "MANUAL"
	if c.AutoMode {
		mode = "AUTO"
	}
	return fmt.Sprintf("Config{CPUCheck: %dmin, UserCheck: %dmin, Threshold: %d%%, Mode: %s}",
		c.CPUCheckMinutes, c.UserCheckMinutes, c.CPUThreshold, mode)
}
