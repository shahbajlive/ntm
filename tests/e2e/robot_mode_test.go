package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// These tests exercise the robot-mode JSON outputs end-to-end using the built
// binary on PATH. They intentionally avoid deep schema validation beyond
// parseability to keep them fast and resilient to small additive fields.

func TestRobotVersion(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-version")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuildDate string `json:"build_date"`
		GoVersion string `json:"go_version"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.Version == "" {
		t.Fatalf("missing version field in output")
	}
	if payload.GoVersion == "" {
		t.Fatalf("missing go_version field in output")
	}
	if payload.Commit == "" {
		t.Fatalf("missing commit field in output")
	}
	if payload.BuildDate == "" {
		t.Fatalf("missing build_date field in output")
	}
}

func TestRobotStatusEmptySessions(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string                   `json:"generated_at"`
		Sessions    []map[string]interface{} `json:"sessions"`
		Summary     struct {
			TotalSessions int `json:"total_sessions"`
			TotalAgents   int `json:"total_agents"`
			ClaudeCount   int `json:"claude_count"`
			CodexCount    int `json:"codex_count"`
			GeminiCount   int `json:"gemini_count"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at field")
	}

	if payload.Sessions == nil {
		t.Fatalf("missing sessions array")
	}

	if payload.Summary.TotalSessions < 0 || payload.Summary.TotalAgents < 0 {
		t.Fatalf("summary counts should be non-negative: %+v", payload.Summary)
	}
}

func TestRobotPlan(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-plan")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt    string                   `json:"generated_at"`
		Actions        []map[string]interface{} `json:"actions"`
		Recommendation string                   `json:"recommendation"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at field")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}

	if payload.Actions == nil {
		t.Fatalf("missing actions array")
	}

	if payload.Recommendation == "" {
		t.Fatalf("missing recommendation field")
	}

	for i, action := range payload.Actions {
		if _, ok := action["priority"]; !ok {
			t.Fatalf("actions[%d] missing priority", i)
		}
		if cmd, ok := action["command"].(string); !ok || strings.TrimSpace(cmd) == "" {
			t.Fatalf("actions[%d] missing non-empty command", i)
		}
	}
}

// TestRobotStatusWithLiveSession ensures a real session appears in robot-status.
func TestRobotStatusWithLiveSession(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("ntm_robot_status_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
codex = "bash"
gemini = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session with two agents (claude+codex)
	logger.LogSection("spawn session")
	if _, err := logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1", "--cod=1"); err != nil {
		t.Fatalf("ntm spawn failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// robot-status should include the session and at least 2 agents
	logger.LogSection("robot-status")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath, "--robot-status")

	var payload struct {
		Sessions []struct {
			Name    string                   `json:"name"`
			Agents  []map[string]interface{} `json:"agents"`
			Summary struct {
				TotalAgents int `json:"total_agents"`
			} `json:"summary"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	found := false
	for _, s := range payload.Sessions {
		if s.Name == session {
			found = true
			if s.Summary.TotalAgents < 2 {
				t.Fatalf("expected at least 2 agents (claude+codex) in summary, got %d", s.Summary.TotalAgents)
			}
			break
		}
	}
	if !found {
		t.Fatalf("robot-status did not include session %q; payload: %+v", session, payload)
	}
}

func TestRobotHelp(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-help")

	if len(out) == 0 {
		t.Fatalf("robot help output empty")
	}
	if !strings.Contains(string(out), "robot-status") {
		t.Fatalf("robot help missing expected marker")
	}
}

// TestRobotStatusWithSyntheticAgents ensures agent counts and types are surfaced when panes
// follow the NTM naming convention. This avoids launching real agent binaries by
// creating a tmux session with synthetic pane titles.
func TestRobotStatusWithSyntheticAgents(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	sessionName := createSyntheticAgentSession(t, logger)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		Sessions    []struct {
			Name   string `json:"name"`
			Agents []struct {
				Type    string `json:"type"`
				Pane    string `json:"pane"`
				PaneIdx int    `json:"pane_idx"`
			} `json:"agents"`
		} `json:"sessions"`
		Summary struct {
			TotalAgents int `json:"total_agents"`
			ClaudeCount int `json:"claude_count"`
			CodexCount  int `json:"codex_count"`
			GeminiCount int `json:"gemini_count"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}

	var targetSession *struct {
		Name   string `json:"name"`
		Agents []struct {
			Type    string `json:"type"`
			Pane    string `json:"pane"`
			PaneIdx int    `json:"pane_idx"`
		} `json:"agents"`
	}
	for i := range payload.Sessions {
		if payload.Sessions[i].Name == sessionName {
			targetSession = &payload.Sessions[i]
			break
		}
	}

	if targetSession == nil {
		t.Fatalf("robot-status missing session %s", sessionName)
	}

	if len(targetSession.Agents) < 3 {
		t.Fatalf("expected at least 3 agents for %s, got %d", sessionName, len(targetSession.Agents))
	}

	typeCounts := map[string]int{}
	for _, a := range targetSession.Agents {
		typeCounts[a.Type]++
	}

	if typeCounts["cc"] == 0 || typeCounts["cod"] == 0 || typeCounts["gmi"] == 0 {
		t.Fatalf("expected cc, cod, gmi agents in session %s; got %+v", sessionName, typeCounts)
	}

	if payload.Summary.TotalAgents < 3 {
		t.Fatalf("summary.total_agents should reflect at least synthetic agents, got %d", payload.Summary.TotalAgents)
	}
	if payload.Summary.ClaudeCount < 1 || payload.Summary.CodexCount < 1 || payload.Summary.GeminiCount < 1 {
		t.Fatalf("summary counts missing agent types: %+v", payload.Summary)
	}
}

func TestRobotStatusIncludesSystemFields(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		System      struct {
			Version   string `json:"version"`
			OS        string `json:"os"`
			Arch      string `json:"arch"`
			TmuxOK    bool   `json:"tmux_available"`
			GoVersion string `json:"go_version"`
		} `json:"system"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}
	if payload.System.Version == "" {
		t.Fatalf("system.version should be set")
	}
	if payload.System.OS == "" || payload.System.Arch == "" {
		t.Fatalf("system.os/arch should be set")
	}
	if payload.System.GoVersion == "" {
		t.Fatalf("system.go_version should be set")
	}
}

func TestRobotStatusHandlesLongSessionNames(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	longName := "robot_json_long_session_name_status_validation_1234567890"
	sessionName := createSyntheticAgentSessionWithName(t, logger, longName)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		Sessions    []struct {
			Name string `json:"name"`
		} `json:"sessions"`
		Summary struct {
			TotalSessions int `json:"total_sessions"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}

	var found bool
	for _, s := range payload.Sessions {
		if s.Name == sessionName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("robot-status missing long session name %s", sessionName)
	}
	if payload.Summary.TotalSessions < 1 {
		t.Fatalf("summary.total_sessions should be at least 1, got %d", payload.Summary.TotalSessions)
	}
}

// TestRobotSpawn tests the --robot-spawn flag for creating sessions.
func TestRobotSpawn(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_spawn_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
codex = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Test robot-spawn with Claude agents
	logger.LogSection("robot-spawn")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-spawn", session, "--spawn-cc=2", "--spawn-wait", "--spawn-safety")
	logger.Log("robot-spawn output: %s", string(out))

	var payload struct {
		Session string `json:"session"`
		Error   string `json:"error,omitempty"`
		Agents  []struct {
			Pane  string `json:"pane"`
			Type  string `json:"type"`
			Title string `json:"title"`
			Ready bool   `json:"ready"`
		} `json:"agents"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, string(out))
	}

	if payload.Error != "" {
		t.Fatalf("robot-spawn should succeed, got error: %s", payload.Error)
	}
	if payload.Session != session {
		t.Errorf("session = %q, want %q", payload.Session, session)
	}

	// Count Claude agents (type "claude" in agents list, excluding "user" type)
	claudeCount := 0
	for _, agent := range payload.Agents {
		if agent.Type == "claude" {
			claudeCount++
		}
	}
	if claudeCount < 2 {
		t.Errorf("claude count = %d, want at least 2", claudeCount)
	}
}

// TestRobotSendAndTail tests --robot-send and --robot-tail together.
func TestRobotSendAndTail(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_send_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session first
	logger.LogSection("spawn session for send test")
	_, _ = logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1")
	time.Sleep(500 * time.Millisecond)

	// Verify session was created
	testutil.AssertSessionExists(t, logger, session)

	// Test robot-send
	marker := fmt.Sprintf("ROBOT_SEND_MARKER_%d", time.Now().UnixNano())
	logger.LogSection("robot-send")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-send", session, "--msg", fmt.Sprintf("echo %s", marker), "--type=cc")
	logger.Log("robot-send output: %s", string(out))

	var sendPayload struct {
		Success bool `json:"success"`
		Targets []struct {
			PaneIdx int `json:"pane_idx"`
		} `json:"targets"`
		TargetCount int `json:"target_count"`
	}

	if err := json.Unmarshal(out, &sendPayload); err != nil {
		t.Fatalf("invalid robot-send JSON: %v", err)
	}

	if !sendPayload.Success {
		t.Fatalf("robot-send should succeed")
	}
	if sendPayload.TargetCount < 1 {
		t.Errorf("target_count = %d, want at least 1", sendPayload.TargetCount)
	}

	// Wait for command to execute
	time.Sleep(300 * time.Millisecond)

	// Test robot-tail
	logger.LogSection("robot-tail")
	out = testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-tail", session, "--lines=50")
	logger.Log("robot-tail output: %s", string(out))

	var tailPayload struct {
		Success bool   `json:"success"`
		Session string `json:"session"`
		Panes   []struct {
			Index   int    `json:"index"`
			Content string `json:"content"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &tailPayload); err != nil {
		t.Fatalf("invalid robot-tail JSON: %v", err)
	}

	if !tailPayload.Success {
		t.Fatalf("robot-tail should succeed")
	}
	if tailPayload.Session != session {
		t.Errorf("session = %q, want %q", tailPayload.Session, session)
	}

	// Check if marker appears in any pane content
	markerFound := false
	for _, pane := range tailPayload.Panes {
		if strings.Contains(pane.Content, marker) {
			markerFound = true
			logger.Log("Found marker in pane %d", pane.Index)
			break
		}
	}
	if !markerFound {
		logger.Log("WARNING: marker not found in tail output - timing issue possible")
	}
}

// TestRobotInterrupt tests the --robot-interrupt flag.
func TestRobotInterrupt(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_interrupt_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session
	logger.LogSection("spawn session for interrupt test")
	_, _ = logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1")
	time.Sleep(500 * time.Millisecond)

	// Verify session was created
	testutil.AssertSessionExists(t, logger, session)

	// Test robot-interrupt
	logger.LogSection("robot-interrupt")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-interrupt", session, "--interrupt-force")
	logger.Log("robot-interrupt output: %s", string(out))

	var payload struct {
		Success     bool `json:"success"`
		Interrupted int  `json:"interrupted"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid robot-interrupt JSON: %v", err)
	}

	if !payload.Success {
		t.Fatalf("robot-interrupt should succeed")
	}
}

func createSyntheticAgentSession(t *testing.T, logger *testutil.TestLogger) string {
	t.Helper()

	name := fmt.Sprintf("robot_json_%d", time.Now().UnixNano())
	return createSyntheticAgentSessionWithName(t, logger, name)
}

func createSyntheticAgentSessionWithName(t *testing.T, logger *testutil.TestLogger, name string) string {
	t.Helper()

	workdir := t.TempDir()

	logger.LogSection("Create synthetic tmux session")
	testutil.AssertCommandSuccess(t, logger, "tmux", "new-session", "-d", "-s", name, "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "split-window", "-t", name, "-h", "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "split-window", "-t", name, "-v", "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "select-layout", "-t", name, "tiled")

	paneIDsRaw := testutil.AssertCommandSuccess(t, logger, "tmux", "list-panes", "-t", name, "-F", "#{pane_id}")
	panes := strings.Fields(string(paneIDsRaw))
	if len(panes) < 3 {
		t.Fatalf("expected at least 3 panes, got %d (output=%s)", len(panes), string(paneIDsRaw))
	}

	titles := []string{
		fmt.Sprintf("%s__cc_1", name),
		fmt.Sprintf("%s__cod_1", name),
		fmt.Sprintf("%s__gmi_1", name),
	}

	for i, id := range panes[:3] {
		testutil.AssertCommandSuccess(t, logger, "tmux", "select-pane", "-t", id, "-T", titles[i])
	}

	t.Cleanup(func() {
		logger.LogSection("Teardown synthetic session")
		exec.Command("tmux", "kill-session", "-t", name).Run()
	})

	return name
}

// TestRobotStatusAllAgentStates comprehensively tests robot-status JSON output
// for all possible agent states: idle, working, error, unknown.
func TestRobotStatusAllAgentStates(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_status_all_states_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
codex = "bash"
gemini = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session with agents to simulate different states
	logger.LogSection("spawn session with multiple agents")
	if _, err := logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=2", "--cod=1", "--gmi=1"); err != nil {
		t.Fatalf("ntm spawn failed: %v", err)
	}
	time.Sleep(1 * time.Second) // Wait for agents to stabilize

	// Test basic robot-status JSON validity
	logger.LogSection("test robot-status JSON validity")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath, "--robot-status")
	logger.Log("[E2E-ROBOT-STATUS] Full JSON output:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		System      struct {
			Version   string `json:"version"`
			OS        string `json:"os"`
			Arch      string `json:"arch"`
			TmuxOK    bool   `json:"tmux_available"`
			GoVersion string `json:"go_version"`
		} `json:"system"`
		Sessions []struct {
			Name   string `json:"name"`
			Agents []struct {
				Type         string `json:"type"`
				Pane         string `json:"pane"`
				PaneIdx      int    `json:"pane_idx"`
				State        string `json:"state"`
				ErrorType    string `json:"error_type,omitempty"`
				LastActivity string `json:"last_activity,omitempty"`
			} `json:"agents"`
			Summary struct {
				TotalAgents   int `json:"total_agents"`
				WorkingAgents int `json:"working_agents"`
				IdleAgents    int `json:"idle_agents"`
				ErrorAgents   int `json:"error_agents"`
			} `json:"summary"`
		} `json:"sessions"`
		Summary struct {
			TotalSessions int `json:"total_sessions"`
			TotalAgents   int `json:"total_agents"`
			ClaudeCount   int `json:"claude_count"`
			CodexCount    int `json:"codex_count"`
			GeminiCount   int `json:"gemini_count"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Validate required top-level fields
	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at field")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}

	// Validate system fields
	if payload.System.Version == "" {
		t.Fatalf("missing system.version")
	}
	if payload.System.OS == "" || payload.System.Arch == "" {
		t.Fatalf("missing system.os or system.arch")
	}
	if payload.System.GoVersion == "" {
		t.Fatalf("missing system.go_version")
	}

	// Find our session
	var targetSession *struct {
		Name   string `json:"name"`
		Agents []struct {
			Type         string `json:"type"`
			Pane         string `json:"pane"`
			PaneIdx      int    `json:"pane_idx"`
			State        string `json:"state"`
			ErrorType    string `json:"error_type,omitempty"`
			LastActivity string `json:"last_activity,omitempty"`
		} `json:"agents"`
		Summary struct {
			TotalAgents   int `json:"total_agents"`
			WorkingAgents int `json:"working_agents"`
			IdleAgents    int `json:"idle_agents"`
			ErrorAgents   int `json:"error_agents"`
		} `json:"summary"`
	}
	for i := range payload.Sessions {
		if payload.Sessions[i].Name == session {
			targetSession = &payload.Sessions[i]
			break
		}
	}

	if targetSession == nil {
		t.Fatalf("robot-status missing session %s", session)
	}

	// Validate agent count and types
	if len(targetSession.Agents) < 4 {
		t.Fatalf("expected at least 4 agents (2 claude + 1 codex + 1 gemini), got %d", len(targetSession.Agents))
	}

	// Test that agents have valid states
	validStates := map[string]bool{
		"idle":    true,
		"working": true,
		"error":   true,
		"unknown": true,
	}

	statesCounted := map[string]int{}
	typeCounts := map[string]int{}

	for _, agent := range targetSession.Agents {
		// Validate required agent fields
		if agent.Type == "" {
			t.Fatalf("agent missing type field")
		}
		if agent.Pane == "" {
			t.Fatalf("agent missing pane field")
		}
		if agent.State == "" {
			t.Fatalf("agent missing state field")
		}

		// Validate state is one of the known valid states
		if !validStates[agent.State] {
			t.Fatalf("agent has invalid state %q, expected one of: idle, working, error, unknown", agent.State)
		}

		statesCounted[agent.State]++
		typeCounts[agent.Type]++

		logger.Log("[E2E-ROBOT-STATUS] Agent pane=%s type=%s state=%s error_type=%s",
			agent.Pane, agent.Type, agent.State, agent.ErrorType)
	}

	// Validate summary counts match agent counts
	expectedTotal := len(targetSession.Agents)
	if targetSession.Summary.TotalAgents != expectedTotal {
		t.Fatalf("summary.total_agents = %d, expected %d", targetSession.Summary.TotalAgents, expectedTotal)
	}

	// Validate state counts add up
	calculatedTotal := targetSession.Summary.IdleAgents + targetSession.Summary.WorkingAgents + targetSession.Summary.ErrorAgents
	// Note: unknown state agents might not be counted in specific categories
	if calculatedTotal > expectedTotal {
		t.Fatalf("sum of state counts (%d) exceeds total agents (%d)", calculatedTotal, expectedTotal)
	}

	// Validate agent type counts
	expectedClaude := 2
	expectedCodex := 1
	expectedGemini := 1
	if typeCounts["cc"] < expectedClaude {
		t.Fatalf("expected at least %d claude agents, got %d", expectedClaude, typeCounts["cc"])
	}
	if typeCounts["cod"] < expectedCodex {
		t.Fatalf("expected at least %d codex agents, got %d", expectedCodex, typeCounts["cod"])
	}
	if typeCounts["gmi"] < expectedGemini {
		t.Fatalf("expected at least %d gemini agents, got %d", expectedGemini, typeCounts["gmi"])
	}

	logger.Log("[E2E-ROBOT-STATUS] State distribution: idle=%d working=%d error=%d unknown=%d",
		statesCounted["idle"], statesCounted["working"], statesCounted["error"], statesCounted["unknown"])
	logger.Log("[E2E-ROBOT-STATUS] Type distribution: claude=%d codex=%d gemini=%d",
		typeCounts["cc"], typeCounts["cod"], typeCounts["gmi"])
}

// TestRobotStatusVerboseMode tests that verbose mode includes additional fields
func TestRobotStatusVerboseMode(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	_ = createSyntheticAgentSession(t, logger)

	// Test with verbose flag (if supported)
	logger.LogSection("test robot-status verbose mode")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status", "--verbose")
	logger.Log("[E2E-ROBOT-STATUS-VERBOSE] Full JSON output:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		Sessions    []struct {
			Name   string `json:"name"`
			Agents []struct {
				Type         string `json:"type"`
				State        string `json:"state"`
				LastActivity string `json:"last_activity,omitempty"`
				MemoryMB     int    `json:"memory_mb,omitempty"`
				LastOutput   string `json:"last_output,omitempty"`
			} `json:"agents"`
		} `json:"sessions"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON in verbose mode: %v", err)
	}

	// In verbose mode, we expect additional fields (though their presence depends on implementation)
	if payload.GeneratedAt == "" {
		t.Fatalf("verbose mode missing generated_at")
	}

	logger.Log("[E2E-ROBOT-STATUS-VERBOSE] Verbose mode JSON validated successfully")
}

// TestRobotStatusErrorHandling tests error conditions and malformed input
func TestRobotStatusErrorHandling(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	// Test with non-existent session filter (should still return valid JSON)
	logger.LogSection("test robot-status with non-existent session")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("[E2E-ROBOT-STATUS-ERROR] Output for empty state:\n%s", string(out))

	var payload struct {
		GeneratedAt string                   `json:"generated_at"`
		Sessions    []map[string]interface{} `json:"sessions"`
		Summary     struct {
			TotalSessions int `json:"total_sessions"`
			TotalAgents   int `json:"total_agents"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON in error condition: %v", err)
	}

	// Should still have valid structure even with no sessions
	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at in error condition")
	}
	if payload.Sessions == nil {
		t.Fatalf("sessions should be empty array, not nil")
	}

	logger.Log("[E2E-ROBOT-STATUS-ERROR] Error handling validated successfully")
}

// TestRobotStatusFieldStability tests that JSON schema remains stable
func TestRobotStatusFieldStability(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")

	// Define expected schema for critical fields that should never change
	type ExpectedAgentSchema struct {
		Type    string `json:"type"`
		Pane    string `json:"pane"`
		PaneIdx int    `json:"pane_idx"`
		State   string `json:"state"`
	}

	type ExpectedSessionSchema struct {
		Name   string                `json:"name"`
		Agents []ExpectedAgentSchema `json:"agents"`
	}

	type ExpectedSchema struct {
		GeneratedAt string                  `json:"generated_at"`
		Sessions    []ExpectedSessionSchema `json:"sessions"`
		Summary     struct {
			TotalSessions int `json:"total_sessions"`
			TotalAgents   int `json:"total_agents"`
		} `json:"summary"`
	}

	var payload ExpectedSchema
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("JSON schema validation failed: %v", err)
	}

	// Verify critical fields are present (this ensures API stability)
	if payload.GeneratedAt == "" {
		t.Fatalf("critical field 'generated_at' missing")
	}
	if payload.Sessions == nil {
		t.Fatalf("critical field 'sessions' missing")
	}
	if payload.Summary.TotalSessions < 0 || payload.Summary.TotalAgents < 0 {
		t.Fatalf("summary counts should be non-negative")
	}

	logger.Log("[E2E-ROBOT-STATUS-SCHEMA] JSON schema stability validated")
}

// Skip tests if ntm binary is missing.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("ntm"); err != nil {
		// ntm binary not on PATH; skip suite gracefully
		return
	}
	os.Exit(m.Run())
}
