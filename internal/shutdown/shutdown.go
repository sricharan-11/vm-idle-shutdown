// Package shutdown provides VM shutdown capabilities.
package shutdown

import (
	"fmt"
	"log"
	"os/exec"
	"time"
)

// Executor handles system shutdown operations.
type Executor struct {
	DryRun bool
}

// NewExecutor creates a new shutdown executor.
func NewExecutor(dryRun bool) *Executor {
	return &Executor{DryRun: dryRun}
}

// Shutdown initiates a system shutdown with a reason logged to the journal.
func (e *Executor) Shutdown(reason string) error {
	log.Println("=== SHUTDOWN INITIATED ===")
	log.Printf("Time:   %s", time.Now().Format(time.RFC3339))
	log.Printf("Reason: %s", reason)

	if e.DryRun {
		log.Println("[DRY RUN] Would execute: shutdown -h now")
		return nil
	}

	cmd := exec.Command("shutdown", "-h", "now")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("shutdown command failed: %w — %s", err, string(output))
	}

	return nil
}
