package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
}

func TestRobotStatusEmptySessions(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string                   `json:"generated_at"`
		Sessions    []map[string]interface{} `json:"sessions"`
		Summary     map[string]interface{}   `json:"summary"`
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

	if payload.Summary == nil {
		t.Fatalf("missing summary object")
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

	if payload.Actions == nil {
		t.Fatalf("missing actions array")
	}

	if payload.Recommendation == "" {
		t.Fatalf("missing recommendation field")
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

func createSyntheticAgentSession(t *testing.T, logger *testutil.TestLogger) string {
	t.Helper()

	name := fmt.Sprintf("robot_json_%d", time.Now().UnixNano())
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

// Skip tests if ntm binary is missing.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("ntm"); err != nil {
		// ntm binary not on PATH; skip suite gracefully
		return
	}
	os.Exit(m.Run())
}
