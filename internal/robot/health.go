// Package robot provides machine-readable output for AI agents.
// health.go contains the --robot-health flag implementation.
package robot

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// HealthOutput provides a focused project health summary for AI agents
type HealthOutput struct {
	CheckedAt time.Time `json:"checked_at"`

	// System-level health
	System SystemHealthInfo `json:"system"`

	// Agent/session health matrix
	Sessions map[string]SessionHealthInfo `json:"sessions"`

	// Alerts for detected issues
	Alerts []string `json:"alerts"`

	// Project/beads health (existing functionality)
	BvAvailable       bool                   `json:"bv_available"`
	BdAvailable       bool                   `json:"bd_available"`
	Error             string                 `json:"error,omitempty"`
	DriftStatus       string                 `json:"drift_status,omitempty"`
	DriftMessage      string                 `json:"drift_message,omitempty"`
	TopBottlenecks    []bv.NodeScore         `json:"top_bottlenecks,omitempty"`
	TopKeystones      []bv.NodeScore         `json:"top_keystones,omitempty"`
	ReadyCount        int                    `json:"ready_count"`
	InProgressCount   int                    `json:"in_progress_count"`
	BlockedCount      int                    `json:"blocked_count"`
	NextRecommended   []RecommendedAction    `json:"next_recommended,omitempty"`
	DependencyContext *bv.DependencyContext  `json:"dependency_context,omitempty"`
}

// SystemHealthInfo contains system-level health metrics
type SystemHealthInfo struct {
	TmuxOK     bool    `json:"tmux_ok"`
	DiskFreeGB float64 `json:"disk_free_gb"`
	LoadAvg    float64 `json:"load_avg"`
}

// SessionHealthInfo contains health info for a single session
type SessionHealthInfo struct {
	Healthy bool                       `json:"healthy"`
	Agents  map[string]AgentHealthInfo `json:"agents"`
}

// AgentHealthInfo contains health metrics for a single agent
type AgentHealthInfo struct {
	Responsive      bool   `json:"responsive"`
	OutputRate      string `json:"output_rate"` // "high", "medium", "low", "none"
	LastActivitySec int    `json:"last_activity_sec"`
	Issue           string `json:"issue,omitempty"`
}

// RecommendedAction is a simplified priority recommendation
type RecommendedAction struct {
	IssueID  string `json:"issue_id"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	Priority int    `json:"priority"`
}

// noOutputThreshold is the time in seconds after which an agent is considered unresponsive
const noOutputThreshold = 300 // 5 minutes

// PrintHealth outputs a focused project health summary for AI consumption
func PrintHealth() error {
	output := HealthOutput{
		CheckedAt:   time.Now().UTC(),
		BvAvailable: bv.IsInstalled(),
		BdAvailable: bv.IsBdInstalled(),
		Sessions:    make(map[string]SessionHealthInfo),
		Alerts:      []string{},
	}

	// Get system health
	output.System = getSystemHealth()

	// Get agent/session health matrix
	populateAgentHealth(&output)

	// Get drift status
	drift := bv.CheckDrift()
	output.DriftStatus = drift.Status.String()
	output.DriftMessage = drift.Message

	// Get top bottlenecks (limit to 5)
	bottlenecks, err := bv.GetTopBottlenecks(5)
	if err == nil {
		output.TopBottlenecks = bottlenecks
	}

	// Get insights for keystones
	insights, err := bv.GetInsights()
	if err == nil && insights != nil {
		keystones := insights.Keystones
		if len(keystones) > 5 {
			keystones = keystones[:5]
		}
		output.TopKeystones = keystones
	}

	// Get priority recommendations
	recommendations, err := bv.GetNextActions(5)
	if err == nil {
		for _, rec := range recommendations {
			var reason string
			if len(rec.Reasoning) > 0 {
				reason = rec.Reasoning[0]
			}
			output.NextRecommended = append(output.NextRecommended, RecommendedAction{
				IssueID:  rec.IssueID,
				Title:    rec.Title,
				Reason:   reason,
				Priority: rec.SuggestedPriority,
			})
		}
	}

	// Get dependency context (includes ready/in-progress/blocked counts)
	depCtx, err := bv.GetDependencyContext(5)
	if err == nil {
		output.DependencyContext = depCtx
		output.ReadyCount = depCtx.ReadyCount
		output.BlockedCount = depCtx.BlockedCount
		output.InProgressCount = len(depCtx.InProgressTasks)
	}

	return encodeJSON(output)
}

// getSystemHealth returns system-level health metrics
func getSystemHealth() SystemHealthInfo {
	info := SystemHealthInfo{
		TmuxOK: tmux.IsInstalled(),
	}

	// Get disk free space (platform-specific)
	info.DiskFreeGB = getDiskFreeGB()

	// Get load average (platform-specific)
	info.LoadAvg = getLoadAverage()

	return info
}

// getDiskFreeGB returns the free disk space in GB for the current directory
func getDiskFreeGB() float64 {
	switch runtime.GOOS {
	case "darwin", "linux":
		// Use df command
		cmd := exec.Command("df", "-k", ".")
		out, err := cmd.Output()
		if err != nil {
			return -1
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) < 2 {
			return -1
		}
		// Parse the second line (data line)
		fields := strings.Fields(lines[1])
		if len(fields) < 4 {
			return -1
		}
		// Field 3 is available space in KB
		availKB, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			return -1
		}
		return availKB / (1024 * 1024) // Convert KB to GB
	default:
		return -1
	}
}

// getLoadAverage returns the 1-minute load average
func getLoadAverage() float64 {
	switch runtime.GOOS {
	case "darwin", "linux":
		// Use sysctl on macOS, /proc/loadavg on Linux
		if runtime.GOOS == "darwin" {
			cmd := exec.Command("sysctl", "-n", "vm.loadavg")
			out, err := cmd.Output()
			if err != nil {
				return -1
			}
			// Output format: "{ 1.23 2.34 3.45 }"
			s := strings.TrimSpace(string(out))
			s = strings.TrimPrefix(s, "{ ")
			s = strings.TrimSuffix(s, " }")
			fields := strings.Fields(s)
			if len(fields) < 1 {
				return -1
			}
			load, err := strconv.ParseFloat(fields[0], 64)
			if err != nil {
				return -1
			}
			return load
		}
		// Linux: read from /proc/loadavg
		cmd := exec.Command("cat", "/proc/loadavg")
		out, err := cmd.Output()
		if err != nil {
			return -1
		}
		fields := strings.Fields(string(out))
		if len(fields) < 1 {
			return -1
		}
		load, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return -1
		}
		return load
	default:
		return -1
	}
}

// populateAgentHealth fills in the agent health matrix for all sessions
func populateAgentHealth(output *HealthOutput) {
	if !output.System.TmuxOK {
		output.Alerts = append(output.Alerts, "tmux not available")
		return
	}

	sessions, err := tmux.ListSessions()
	if err != nil {
		output.Alerts = append(output.Alerts, fmt.Sprintf("failed to list sessions: %v", err))
		return
	}

	for _, sess := range sessions {
		sessHealth := SessionHealthInfo{
			Healthy: true,
			Agents:  make(map[string]AgentHealthInfo),
		}

		panes, err := tmux.GetPanes(sess.Name)
		if err != nil {
			output.Alerts = append(output.Alerts, fmt.Sprintf("%s: failed to get panes: %v", sess.Name, err))
			sessHealth.Healthy = false
			output.Sessions[sess.Name] = sessHealth
			continue
		}

		for _, pane := range panes {
			paneKey := fmt.Sprintf("%d.%d", 0, pane.Index)
			agentHealth := getAgentHealth(sess.Name, pane)

			sessHealth.Agents[paneKey] = agentHealth

			// Check for issues and add to alerts
			if !agentHealth.Responsive {
				sessHealth.Healthy = false
				output.Alerts = append(output.Alerts, fmt.Sprintf("%s %s: %s", sess.Name, paneKey, agentHealth.Issue))
			}
		}

		output.Sessions[sess.Name] = sessHealth
	}
}

// getAgentHealth calculates health metrics for a single agent pane
func getAgentHealth(session string, pane tmux.Pane) AgentHealthInfo {
	health := AgentHealthInfo{
		Responsive:      true,
		OutputRate:      "unknown",
		LastActivitySec: -1,
	}

	// Get pane activity time
	activityTime, err := tmux.GetPaneActivity(pane.ID)
	if err == nil {
		health.LastActivitySec = int(time.Since(activityTime).Seconds())

		// Check if unresponsive (no output for threshold time)
		if health.LastActivitySec > noOutputThreshold {
			health.Responsive = false
			health.Issue = fmt.Sprintf("no_output_%dm", noOutputThreshold/60)
		}
	}

	// Calculate output rate from recent activity
	health.OutputRate = calculateOutputRate(health.LastActivitySec)

	// Capture recent output to detect error states
	captured, err := tmux.CapturePaneOutput(pane.ID, 20)
	if err == nil {
		lines := splitLines(stripANSI(captured))
		state := detectState(lines, pane.Title)

		if state == "error" {
			health.Responsive = false
			health.Issue = "error_state_detected"
		}
	}

	return health
}

// calculateOutputRate determines output rate based on last activity time
func calculateOutputRate(lastActivitySec int) string {
	if lastActivitySec < 0 {
		return "unknown"
	}
	switch {
	case lastActivitySec <= 1:
		return "high" // >1 line/sec equivalent
	case lastActivitySec <= 10:
		return "medium"
	case lastActivitySec <= 60:
		return "low" // <1 line/min equivalent
	default:
		return "none"
	}
}
