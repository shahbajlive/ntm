package integration

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("tmux"); err != nil {
		// tmux is required for these integration tests
		return
	}
	os.Exit(m.Run())
}

func TestStatusDetectsIdlePrompt(t *testing.T) {
	testutil.RequireTmux(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	_, paneID := createSessionWithTitle(t, logger, "user_1")

	// First, start a clean bash shell to avoid interference from fancy prompts
	if err := tmux.SendKeys(paneID, "exec /bin/bash --norc --noprofile", true); err != nil {
		t.Fatalf("failed to start clean bash: %v", err)
	}
	// Wait for bash to fully initialize
	time.Sleep(800 * time.Millisecond)

	// Set a simple prompt and run a command
	if err := tmux.SendKeys(paneID, "PS1='$ '; echo IDLE_MARKER", true); err != nil {
		t.Fatalf("failed to set prompt and run command: %v", err)
	}

	// Poll for the echo output to appear on its own line
	var output string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		captured, err := tmux.CapturePaneOutput(paneID, 50)
		if err != nil {
			continue
		}
		lines := strings.Split(captured, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "IDLE_MARKER" {
				output = captured
				break
			}
		}
		if output != "" {
			break
		}
	}
	if output == "" {
		output, _ = tmux.CapturePaneOutput(paneID, 50)
		t.Fatalf("timeout waiting for marker; captured=%q", output)
	}

	// Give bash time to show its prompt after the echo
	time.Sleep(200 * time.Millisecond)

	requirePaneActivity(t, paneID)

	// After echo completes, bash should show "$ " prompt = idle state
	detector := status.NewDetector()
	st, err := detector.Detect(paneID)
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}
	if st.State != status.StateIdle {
		output, _ = tmux.CapturePaneOutput(paneID, 50)
		t.Fatalf("expected idle, got %s; agentType=%q, output=%q", st.State, st.AgentType, output)
	}
}

func TestStatusDetectsWorkingPane(t *testing.T) {
	testutil.RequireTmux(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	_, paneID := createSessionWithTitle(t, logger, "cod_1")

	// Start a longer-running command and wait for visible output to reduce flakiness.
	// The detection relies on seeing activity and not detecting an idle prompt.
	if err := tmux.SendKeys(paneID, "for i in 1 2 3 4 5; do echo working-$i; sleep 0.5; done", true); err != nil {
		t.Fatalf("failed to start work loop: %v", err)
	}
	// Wait for first output to appear
	time.Sleep(600 * time.Millisecond)

	requirePaneActivity(t, paneID)

	detector := status.NewDetector()
	st, err := detector.Detect(paneID)
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}
	if st.State != status.StateWorking {
		// Capture output for debugging
		output, _ := tmux.CapturePaneOutput(paneID, 50)
		t.Fatalf("expected working, got %s; output=%q", st.State, output)
	}
}

func TestStatusDetectsErrors(t *testing.T) {
	testutil.RequireTmux(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	_, paneID := createSessionWithTitle(t, logger, "cc_1")

	if err := tmux.SendKeys(paneID, "echo \"HTTP 429 rate limit\"; printf \"$ \"", true); err != nil {
		t.Fatalf("failed to write error output: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	requirePaneActivity(t, paneID)

	detector := status.NewDetector()
	st, err := detector.Detect(paneID)
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}
	if st.State != status.StateError {
		t.Fatalf("expected error state, got %s", st.State)
	}
	if st.ErrorType != status.ErrorRateLimit {
		t.Fatalf("expected ErrorRateLimit, got %s", st.ErrorType)
	}
}

func TestStatusDetectsAgentTypes(t *testing.T) {
	testutil.RequireTmux(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	session, pane1 := createSessionWithTitle(t, logger, "cc_1")

	pane2, err := tmux.SplitWindow(session, t.TempDir())
	if err != nil {
		t.Fatalf("failed to split window for cod pane: %v", err)
	}
	if err := tmux.SetPaneTitle(pane2, fmt.Sprintf("%s__cod_1", session)); err != nil {
		t.Fatalf("failed to set cod pane title: %v", err)
	}

	pane3, err := tmux.SplitWindow(session, t.TempDir())
	if err != nil {
		t.Fatalf("failed to split window for gmi pane: %v", err)
	}
	if err := tmux.SetPaneTitle(pane3, fmt.Sprintf("%s__gmi_1", session)); err != nil {
		t.Fatalf("failed to set gmi pane title: %v", err)
	}

	_ = tmux.ApplyTiledLayout(session)

	requirePaneActivity(t, pane1)

	detector := status.NewDetector()
	statuses, err := detector.DetectAll(session)
	if err != nil {
		t.Fatalf("detect all failed: %v", err)
	}

	found := map[string]bool{}
	for _, st := range statuses {
		found[st.AgentType] = true
	}

	for _, agent := range []string{"cc", "cod", "gmi"} {
		if !found[agent] {
			t.Fatalf("expected to detect agent type %s", agent)
		}
	}

	// Ensure the original pane retained its type
	for _, st := range statuses {
		if st.PaneID == pane1 && st.AgentType != "cc" {
			t.Fatalf("expected pane %s to be cc, got %s", pane1, st.AgentType)
		}
	}
}

func TestStatusIgnoresANSISequences(t *testing.T) {
	testutil.RequireTmux(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	_, paneID := createSessionWithTitle(t, logger, "cc_1")

	if err := tmux.SendKeys(paneID, "printf \"\\033[31mHTTP 429 rate limit\\033[0m\\n$ \"", true); err != nil {
		t.Fatalf("failed to write colored error output: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	requirePaneActivity(t, paneID)

	detector := status.NewDetector()
	st, err := detector.Detect(paneID)
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}
	if st.State != status.StateError || st.ErrorType != status.ErrorRateLimit {
		t.Fatalf("expected colored output to be detected as rate limit error, got state=%s type=%s", st.State, st.ErrorType)
	}
}

func createSessionWithTitle(t *testing.T, logger *testutil.TestLogger, titleSuffix string) (string, string) {
	t.Helper()

	session := fmt.Sprintf("ntm_status_%d", time.Now().UnixNano())
	logger.Log("Creating tmux session %s", session)

	if err := tmux.CreateSession(session, t.TempDir()); err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	t.Cleanup(func() {
		logger.Log("Killing tmux session %s", session)
		_ = tmux.KillSession(session)
	})

	panes, err := tmux.GetPanesWithActivity(session)
	if err != nil {
		t.Fatalf("failed to list panes: %v", err)
	}
	if len(panes) == 0 {
		t.Fatalf("session %s has no panes", session)
	}

	paneID := panes[0].Pane.ID
	title := fmt.Sprintf("%s__%s", session, titleSuffix)
	if err := tmux.SetPaneTitle(paneID, title); err != nil {
		t.Fatalf("failed to set pane title: %v", err)
	}

	return session, paneID
}

func requirePaneActivity(t *testing.T, paneID string) {
	t.Helper()

	if _, err := tmux.GetPaneActivity(paneID); err != nil {
		t.Skipf("tmux pane_last_activity unavailable: %v", err)
	}
}
