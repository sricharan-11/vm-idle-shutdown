// Package shutdown provides VM shutdown capabilities
package shutdown

import (
	"fmt"
	"os/exec"
	"time"
)

// Executor handles system shutdown operations
type Executor struct {
	// DryRun if true, logs shutdown action without actually shutting down
	DryRun bool
}

// NewExecutor creates a new shutdown executor
func NewExecutor(dryRun bool) *Executor {
	return &Executor{DryRun: dryRun}
}

// Shutdown initiates a system shutdown
func (e *Executor) Shutdown(reason string) error {
	timestamp := time.Now().Format(time.RFC3339)
	
	fmt.Printf("=== SHUTDOWN INITIATED ===\n")
	fmt.Printf("Time: %s\n", timestamp)
	fmt.Printf("Reason: %s\n", reason)
	fmt.Printf("========================\n")

	if e.DryRun {
		fmt.Printf("[DRY RUN] Would execute: shutdown -h now\n")
		return nil
	}

	// Execute shutdown command
	cmd := exec.Command("shutdown", "-h", "now")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute shutdown command: %w", err)
	}

	return nil
}
