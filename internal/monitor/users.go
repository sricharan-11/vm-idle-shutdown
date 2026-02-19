package monitor

import (
	"bytes"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxUserSampleRetention is how far back user samples are kept.
const maxUserSampleRetention = 2 * time.Hour

// UserMonitor tracks the number of logged-in users over time.
type UserMonitor struct {
	mu       sync.RWMutex
	samples  []userSample
	interval time.Duration
}

// userSample represents a single user count measurement.
type userSample struct {
	timestamp time.Time
	userCount int
	users     []string
}

// NewUserMonitor creates a new user monitor with the specified sampling interval.
func NewUserMonitor(samplingInterval time.Duration) *UserMonitor {
	return &UserMonitor{
		samples:  make([]userSample, 0, 128),
		interval: samplingInterval,
	}
}

// Start begins user monitoring in a background goroutine.
func (m *UserMonitor) Start(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

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

// takeSample reads current user count and appends to the rolling buffer.
func (m *UserMonitor) takeSample() {
	users, err := getLoggedInUsers()
	if err != nil {
		log.Printf("Error reading logged-in users: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.samples = append(m.samples, userSample{
		timestamp: now,
		userCount: len(users),
		users:     users,
	})

	// Prune old samples
	cutoff := now.Add(-maxUserSampleRetention)
	start := 0
	for start < len(m.samples) && !m.samples[start].timestamp.After(cutoff) {
		start++
	}
	if start > 0 {
		m.samples = m.samples[start:]
	}
}

// getLoggedInUsers returns a deduplicated list of currently logged-in users.
func getLoggedInUsers() ([]string, error) {
	cmd := exec.Command("who")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	seen := make(map[string]struct{})

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			seen[fields[0]] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for user := range seen {
		result = append(result, user)
	}
	return result, nil
}

// NoUsersLoggedIn checks if there have been zero logged-in users
// for the specified duration.
func (m *UserMonitor) NoUsersLoggedIn(minutes int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	var samplesInWindow []userSample
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			samplesInWindow = append(samplesInWindow, s)
		}
	}

	minSamples := minutes / 2
	if minSamples < 1 {
		minSamples = 1
	}
	if len(samplesInWindow) < minSamples {
		log.Printf("User check: insufficient samples (%d/%d) for %d-min window",
			len(samplesInWindow), minSamples, minutes)
		return false
	}

	for _, s := range samplesInWindow {
		if s.userCount > 0 {
			log.Printf("User check: %d users at %s: %v — not idle",
				s.userCount, s.timestamp.Format(time.RFC3339), s.users)
			return false
		}
	}

	log.Printf("User check: 0 users for all %d samples over last %d min ✓",
		len(samplesInWindow), minutes)
	return true
}

// GetCurrentUserCount returns the most recent user count.
func (m *UserMonitor) GetCurrentUserCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.samples) == 0 {
		return 0
	}
	return m.samples[len(m.samples)-1].userCount
}
