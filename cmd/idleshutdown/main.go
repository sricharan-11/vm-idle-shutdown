// IdleShutdown Agent - A systemd service for RHEL VMs that monitors
// CPU usage and logged-in users to automatically shut down idle VMs.
package main

import (
	"flag"
	"fmt"
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
	defaultsPath := flag.String("defaults", config.DefaultDefaultsPath, "Path to defaults file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no actual shutdown)")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("===========================================")
	log.Println("  IdleShutdown Agent Starting")
	log.Println("===========================================")
	log.Printf("Config path:   %s", *configPath)
	log.Printf("Defaults path: %s", *defaultsPath)
	log.Printf("Dry-run mode:  %v", *dryRun)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	log.Printf("Configuration loaded: %s", cfg)

	// Load calibration defaults
	calibCfg, err := config.LoadDefaults(*defaultsPath)
	if err != nil {
		log.Fatalf("Error loading defaults: %v", err)
	}
	log.Printf("Calibration defaults loaded: Initial=%v hours, Recalib=%v days, Lookback=%v hours",
		calibCfg.InitialTrackingHours, calibCfg.RecalibrationIntervalDays, calibCfg.RecalibrationTrackingHours)

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

	// Handle auto/manual mode
	var calib *calibrator.Calibrator

	if cfg.AutoMode {
		log.Println("Mode: AUTO — cpu_threshold is absent (commented out)")

		calib = calibrator.New(*configPath, config.DefaultStatePath, calibCfg)

		if calib.IsInLearningPhase() {
			remaining := calib.LearningTimeRemaining()
			log.Printf("  📊 Learning phase: %s remaining — shutdown evaluation PAUSED",
				formatDuration(remaining))
			log.Printf("  Initial calibration: after %s of data", calibCfg.InitialLookback())
			calib.WriteLearningBanner()
		} else {
			threshold := calib.CurrentThreshold()
			cfg.CPUThreshold = threshold
			log.Printf("  Calibrated threshold: %d%%", threshold)
			log.Printf("  Recalibration: every %s using %s of data",
				calibCfg.RecalibrationInterval(), calibCfg.RecalibrationLookback())
		}

		go runCalibrationLoop(calib, calibCfg, cpuMonitor, stopCh)
	} else {
		log.Printf("Mode: MANUAL — cpu_threshold = %d%% (set in config.ini)", cfg.CPUThreshold)
		// Strip any leftover auto-mode banner
		calibrator.StripBanner(*configPath)
	}

	// Main evaluation loop
	ticker := time.NewTicker(evaluationInterval)
	defer ticker.Stop()

	log.Println("Entering evaluation loop...")

	for {
		select {
		case sig := <-sigCh:
			log.Printf("Received signal %v, shutting down gracefully...", sig)
			close(stopCh)
			log.Println("IdleShutdown Agent stopped.")
			return

		case <-ticker.C:
			// Reload config each tick
			latestCfg, reloadErr := config.Load(*configPath)
			if reloadErr != nil {
				log.Printf("Warning: config reload failed: %v — using last known config", reloadErr)
			} else {
				cfg = latestCfg
			}

			// In auto mode during learning phase: skip eval
			if cfg.AutoMode && calib != nil && calib.IsInLearningPhase() {
				remaining := calib.LearningTimeRemaining()
				log.Printf("Learning phase: %s remaining — skipping shutdown evaluation",
					formatDuration(remaining))
				continue
			}

			// In auto mode: use threshold from calibration state
			if cfg.AutoMode && calib != nil {
				cfg.CPUThreshold = calib.CurrentThreshold()
			}

			evaluateShutdownCondition(cfg, cpuMonitor, userMonitor, shutdownExec)
		}
	}
}

// runCalibrationLoop runs initial and periodic recalibration.
func runCalibrationLoop(
	calib *calibrator.Calibrator,
	calibCfg *config.CalibrationConfig,
	cpuMon *monitor.CPUMonitor,
	stopCh <-chan struct{},
) {
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
					calibCfg.InitialLookback(), len(samples))
				threshold, err := calib.Run(samples, calibCfg.InitialLookback())
				if err != nil {
					log.Printf("[Calibrator] Initial calibration failed: %v", err)
					continue
				}
				log.Printf("[Calibrator] ✅ Initial calibration complete: cpu_threshold = %.0f%%", threshold)
				restartService()

			} else if calib.ShouldRunWeekly() {
				log.Printf("[Calibrator] Weekly recalibration due — %s lookback (%d samples)...",
					calibCfg.RecalibrationLookback(), len(samples))
				threshold, err := calib.Run(samples, calibCfg.RecalibrationLookback())
				if err != nil {
					log.Printf("[Calibrator] Weekly recalibration failed: %v", err)
					continue
				}
				log.Printf("[Calibrator] ✅ Weekly recalibration complete: cpu_threshold = %.0f%%", threshold)
				restartService()

			} else if calib.IsInLearningPhase() {
				// Refresh the learning banner with updated remaining time
				calib.WriteLearningBanner()
			}
		}
	}
}

// restartService restarts the IdleShutdown systemd service.
func restartService() {
	log.Println("[Calibrator] Restarting service to apply new threshold...")
	cmd := exec.Command("systemctl", "restart", "IdleShutdown")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[Calibrator] Warning: service restart failed: %v — %s", err, string(output))
	}
}

// evaluateShutdownCondition checks if both idle conditions are met.
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
		log.Printf("🛑 SHUTDOWN TRIGGERED — CPU < %d%% for %d min, 0 users for %d min",
			cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

		reason := "VM idle — CPU below threshold and no users logged in"
		if err := shutdownExec.Shutdown(reason); err != nil {
			log.Printf("ERROR: shutdown command failed: %v", err)
		}
	}
}

// formatDuration returns a human-readable duration like "23h 14m".
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
