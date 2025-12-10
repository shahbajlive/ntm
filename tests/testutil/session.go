package testutil

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// AgentConfig specifies the number of agents of each type to spawn.
type AgentConfig struct {
	Claude int // Number of Claude Code agents
	Codex  int // Number of Codex agents
	Gemini int // Number of Gemini agents
}

// SessionConfig holds configuration for creating a test session.
type SessionConfig struct {
	Agents   AgentConfig
	WorkDir  string // Working directory for the session
	ExtraArgs []string // Additional arguments to pass to ntm spawn
}

// CreateTestSession creates a new ntm session for testing.
// It automatically registers cleanup to kill the session when the test completes.
// Returns the session name.
func CreateTestSession(t *testing.T, logger *TestLogger, config SessionConfig) string {
	t.Helper()

	// Generate unique session name
	name := fmt.Sprintf("ntm_test_%d", time.Now().UnixNano())
	logger.LogSection("Creating Test Session")
	logger.Log("Session name: %s", name)

	// Build spawn command arguments
	args := []string{"spawn", name}
	if config.Agents.Claude > 0 {
		args = append(args, fmt.Sprintf("--cc=%d", config.Agents.Claude))
	}
	if config.Agents.Codex > 0 {
		args = append(args, fmt.Sprintf("--cod=%d", config.Agents.Codex))
	}
	if config.Agents.Gemini > 0 {
		args = append(args, fmt.Sprintf("--gem=%d", config.Agents.Gemini))
	}
	if config.WorkDir != "" {
		args = append(args, "--dir="+config.WorkDir)
	}
	args = append(args, config.ExtraArgs...)

	// Create the session
	out, err := logger.Exec("ntm", args...)
	if err != nil {
		t.Fatalf("failed to create session %s: %v\nOutput: %s", name, err, string(out))
	}

	// Register cleanup to kill the session
	t.Cleanup(func() {
		logger.LogSection("Teardown")
		logger.Log("Killing session %s", name)
		killSession(logger, name)
	})

	// Wait a moment for session to initialize
	time.Sleep(500 * time.Millisecond)

	logger.Log("Session %s created successfully", name)
	return name
}

// CreateTestSessionSimple creates a session with a simple agent configuration.
// This is a convenience wrapper around CreateTestSession.
func CreateTestSessionSimple(t *testing.T, logger *TestLogger, agents map[string]int) string {
	t.Helper()

	config := SessionConfig{}
	for agentType, count := range agents {
		switch strings.ToLower(agentType) {
		case "claude", "cc":
			config.Agents.Claude = count
		case "codex", "cod":
			config.Agents.Codex = count
		case "gemini", "gem":
			config.Agents.Gemini = count
		}
	}

	return CreateTestSession(t, logger, config)
}

// killSession forcefully kills an ntm session.
func killSession(logger *TestLogger, name string) {
	// Try ntm kill first
	out, err := exec.Command("ntm", "kill", "-f", name).CombinedOutput()
	if err != nil {
		logger.Log("ntm kill failed: %v, output: %s", err, string(out))
		// Fallback to tmux kill-session
		exec.Command("tmux", "kill-session", "-t", name).Run()
	} else {
		logger.Log("Session %s killed successfully", name)
	}
}

// KillAllTestSessions kills all ntm test sessions (those starting with "ntm_test_").
// Useful for cleanup in TestMain or after failed tests.
func KillAllTestSessions(logger *TestLogger) {
	logger.LogSection("Killing All Test Sessions")

	// List all tmux sessions
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		logger.Log("Failed to list tmux sessions: %v", err)
		return
	}

	sessions := strings.Split(strings.TrimSpace(string(out)), "\n")
	killed := 0
	for _, session := range sessions {
		if strings.HasPrefix(session, "ntm_test_") {
			logger.Log("Killing orphan test session: %s", session)
			exec.Command("tmux", "kill-session", "-t", session).Run()
			killed++
		}
	}

	logger.Log("Killed %d orphan test sessions", killed)
}

// SessionExists checks if a tmux session exists.
func SessionExists(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	return err == nil
}

// GetSessionPaneCount returns the number of panes in a session.
func GetSessionPaneCount(name string) (int, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", name, "-F", "#{pane_id}").Output()
	if err != nil {
		return 0, err
	}
	panes := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(panes) == 1 && panes[0] == "" {
		return 0, nil
	}
	return len(panes), nil
}

// WaitForSession waits for a session to exist, with timeout.
func WaitForSession(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if SessionExists(name) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("session %s did not appear within %s", name, timeout)
}

// CapturePane captures the visible content of a pane.
func CapturePane(name string, paneIndex int) (string, error) {
	target := fmt.Sprintf("%s:%d", name, paneIndex)
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture pane %s: %w", target, err)
	}
	return string(out), nil
}
