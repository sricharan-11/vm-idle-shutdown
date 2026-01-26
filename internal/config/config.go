// Package config provides configuration loading from INI files
package config

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

// Default configuration values
const (
	DefaultCPUCheckMinutes  = 60
	DefaultUserCheckMinutes = 60
	DefaultCPUThreshold     = 25
	DefaultConfigPath       = "/etc/idleshutdown/config.ini"
)

// Config holds the agent configuration parameters
type Config struct {
	// CPUCheckMinutes is the duration (x) in minutes to monitor CPU usage
	CPUCheckMinutes int
	// UserCheckMinutes is the duration (y) in minutes to check for logged-in users
	UserCheckMinutes int
	// CPUThreshold is the CPU usage percentage threshold (z)
	CPUThreshold int
}

// Load reads configuration from the INI file at the specified path.
// If the file doesn't exist, returns default configuration.
func Load(path string) (*Config, error) {
	cfg := &Config{
		CPUCheckMinutes:  DefaultCPUCheckMinutes,
		UserCheckMinutes: DefaultUserCheckMinutes,
		CPUThreshold:     DefaultCPUThreshold,
	}

	// Check if config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found at %s, using defaults\n", path)
		return cfg, nil
	}

	// Load INI file
	iniFile, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	// Read monitoring section
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

	if key, err := section.GetKey("cpu_threshold"); err == nil {
		if val, err := key.Int(); err == nil && val >= 0 && val <= 100 {
			cfg.CPUThreshold = val
		}
	}

	return cfg, nil
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	return fmt.Sprintf("Config{CPUCheckMinutes: %d, UserCheckMinutes: %d, CPUThreshold: %d%%}",
		c.CPUCheckMinutes, c.UserCheckMinutes, c.CPUThreshold)
}
