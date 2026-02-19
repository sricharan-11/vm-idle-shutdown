// IdleShutdown Agent - A systemd service for RHEL VMs that monitors
// CPU usage and logged-in users to automatically shut down idle VMs.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"idleshutdown/internal/calibrator"
	"idleshutdown/internal/config"
	"idleshutdown/internal/monitor"
	"idleshutdown/internal/shutdown"
)

const (
	// Sampling interval for CPU and user monitoring (30 seconds)
	samplingInterval = 30 * time.Second
	// Evaluation interval for shutdown decision (1 minute)
	evaluationInterval = 1 * time.Minute
	// Calibration check interval (check every hour if calibration is due)
	calibrationCheckInterval = 1 * time.Hour
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", config.DefaultConfigPath, "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no actual shutdown)")
	flag.Parse()

	fmt.Println("===========================================")
	fmt.Println("  IdleShutdown Agent Starting")
	fmt.Println("===========================================")
	fmt.Printf("Config path: %s\n", *configPath)
	fmt.Printf("Dry-run mode: %v\n", *dryRun)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Configuration loaded: %s\n", cfg)

	// Create stop channel for graceful shutdown
	stopCh := make(chan struct{})

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize monitors
	cpuMonitor := monitor.NewCPUMonitor(samplingInterval)
	userMonitor := monitor.NewUserMonitor(samplingInterval)

	// Start monitors
	fmt.Println("Starting CPU monitor...")
	cpuMonitor.Start(stopCh)

	fmt.Println("Starting User monitor...")
	userMonitor.Start(stopCh)

	// Initialize shutdown executor
	shutdownExec := shutdown.NewExecutor(*dryRun)

	// Start auto-calibration goroutine if in auto mode
	if cfg.IsAutoMode() {
		fmt.Println("CPU mode: AUTO - Will self-calibrate idle CPU threshold")
		fmt.Printf("  Initial calibration: after 24h of data\n")
		fmt.Printf("  Weekly recalibration: every 7 days using 72h of data\n")
		go runCalibrationLoop(*configPath, cpuMonitor, stopCh)
	} else {
		fmt.Printf("CPU mode: MANUAL - Using fixed threshold of %d%%\n", cfg.CPUThreshold)
	}

	// Main evaluation loop
	ticker := time.NewTicker(evaluationInterval)
	defer ticker.Stop()

	fmt.Println("Starting evaluation loop...")
	fmt.Printf("Shutdown condition: CPU < %d%% for %d min AND 0 users for %d min\n",
		cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

	for {
		select {
		case sig := <-sigCh:
			fmt.Printf("\nReceived signal %v, shutting down gracefully...\n", sig)
			close(stopCh)
			fmt.Println("IdleShutdown Agent stopped.")
			return

		case <-ticker.C:
			// Reload config each tick so calibration threshold updates take effect
			latestCfg, err := config.Load(*configPath)
			if err != nil {
				fmt.Printf("Warning: could not reload config: %v - using last known config\n", err)
				latestCfg = cfg
			}
			cfg = latestCfg
			evaluateShutdownCondition(cfg, cpuMonitor, userMonitor, shutdownExec)
		}
	}
}

// runCalibrationLoop runs the auto-calibration process in a background goroutine.
// It waits for 24h of data, runs initial calibration, then recalibrates weekly with 72h lookback.
func runCalibrationLoop(configPath string, cpuMon *monitor.CPUMonitor, stopCh <-chan struct{}) {
	calib := calibrator.New(configPath, config.DefaultStatePath)
	ticker := time.NewTicker(calibrationCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			samples := cpuMon.GetSamples()

			if calib.ShouldRunInitial() {
				fmt.Println("[Calibrator] 24h elapsed - running initial calibration...")
				threshold, err := calib.Run(samples, calibrator.InitialLookback)
				if err != nil {
					fmt.Printf("[Calibrator] Initial calibration failed: %v\n", err)
					continue
				}
				fmt.Printf("[Calibrator] Initial calibration complete: cpu_threshold set to %.0f%%\n", threshold)
				restartService()

			} else if calib.ShouldRunWeekly() {
				fmt.Println("[Calibrator] Weekly recalibration due - running with 72h lookback...")
				threshold, err := calib.Run(samples, calibrator.WeeklyLookback)
				if err != nil {
					fmt.Printf("[Calibrator] Weekly recalibration failed: %v\n", err)
					continue
				}
				fmt.Printf("[Calibrator] Weekly recalibration complete: cpu_threshold set to %.0f%%\n", threshold)
				restartService()
			}
		}
	}
}

// restartService restarts the IdleShutdown systemd service to pick up the new config.
func restartService() {
	fmt.Println("[Calibrator] Restarting service to apply new threshold...")
	cmd := exec.Command("systemctl", "restart", "IdleShutdown")
	if err := cmd.Run(); err != nil {
		fmt.Printf("[Calibrator] Warning: could not restart service: %v\n", err)
	}
}

// evaluateShutdownCondition checks if the shutdown conditions are met
func evaluateShutdownCondition(
	cfg *config.Config,
	cpuMon *monitor.CPUMonitor,
	userMon *monitor.UserMonitor,
	shutdownExec *shutdown.Executor,
) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n[%s] Evaluating shutdown conditions...\n", timestamp)
	fmt.Printf("  Current CPU: %.2f%%, Current Users: %d\n",
		cpuMon.GetCurrentUsage(), userMon.GetCurrentUserCount())

	// Check CPU condition
	cpuBelowThreshold := cpuMon.IsBelowThreshold(cfg.CPUThreshold, cfg.CPUCheckMinutes)
	fmt.Printf("  CPU below %d%% for %d min: %v\n",
		cfg.CPUThreshold, cfg.CPUCheckMinutes, cpuBelowThreshold)

	// Check user condition
	noUsers := userMon.NoUsersLoggedIn(cfg.UserCheckMinutes)
	fmt.Printf("  No users for %d min: %v\n", cfg.UserCheckMinutes, noUsers)

	// Both conditions must be true for shutdown
	if cpuBelowThreshold && noUsers {
		reason := fmt.Sprintf("VM idle - CPU below %d%% for %d minutes and no users logged in for %d minutes",
			cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

		if err := shutdownExec.Shutdown(reason); err != nil {
			fmt.Printf("Error executing shutdown: %v\n", err)
		}
	} else {
		fmt.Println("  Shutdown conditions NOT met, continuing to monitor...")
	}
}
