package swarm

import (
	"testing"
	"time"
)

func TestNewSessionOrchestrator(t *testing.T) {
	orch := NewSessionOrchestrator()

	if orch == nil {
		t.Fatal("expected non-nil orchestrator")
	}

	if orch.TmuxClient != nil {
		t.Error("expected nil TmuxClient (use default)")
	}

	if orch.StaggerDelay != 300*time.Millisecond {
		t.Errorf("expected StaggerDelay 300ms, got %v", orch.StaggerDelay)
	}
}

func TestSessionOrchestrator_CreateSessions_NilPlan(t *testing.T) {
	orch := NewSessionOrchestrator()

	result, err := orch.CreateSessions(nil)

	if err == nil {
		t.Error("expected error for nil plan")
	}

	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

func TestSessionOrchestrator_CreateSessions_EmptyPlan(t *testing.T) {
	orch := NewSessionOrchestrator()

	plan := &SwarmPlan{
		Sessions: []SessionSpec{},
	}

	result, err := orch.CreateSessions(plan)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(result.Sessions))
	}
}

func TestSessionOrchestrator_FormatPaneTitle(t *testing.T) {
	orch := NewSessionOrchestrator()

	tests := []struct {
		sessionName string
		pane        PaneSpec
		expected    string
	}{
		{
			sessionName: "cc_agents_1",
			pane:        PaneSpec{Index: 1, AgentType: "cc"},
			expected:    "cc_agents_1__cc_1",
		},
		{
			sessionName: "cod_agents_2",
			pane:        PaneSpec{Index: 5, AgentType: "cod"},
			expected:    "cod_agents_2__cod_5",
		},
		{
			sessionName: "gmi_agents_3",
			pane:        PaneSpec{Index: 3, AgentType: "gmi"},
			expected:    "gmi_agents_3__gmi_3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := orch.formatPaneTitle(tt.sessionName, tt.pane)
			if got != tt.expected {
				t.Errorf("formatPaneTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCreateSessionResult(t *testing.T) {
	result := CreateSessionResult{
		SessionSpec: SessionSpec{
			Name:      "test_session",
			AgentType: "cc",
			PaneCount: 4,
		},
		SessionName: "test_session",
		PaneIDs:     []string{"%1", "%2", "%3", "%4"},
		Error:       nil,
	}

	if result.SessionName != "test_session" {
		t.Errorf("expected session name 'test_session', got %q", result.SessionName)
	}

	if len(result.PaneIDs) != 4 {
		t.Errorf("expected 4 pane IDs, got %d", len(result.PaneIDs))
	}

	if result.Error != nil {
		t.Errorf("expected nil error, got %v", result.Error)
	}
}

func TestOrchestrationResult(t *testing.T) {
	result := OrchestrationResult{
		Sessions: []CreateSessionResult{
			{
				SessionName: "cc_agents_1",
				PaneIDs:     []string{"%1", "%2", "%3"},
			},
			{
				SessionName: "cod_agents_1",
				PaneIDs:     []string{"%4", "%5"},
			},
		},
		TotalPanes:      5,
		SuccessfulPanes: 5,
		FailedPanes:     0,
		Errors:          nil,
	}

	if len(result.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(result.Sessions))
	}

	if result.TotalPanes != 5 {
		t.Errorf("expected 5 total panes, got %d", result.TotalPanes)
	}

	if result.SuccessfulPanes != 5 {
		t.Errorf("expected 5 successful panes, got %d", result.SuccessfulPanes)
	}

	if result.FailedPanes != 0 {
		t.Errorf("expected 0 failed panes, got %d", result.FailedPanes)
	}

	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestOrchestrationResultWithErrors(t *testing.T) {
	result := OrchestrationResult{
		Sessions: []CreateSessionResult{
			{
				SessionName: "cc_agents_1",
				PaneIDs:     []string{"%1", "%2"},
				Error:       nil,
			},
		},
		TotalPanes:      4,
		SuccessfulPanes: 2,
		FailedPanes:     2,
		Errors:          []error{nil}, // Placeholder for test
	}

	if result.SuccessfulPanes != 2 {
		t.Errorf("expected 2 successful panes, got %d", result.SuccessfulPanes)
	}

	if result.FailedPanes != 2 {
		t.Errorf("expected 2 failed panes, got %d", result.FailedPanes)
	}
}
