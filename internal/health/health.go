// Package health provides agent health checking and status detection.
package health

import (
	"regexp"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// Status represents the overall health status of an agent
type Status string

const (
	StatusOK      Status = "ok"      // Agent is healthy and working
	StatusWarning Status = "warning" // Agent may have issues (stale, near rate limit)
	StatusError   Status = "error"   // Agent has errors (crashed, rate limited)
	StatusUnknown Status = "unknown" // Cannot determine status
)

// ActivityLevel represents how active the agent is
type ActivityLevel string

const (
	ActivityActive  ActivityLevel = "active"  // Content changed recently (< 30s)
	ActivityIdle    ActivityLevel = "idle"    // No change, but process running (prompt visible)
	ActivityStale   ActivityLevel = "stale"   // No change for extended period (> 5m)
	ActivityUnknown ActivityLevel = "unknown" // Cannot determine
)

// ProcessStatus represents the process state
type ProcessStatus string

const (
	ProcessRunning ProcessStatus = "running" // Process is alive
	ProcessExited  ProcessStatus = "exited"  // Process has exited
	ProcessUnknown ProcessStatus = "unknown" // Cannot determine
)

// Issue represents a detected problem
type Issue struct {
	Type    string `json:"type"`    // error_type identifier
	Message string `json:"message"` // Human-readable description
}

// AgentHealth contains health information for a single agent
type AgentHealth struct {
	Pane          int           `json:"pane"`            // Pane index
	PaneID        string        `json:"pane_id"`         // Full pane ID
	AgentType     string        `json:"agent_type"`      // claude, codex, gemini, user, unknown
	Status        Status        `json:"status"`          // Overall health status
	ProcessStatus ProcessStatus `json:"process_status"`  // Process running state
	Activity      ActivityLevel `json:"activity"`        // Activity level
	LastActivity  *time.Time    `json:"last_activity"`   // Last activity timestamp
	IdleSeconds   int           `json:"idle_seconds"`    // Seconds since last activity
	Issues        []Issue       `json:"issues"`          // Detected issues
}

// SessionHealth contains health information for an entire session
type SessionHealth struct {
	Session     string        `json:"session"`      // Session name
	CheckedAt   time.Time     `json:"checked_at"`   // When check was performed
	Agents      []AgentHealth `json:"agents"`       // Per-agent health
	Summary     HealthSummary `json:"summary"`      // Aggregate summary
	OverallStatus Status      `json:"overall_status"` // Worst status among all agents
}

// HealthSummary provides aggregate statistics
type HealthSummary struct {
	Total     int `json:"total"`      // Total agents
	Healthy   int `json:"healthy"`    // Agents with OK status
	Warning   int `json:"warning"`    // Agents with warning status
	Error     int `json:"error"`      // Agents with error status
	Unknown   int `json:"unknown"`    // Agents with unknown status
}

// Error patterns for detection
var errorPatterns = []struct {
	Pattern *regexp.Regexp
	Type    string
	Message string
}{
	{regexp.MustCompile(`(?i)rate.?limit`), "rate_limit", "Rate limit detected"},
	{regexp.MustCompile(`(?i)429`), "rate_limit", "HTTP 429 rate limit"},
	{regexp.MustCompile(`(?i)too.?many.?requests`), "rate_limit", "Too many requests"},
	{regexp.MustCompile(`(?i)quota.?exceeded`), "rate_limit", "Quota exceeded"},
	{regexp.MustCompile(`(?i)authentication.?(failed|error)`), "auth_error", "Authentication error"},
	{regexp.MustCompile(`(?i)(^|\s)401(\s|$)`), "auth_error", "HTTP 401 unauthorized"},
	{regexp.MustCompile(`(?i)unauthorized`), "auth_error", "Unauthorized access"},
	{regexp.MustCompile(`(?i)panic:`), "crash", "Panic detected"},
	{regexp.MustCompile(`(?i)fatal.?error`), "crash", "Fatal error detected"},
	{regexp.MustCompile(`(?i)segmentation.?fault`), "crash", "Segmentation fault"},
	{regexp.MustCompile(`(?i)stack.?trace`), "crash", "Stack trace detected"},
	{regexp.MustCompile(`(?i)connection.?(refused|reset|timeout)`), "network_error", "Connection error"},
	{regexp.MustCompile(`(?i)network.?(error|unreachable)`), "network_error", "Network error"},
}

// Prompt patterns for idle detection
var idlePromptPatterns = []string{
	"claude>", "claude >", "Claude>", "Claude >",
	"codex>", "codex >", "Codex>", "Codex >",
	"gemini>", "gemini >", "Gemini>", "Gemini >",
	"> ", "$ ", "% ", "# ", ">>> ",
}

// CheckSession performs health checks on all agents in a session
func CheckSession(session string) (*SessionHealth, error) {
	if !tmux.SessionExists(session) {
		return nil, &SessionNotFoundError{Session: session}
	}

	panesWithActivity, err := tmux.GetPanesWithActivity(session)
	if err != nil {
		return nil, err
	}

	health := &SessionHealth{
		Session:   session,
		CheckedAt: time.Now().UTC(),
		Agents:    make([]AgentHealth, 0, len(panesWithActivity)),
		Summary:   HealthSummary{},
		OverallStatus: StatusOK,
	}

	for _, pa := range panesWithActivity {
		agentHealth := checkAgent(pa)
		health.Agents = append(health.Agents, agentHealth)

		// Update summary
		health.Summary.Total++
		switch agentHealth.Status {
		case StatusOK:
			health.Summary.Healthy++
		case StatusWarning:
			health.Summary.Warning++
		case StatusError:
			health.Summary.Error++
		default:
			health.Summary.Unknown++
		}

		// Update overall status (worst wins)
		if statusSeverity(agentHealth.Status) > statusSeverity(health.OverallStatus) {
			health.OverallStatus = agentHealth.Status
		}
	}

	return health, nil
}

// checkAgent performs health checks on a single agent pane
func checkAgent(pa tmux.PaneActivity) AgentHealth {
	agent := AgentHealth{
		Pane:          pa.Pane.Index,
		PaneID:        pa.Pane.ID,
		AgentType:     detectAgentType(pa.Pane.Title),
		Status:        StatusUnknown,
		ProcessStatus: ProcessUnknown,
		Activity:      ActivityUnknown,
		Issues:        []Issue{},
	}

	// Set last activity
	if !pa.LastActivity.IsZero() {
		agent.LastActivity = &pa.LastActivity
		agent.IdleSeconds = int(time.Since(pa.LastActivity).Seconds())
	}

	// Capture pane output for analysis
	output, err := tmux.CapturePaneOutput(pa.Pane.ID, 50)
	if err != nil {
		agent.ProcessStatus = ProcessUnknown
		agent.Status = StatusUnknown
		return agent
	}

	// Check for error patterns
	agent.Issues = detectErrors(output)

	// Determine activity level
	agent.Activity = detectActivity(output, pa.LastActivity, pa.Pane.Title)

	// Determine process status
	agent.ProcessStatus = detectProcessStatus(output, pa.Pane.Command)

	// Calculate overall status
	agent.Status = calculateStatus(agent)

	return agent
}

// detectAgentType determines agent type from pane title
func detectAgentType(title string) string {
	titleLower := strings.ToLower(title)
	switch {
	case strings.Contains(titleLower, "__cc") || strings.Contains(titleLower, "claude"):
		return "claude"
	case strings.Contains(titleLower, "__cod") || strings.Contains(titleLower, "codex"):
		return "codex"
	case strings.Contains(titleLower, "__gmi") || strings.Contains(titleLower, "gemini"):
		return "gemini"
	case strings.Contains(titleLower, "user"):
		return "user"
	default:
		return "unknown"
	}
}

// detectErrors scans output for error patterns
func detectErrors(output string) []Issue {
	var issues []Issue
	seen := make(map[string]bool)

	for _, ep := range errorPatterns {
		if ep.Pattern.MatchString(output) {
			if !seen[ep.Type] {
				issues = append(issues, Issue{
					Type:    ep.Type,
					Message: ep.Message,
				})
				seen[ep.Type] = true
			}
		}
	}

	return issues
}

// detectActivity determines the activity level of an agent
func detectActivity(output string, lastActivity time.Time, title string) ActivityLevel {
	// Check last activity timestamp
	idleTime := time.Since(lastActivity)

	// Check for idle prompt
	lines := strings.Split(output, "\n")
	hasIdlePrompt := false
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		for _, prompt := range idlePromptPatterns {
			if strings.HasSuffix(line, prompt) || line == strings.TrimSpace(prompt) {
				hasIdlePrompt = true
				break
			}
		}
		if hasIdlePrompt {
			break
		}
	}

	// Determine activity level
	if idleTime < 30*time.Second {
		return ActivityActive
	} else if idleTime < 5*time.Minute {
		if hasIdlePrompt {
			return ActivityIdle
		}
		return ActivityActive
	} else {
		// Stale if > 5 minutes with no activity
		if hasIdlePrompt {
			return ActivityIdle // Still responsive, just waiting
		}
		return ActivityStale
	}
}

// detectProcessStatus determines if the agent process is running
func detectProcessStatus(output string, command string) ProcessStatus {
	// Check for exit indicators in output
	exitPatterns := []string{
		"exit status", "exited with", "process exited",
		"connection closed", "session ended",
	}

	outputLower := strings.ToLower(output)
	for _, pattern := range exitPatterns {
		if strings.Contains(outputLower, pattern) {
			return ProcessExited
		}
	}

	// If command is empty or shell-like, assume running
	if command == "" || strings.Contains(command, "bash") || strings.Contains(command, "zsh") || strings.Contains(command, "sh") {
		return ProcessRunning
	}

	// Default to running if no exit indicators
	return ProcessRunning
}

// calculateStatus determines overall status from all factors
func calculateStatus(agent AgentHealth) Status {
	// Error status if any critical issues
	for _, issue := range agent.Issues {
		if issue.Type == "crash" || issue.Type == "auth_error" {
			return StatusError
		}
	}

	// Error if process exited
	if agent.ProcessStatus == ProcessExited {
		return StatusError
	}

	// Warning if rate limited
	for _, issue := range agent.Issues {
		if issue.Type == "rate_limit" || issue.Type == "network_error" {
			return StatusWarning
		}
	}

	// Warning if stale
	if agent.Activity == ActivityStale {
		return StatusWarning
	}

	// OK if we got here
	if agent.ProcessStatus == ProcessRunning || agent.Activity == ActivityActive || agent.Activity == ActivityIdle {
		return StatusOK
	}

	return StatusUnknown
}

// statusSeverity returns numeric severity for status comparison
func statusSeverity(s Status) int {
	switch s {
	case StatusOK:
		return 0
	case StatusWarning:
		return 1
	case StatusError:
		return 2
	default:
		return 0
	}
}

// SessionNotFoundError is returned when session doesn't exist
type SessionNotFoundError struct {
	Session string
}

func (e *SessionNotFoundError) Error() string {
	return "session '" + e.Session + "' not found"
}
