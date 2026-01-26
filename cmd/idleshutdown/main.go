// IdleShutdown Agent - A systemd service for RHEL VMs that monitors
// CPU usage and logged-in users to automatically shut down idle VMs.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"idleshutdown/internal/config"
	"idleshutdown/internal/monitor"
	"idleshutdown/internal/shutdown"
)

const (
	// Sampling interval for CPU and user monitoring (30 seconds)
	samplingInterval = 30 * time.Second
	// Evaluation interval for shutdown decision (1 minute)
	evaluationInterval = 1 * time.Minute
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

	// Main evaluation loop
	fmt.Println("Starting evaluation loop...")
	fmt.Printf("Will check: CPU < %d%% for %d min AND 0 users for %d min\n",
		cfg.CPUThreshold, cfg.CPUCheckMinutes, cfg.UserCheckMinutes)

	ticker := time.NewTicker(evaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case sig := <-sigCh:
			fmt.Printf("\nReceived signal %v, shutting down gracefully...\n", sig)
			close(stopCh)
			fmt.Println("IdleShutdown Agent stopped.")
			return

		case <-ticker.C:
			evaluateShutdownCondition(cfg, cpuMonitor, userMonitor, shutdownExec)
		}
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
