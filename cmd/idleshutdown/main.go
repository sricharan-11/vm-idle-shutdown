// IdleShutdown Agent - A systemd service for RHEL VMs that monitors
// CPU usage and logged-in users to automatically shut down idle VMs.
package main

import (
	"flag"
	"log"
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
	// samplingInterval is how often CPU and user readings are taken.
	samplingInterval = 30 * time.Second
	// evaluationInterval is how often the shutdown decision is evaluated.
	evaluationInterval = 1 * time.Minute
	// calibrationCheckInterval is how often we check if a calibration run is due.
	calibrationCheckInterval = 1 * time.Hour
)

func main() {
	configPath := flag.String("config", config.DefaultConfigPath, "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no actual shutdown)")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("===========================================")
	log.Println("  IdleShutdown Agent Starting")
	log.Println("===========================================")
	log.Printf("Config path: %s", *configPath)
	log.Printf("Dry-run mode: %v", *dryRun)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	log.Printf("Configuration loaded: %s", cfg)

	// Create stop channel for graceful shutdown
	stopCh := make(chan struct{})

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize monitors
	cpuMonitor := monitor.NewCPUMonitor(samplingInterval)
	userMonitor := monitor.NewUserMonitor(samplingInterval)

	log.Println("Starting CPU monitor...")
	cpuMonitor.Start(stopCh)

	log.Println("Starting User monitor...")
	userMonitor.Start(stopCh)

	// Initialize shutdown executor
	shutdownExec := shutdown.NewExecutor(*dryRun)

	// Start auto-calibration goroutine if in auto mode
	if cfg.IsAutoMode() {
		log.Println("CPU mode: AUTO — will self-calibrate idle CPU threshold")
		log.Printf("  Initial calibration: after %s of data", calibrator.InitialLookback)
		log.Printf("  Weekly recalibration: every %s using %s of data", calibrator.CalibrationInterval, calibrator.WeeklyLookback)
		go runCalibrationLoop(*configPath, cpuMonitor, stopCh)
	} else {
		log.Printf("CPU mode: MANUAL — using fixed threshold of %d%%", cfg.CPUThreshold)
	}

	// Main evaluation loop
	ticker := time.NewTicker(evaluationInterval)
	defer ticker.Stop()

	log.Println("Entering evaluation loop...")
	log.Printf("Shutdown triggers when: CPU < %d%% for %d min AND 0 users for %d min",
		cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

	for {
		select {
		case sig := <-sigCh:
			log.Printf("Received signal %v, shutting down gracefully...", sig)
			close(stopCh)
			log.Println("IdleShutdown Agent stopped.")
			return

		case <-ticker.C:
			// Reload config each tick so calibration threshold updates take effect
			latestCfg, reloadErr := config.Load(*configPath)
			if reloadErr != nil {
				log.Printf("Warning: config reload failed: %v — using last known config", reloadErr)
			} else {
				cfg = latestCfg
			}
			evaluateShutdownCondition(cfg, cpuMonitor, userMonitor, shutdownExec)
		}
	}
}

// runCalibrationLoop runs the auto-calibration process in a background goroutine.
// It waits for 24h of data, runs initial calibration, then recalibrates weekly.
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
				log.Printf("[Calibrator] %s elapsed — running initial calibration (%d samples)...",
					calibrator.InitialLookback, len(samples))
				threshold, err := calib.Run(samples, calibrator.InitialLookback)
				if err != nil {
					log.Printf("[Calibrator] Initial calibration failed: %v", err)
					continue
				}
				log.Printf("[Calibrator] Initial calibration complete: cpu_threshold = %.0f%%", threshold)
				restartService()

			} else if calib.ShouldRunWeekly() {
				log.Printf("[Calibrator] Weekly recalibration due — %s lookback (%d samples)...",
					calibrator.WeeklyLookback, len(samples))
				threshold, err := calib.Run(samples, calibrator.WeeklyLookback)
				if err != nil {
					log.Printf("[Calibrator] Weekly recalibration failed: %v", err)
					continue
				}
				log.Printf("[Calibrator] Weekly recalibration complete: cpu_threshold = %.0f%%", threshold)
				restartService()
			}
		}
	}
}

// restartService restarts the IdleShutdown systemd service to pick up the new config.
func restartService() {
	log.Println("[Calibrator] Restarting service to apply new threshold...")
	cmd := exec.Command("systemctl", "restart", "IdleShutdown")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[Calibrator] Warning: service restart failed: %v — %s", err, string(output))
	}
}

// evaluateShutdownCondition checks if both idle conditions are met and triggers shutdown.
func evaluateShutdownCondition(
	cfg *config.Config,
	cpuMon *monitor.CPUMonitor,
	userMon *monitor.UserMonitor,
	shutdownExec *shutdown.Executor,
) {
	currentCPU := cpuMon.GetCurrentUsage()
	currentUsers := userMon.GetCurrentUserCount()

	log.Printf("Evaluating: CPU=%.2f%% (threshold=%d%%), Users=%d",
		currentCPU, cfg.CPUThreshold, currentUsers)

	cpuBelowThreshold := cpuMon.IsBelowThreshold(cfg.CPUThreshold, cfg.CPUCheckMinutes)
	noUsers := userMon.NoUsersLoggedIn(cfg.UserCheckMinutes)

	if cpuBelowThreshold && noUsers {
		reason := log.Prefix() // Suppress the prefix just for this log
		_ = reason
		log.Printf("SHUTDOWN TRIGGERED — CPU < %d%% for %d min, 0 users for %d min",
			cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

		shutdownReason := "VM idle — CPU below threshold and no users logged in"
		if err := shutdownExec.Shutdown(shutdownReason); err != nil {
			log.Printf("ERROR: shutdown command failed: %v", err)
		}
	}
}
