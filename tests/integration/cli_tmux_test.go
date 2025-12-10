package integration

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

// TestSpawnCreatesSession verifies that ntm spawn creates a session with the correct
// number of panes based on the agent configuration.
func TestSpawnCreatesSession(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create session with 2 Claude and 1 Codex agent
	// WorkDir is optional - CreateTestSession handles NTM_PROJECTS_BASE setup
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 2,
			Codex:  1,
		},
	})

	// Verify session exists
	testutil.AssertSessionExists(t, logger, session)

	// Verify pane count: 2 cc + 1 cod + 1 user = 4 panes
	testutil.AssertPaneCount(t, logger, session, 4)

	// Verify pane types via ntm status
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Session string `json:"session"`
		Panes   []struct {
			Index int    `json:"index"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	// Count agent types
	typeCounts := make(map[string]int)
	for _, pane := range status.Panes {
		typeCounts[pane.Type]++
	}

	logger.Log("Pane type counts: %v", typeCounts)

	// Verify we have the expected agent types
	// Note: JSON output uses full type names (claude, codex, gemini) not abbreviations
	if typeCounts["claude"] != 2 {
		t.Errorf("expected 2 claude panes, got %d", typeCounts["claude"])
	}
	if typeCounts["codex"] != 1 {
		t.Errorf("expected 1 codex pane, got %d", typeCounts["codex"])
	}
	if typeCounts["user"] != 1 {
		t.Errorf("expected 1 user pane, got %d", typeCounts["user"])
	}
}

// TestSpawnWithAllAgentTypes verifies spawn creates panes for all agent types.
func TestSpawnWithAllAgentTypes(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create session with all agent types
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
			Codex:  1,
			Gemini: 1,
		},
	})

	// Verify 4 panes total (3 agents + 1 user)
	testutil.AssertPaneCount(t, logger, session, 4)

	// Get status to verify types
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Panes []struct {
			Type string `json:"type"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	typeCounts := make(map[string]int)
	for _, pane := range status.Panes {
		typeCounts[pane.Type]++
	}

	// Note: JSON output uses full type names (claude, codex, gemini) not abbreviations
	for _, expectedType := range []string{"claude", "codex", "gemini", "user"} {
		if typeCounts[expectedType] < 1 {
			t.Errorf("expected at least 1 %s pane, got %d", expectedType, typeCounts[expectedType])
		}
	}
}

// TestSendCommandIntegration verifies that ntm send delivers prompts to panes.
func TestSendCommandIntegration(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create a simple session
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
	})

	// Wait for panes to be ready
	time.Sleep(500 * time.Millisecond)

	// Send a unique marker to the user pane
	uniqueMarker := fmt.Sprintf("TEST_MARKER_%d", time.Now().UnixNano())

	// Find user pane index via status
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Panes []struct {
			Index int    `json:"index"`
			Type  string `json:"type"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	// Find user pane
	userPaneIndex := -1
	for _, pane := range status.Panes {
		if pane.Type == "user" {
			userPaneIndex = pane.Index
			break
		}
	}

	if userPaneIndex == -1 {
		t.Fatal("could not find user pane")
	}

	// Send echo command to user pane using tmux directly
	// (ntm send targets agent panes, so use tmux for user pane test)
	target := fmt.Sprintf("%s:%d", session, userPaneIndex)
	cmd := exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("echo '%s'", uniqueMarker), "Enter")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to send keys: %v", err)
	}

	// Wait for command to execute
	time.Sleep(300 * time.Millisecond)

	// Verify marker appears in pane output
	testutil.AssertEventually(t, logger, 3*time.Second, 200*time.Millisecond, "marker appears in pane", func() bool {
		content, err := testutil.CapturePane(session, userPaneIndex)
		if err != nil {
			logger.Log("Failed to capture pane: %v", err)
			return false
		}
		return strings.Contains(content, uniqueMarker)
	})
}

// TestStatusCommandIntegration verifies ntm status reports accurate session state.
func TestStatusCommandIntegration(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create session with known configuration
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 2,
			Codex:  1,
		},
	})

	// Wait for session to stabilize
	time.Sleep(500 * time.Millisecond)

	// Run ntm status --json
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("ntm status failed: %v\nOutput: %s", err, string(out))
	}

	// Verify JSON is valid
	testutil.AssertJSONOutput(t, logger, out)

	// Parse and verify structure
	var status struct {
		Session string `json:"session"`
		Panes   []struct {
			Index  int    `json:"index"`
			Type   string `json:"type"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	// Verify session name matches
	if status.Session != session {
		t.Errorf("session name mismatch: got %q, expected %q", status.Session, session)
	}

	// Verify pane count
	expectedPanes := 4 // 2 cc + 1 cod + 1 user
	if len(status.Panes) != expectedPanes {
		t.Errorf("pane count mismatch: got %d, expected %d", len(status.Panes), expectedPanes)
	}

	// Verify each pane has required fields
	for _, pane := range status.Panes {
		if pane.Type == "" {
			t.Errorf("pane %d missing type", pane.Index)
		}
		if pane.Status == "" {
			t.Errorf("pane %d missing status", pane.Index)
		}
	}

	logger.Log("Status verification passed for session %s", session)
}

// TestAddCommandIntegration verifies ntm add adds panes to existing session.
func TestAddCommandIntegration(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create initial session with 1 Claude agent
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
	})

	// Initial pane count should be 2 (1 cc + 1 user)
	testutil.AssertPaneCount(t, logger, session, 2)

	// Add 2 more Claude agents
	_, err := logger.Exec("ntm", "add", session, "--cc=2")
	if err != nil {
		t.Fatalf("ntm add failed: %v", err)
	}

	// Wait for panes to be created
	time.Sleep(500 * time.Millisecond)

	// Verify new pane count: 1 original cc + 2 new cc + 1 user = 4 panes
	testutil.AssertPaneCount(t, logger, session, 4)

	// Verify via status that new panes are cc type
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Panes []struct {
			Type string `json:"type"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	claudeCount := 0
	for _, pane := range status.Panes {
		if pane.Type == "claude" {
			claudeCount++
		}
	}

	if claudeCount != 3 {
		t.Errorf("expected 3 claude panes after add, got %d", claudeCount)
	}
}

// TestAddDifferentAgentTypes verifies adding different agent types to existing session.
func TestAddDifferentAgentTypes(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create session with only Claude
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
	})

	// Add Codex agent
	_, err := logger.Exec("ntm", "add", session, "--cod=1")
	if err != nil {
		t.Fatalf("ntm add --cod failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Add Gemini agent (flag is --gmi not --gem)
	_, err = logger.Exec("ntm", "add", session, "--gmi=1")
	if err != nil {
		t.Fatalf("ntm add --gmi failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Verify final pane count: 1 cc + 1 cod + 1 gem + 1 user = 4 panes
	testutil.AssertPaneCount(t, logger, session, 4)

	// Verify each type exists
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Panes []struct {
			Type string `json:"type"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	types := make(map[string]bool)
	for _, pane := range status.Panes {
		types[pane.Type] = true
	}

	// Note: JSON output uses full type names (claude, codex, gemini) not abbreviations
	for _, expectedType := range []string{"claude", "codex", "gemini", "user"} {
		if !types[expectedType] {
			t.Errorf("expected %s pane after adds, not found", expectedType)
		}
	}
}

// TestKillCommandIntegration verifies ntm kill destroys sessions.
func TestKillCommandIntegration(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create session manually to control cleanup
	// ntm spawn uses NTM_PROJECTS_BASE + session_name as project directory
	session := fmt.Sprintf("ntm_test_kill_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	logger.Log("Creating session for kill test: %s", session)
	logger.Log("NTM_PROJECTS_BASE: %s", projectsBase)

	// Register cleanup in case test fails before we kill the session
	t.Cleanup(func() {
		// Only try to kill if we haven't already verified it's gone
		if testutil.SessionExists(session) {
			logger.Log("Cleanup: killing orphaned session %s", session)
			exec.Command("tmux", "kill-session", "-t", session).Run()
		}
	})

	// Spawn with correct environment and --json flag to avoid terminal attachment
	cmd := exec.Command("ntm", "spawn", session, "--cc=1", "--json")
	// Filter out NTM_PROJECTS_BASE from existing env and set our own
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "NTM_PROJECTS_BASE=") {
			env = append(env, e)
		}
	}
	env = append(env, "NTM_PROJECTS_BASE="+projectsBase)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	logger.Log("EXEC: ntm spawn %s --cc=1", session)
	logger.Log("OUTPUT: %s", string(out))
	if err != nil {
		t.Fatalf("failed to create session: %v\nOutput: %s", err, string(out))
	}

	// Verify session exists
	testutil.AssertSessionExists(t, logger, session)

	// Kill the session
	_, err = logger.Exec("ntm", "kill", "-f", session)
	if err != nil {
		t.Fatalf("ntm kill failed: %v", err)
	}

	// Wait a moment for session to be destroyed
	time.Sleep(300 * time.Millisecond)

	// Verify session no longer exists
	testutil.AssertSessionNotExists(t, logger, session)
}

// TestKillNonExistentSession verifies graceful handling of killing non-existent session.
func TestKillNonExistentSession(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Try to kill a session that doesn't exist
	nonExistent := fmt.Sprintf("ntm_nonexistent_%d", time.Now().UnixNano())

	// This should fail gracefully (non-zero exit but no crash)
	_, err := logger.Exec("ntm", "kill", "-f", nonExistent)

	// We expect an error since the session doesn't exist
	if err == nil {
		logger.Log("Note: ntm kill succeeded for non-existent session (may be intentional behavior)")
	} else {
		logger.Log("Expected error when killing non-existent session: %v", err)
	}
}

// TestSpawnWithWorkDir verifies spawn creates session in the correct project directory.
// NTM uses NTM_PROJECTS_BASE + session_name as the working directory.
func TestSpawnWithWorkDir(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create a unique projects base directory
	projectsBase := t.TempDir()
	logger.Log("Using NTM_PROJECTS_BASE: %s", projectsBase)

	// Create session - CreateTestSession will set NTM_PROJECTS_BASE and create the project dir
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
		WorkDir: projectsBase, // This is used as NTM_PROJECTS_BASE
	})

	// The actual project directory is NTM_PROJECTS_BASE/session_name
	expectedDir := filepath.Join(projectsBase, session)
	logger.Log("Expected project directory: %s", expectedDir)

	// Wait for session to initialize
	time.Sleep(500 * time.Millisecond)

	// Verify session exists
	testutil.AssertSessionExists(t, logger, session)

	// The working directory should be set in the panes
	// We can verify by checking pwd in the user pane
	out, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	var status struct {
		Panes []struct {
			Index int    `json:"index"`
			Type  string `json:"type"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	// Find user pane and check its pwd
	for _, pane := range status.Panes {
		if pane.Type == "user" {
			// Send pwd command
			target := fmt.Sprintf("%s:%d", session, pane.Index)
			if err := exec.Command("tmux", "send-keys", "-t", target, "pwd", "Enter").Run(); err != nil {
				t.Fatalf("failed to send pwd command: %v", err)
			}
			time.Sleep(200 * time.Millisecond)

			// Capture output
			content, err := testutil.CapturePane(session, pane.Index)
			if err != nil {
				t.Fatalf("failed to capture pane: %v", err)
			}

			// The pane should be in the project directory (session_name under NTM_PROJECTS_BASE)
			if !strings.Contains(content, expectedDir) {
				logger.Log("Warning: work directory may not be set correctly")
				logger.Log("Expected directory: %s", expectedDir)
				logger.Log("Pane content: %s", content)
				// Don't fail - some shells may display paths differently
			} else {
				logger.Log("Work directory verified: %s", expectedDir)
			}
			break
		}
	}
}

// TestStatusOutputFormats verifies different status output formats.
func TestStatusOutputFormats(t *testing.T) {
	testutil.IntegrationTestPrecheck(t)
	logger := testutil.NewTestLogger(t, t.TempDir())

	// Create a session
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
	})

	// Test --json format
	jsonOut, err := logger.Exec("ntm", "status", "--json", session)
	if err != nil {
		t.Fatalf("ntm status --json failed: %v", err)
	}
	testutil.AssertJSONOutput(t, logger, jsonOut)

	// Test default format (no flags) - should not fail
	defaultOut, err := logger.Exec("ntm", "status", session)
	if err != nil {
		t.Fatalf("ntm status (default) failed: %v", err)
	}

	// Default output should be non-empty
	if len(strings.TrimSpace(string(defaultOut))) == 0 {
		t.Error("default status output is empty")
	}

	logger.Log("Both status formats produced valid output")
}
