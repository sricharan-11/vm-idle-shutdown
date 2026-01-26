package monitor

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// UserMonitor tracks the number of logged-in users over time
type UserMonitor struct {
	mu       sync.RWMutex
	samples  []userSample
	interval time.Duration
}

// userSample represents a single user count measurement
type userSample struct {
	timestamp time.Time
	userCount int
	users     []string
}

// NewUserMonitor creates a new user monitor with the specified sampling interval
func NewUserMonitor(samplingInterval time.Duration) *UserMonitor {
	return &UserMonitor{
		samples:  make([]userSample, 0),
		interval: samplingInterval,
	}
}

// Start begins user monitoring in a background goroutine
func (m *UserMonitor) Start(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		// Take initial reading
		m.takeSample()

		for {
			select {
			case <-ticker.C:
				m.takeSample()
			case <-stopCh:
				return
			}
		}
	}()
}

// takeSample reads current user count and adds it to the sample history
func (m *UserMonitor) takeSample() {
	users, err := m.getLoggedInUsers()
	if err != nil {
		fmt.Printf("Error reading logged-in users: %v\n", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.samples = append(m.samples, userSample{
		timestamp: time.Now(),
		userCount: len(users),
		users:     users,
	})

	// Keep only samples from the last 2 hours to avoid memory growth
	cutoff := time.Now().Add(-2 * time.Hour)
	filtered := make([]userSample, 0, len(m.samples))
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			filtered = append(filtered, s)
		}
	}
	m.samples = filtered
}

// getLoggedInUsers returns a list of currently logged-in users using the 'who' command
func (m *UserMonitor) getLoggedInUsers() ([]string, error) {
	cmd := exec.Command("who")
	var out bytes.Buffer
	cmd.Stdout = &out
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute 'who' command: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	users := make(map[string]bool)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			users[fields[0]] = true
		}
	}

	result := make([]string, 0, len(users))
	for user := range users {
		result = append(result, user)
	}
	return result, nil
}

// NoUsersLoggedIn checks if there have been zero logged-in users
// for the specified duration
func (m *UserMonitor) NoUsersLoggedIn(minutes int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	// Get samples within the time window
	var samplesInWindow []userSample
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			samplesInWindow = append(samplesInWindow, s)
		}
	}

	// Need at least some samples to make a decision
	minSamples := minutes / 2
	if minSamples < 1 {
		minSamples = 1
	}
	if len(samplesInWindow) < minSamples {
		fmt.Printf("User check: insufficient samples (%d/%d) for %d minute window\n",
			len(samplesInWindow), minSamples, minutes)
		return false
	}

	// Check if all samples show zero users
	for _, s := range samplesInWindow {
		if s.userCount > 0 {
			fmt.Printf("User check: %d users logged in at %s: %v\n",
				s.userCount, s.timestamp.Format(time.RFC3339), s.users)
			return false
		}
	}

	fmt.Printf("User check: no users logged in for all %d samples over last %d minutes\n",
		len(samplesInWindow), minutes)
	return true
}

// GetCurrentUserCount returns the most recent user count
func (m *UserMonitor) GetCurrentUserCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.samples) == 0 {
		return 0
	}
	return m.samples[len(m.samples)-1].userCount
}
