package robot

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNewRobotResponse(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		resp := NewRobotResponse(true)
		if !resp.Success {
			t.Error("expected Success to be true")
		}
		if resp.Timestamp == "" {
			t.Error("expected Timestamp to be set")
		}
		// Verify timestamp is valid RFC3339
		_, err := time.Parse(time.RFC3339, resp.Timestamp)
		if err != nil {
			t.Errorf("Timestamp is not valid RFC3339: %v", err)
		}
	})

	t.Run("failure response", func(t *testing.T) {
		resp := NewRobotResponse(false)
		if resp.Success {
			t.Error("expected Success to be false")
		}
	})
}

func TestNewErrorResponse(t *testing.T) {
	err := errors.New("session not found")
	resp := NewErrorResponse(err, ErrCodeSessionNotFound, "Use 'ntm list' to see sessions")

	if resp.Success {
		t.Error("expected Success to be false")
	}
	if resp.Error != "session not found" {
		t.Errorf("expected Error 'session not found', got %q", resp.Error)
	}
	if resp.ErrorCode != ErrCodeSessionNotFound {
		t.Errorf("expected ErrorCode %q, got %q", ErrCodeSessionNotFound, resp.ErrorCode)
	}
	if resp.Hint != "Use 'ntm list' to see sessions" {
		t.Errorf("unexpected Hint: %q", resp.Hint)
	}
}

func TestRobotResponseJSON(t *testing.T) {
	t.Run("success response serialization", func(t *testing.T) {
		resp := NewRobotResponse(true)
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if parsed["success"] != true {
			t.Error("expected success to be true in JSON")
		}
		if _, ok := parsed["timestamp"]; !ok {
			t.Error("expected timestamp in JSON")
		}
		// Error fields should be omitted
		if _, ok := parsed["error"]; ok {
			t.Error("error should be omitted when empty")
		}
		if _, ok := parsed["error_code"]; ok {
			t.Error("error_code should be omitted when empty")
		}
	})

	t.Run("error response serialization", func(t *testing.T) {
		resp := NewErrorResponse(
			errors.New("test error"),
			ErrCodeInternalError,
			"Try again",
		)
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if parsed["success"] != false {
			t.Error("expected success to be false in JSON")
		}
		if parsed["error"] != "test error" {
			t.Errorf("expected error 'test error', got %v", parsed["error"])
		}
		if parsed["error_code"] != ErrCodeInternalError {
			t.Errorf("expected error_code %q, got %v", ErrCodeInternalError, parsed["error_code"])
		}
		if parsed["hint"] != "Try again" {
			t.Errorf("expected hint 'Try again', got %v", parsed["hint"])
		}
	})
}

func TestAgentHints(t *testing.T) {
	hints := AgentHints{
		Summary: "2 sessions, 5 agents",
		SuggestedActions: []RobotAction{
			{Action: "send_prompt", Target: "idle agents", Reason: "2 available"},
			{Action: "wait", Reason: "3 agents busy"},
		},
		Warnings: []string{"Agent in pane 2 at 90% context"},
		Notes:    []string{"Consider spawning more agents"},
	}

	data, err := json.Marshal(hints)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed AgentHints
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.Summary != hints.Summary {
		t.Errorf("Summary mismatch: got %q", parsed.Summary)
	}
	if len(parsed.SuggestedActions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(parsed.SuggestedActions))
	}
	if parsed.SuggestedActions[0].Action != "send_prompt" {
		t.Errorf("first action should be send_prompt")
	}
	if len(parsed.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(parsed.Warnings))
	}
}

func TestWithAgentHints(t *testing.T) {
	// Create a response with agent hints
	type StatusResponse struct {
		RobotResponse
		SessionCount int `json:"session_count"`
	}

	resp := StatusResponse{
		RobotResponse: NewRobotResponse(true),
		SessionCount:  3,
	}

	hints := &AgentHints{
		Summary: "3 active sessions",
		SuggestedActions: []RobotAction{
			{Action: "monitor", Reason: "all agents working"},
		},
	}

	wrapped := AddAgentHints(resp, hints)
	data, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify original fields are present
	if parsed["success"] != true {
		t.Error("expected success in output")
	}
	if parsed["session_count"] != float64(3) {
		t.Errorf("expected session_count 3, got %v", parsed["session_count"])
	}

	// Verify _agent_hints is present
	hintsData, ok := parsed["_agent_hints"]
	if !ok {
		t.Fatal("expected _agent_hints in output")
	}

	hintsMap, ok := hintsData.(map[string]interface{})
	if !ok {
		t.Fatal("_agent_hints should be an object")
	}

	if hintsMap["summary"] != "3 active sessions" {
		t.Errorf("unexpected summary: %v", hintsMap["summary"])
	}
}

func TestWithAgentHintsNil(t *testing.T) {
	// When hints are nil, should just return the data
	resp := NewRobotResponse(true)
	wrapped := AddAgentHints(resp, nil)

	data, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, ok := parsed["_agent_hints"]; ok {
		t.Error("_agent_hints should not be present when nil")
	}
}

func TestErrorCodes(t *testing.T) {
	// Verify error codes are defined as expected strings
	codes := []string{
		ErrCodeSessionNotFound,
		ErrCodePaneNotFound,
		ErrCodeInvalidFlag,
		ErrCodeTimeout,
		ErrCodeNotImplemented,
		ErrCodeDependencyMissing,
		ErrCodeInternalError,
		ErrCodePermissionDenied,
		ErrCodeResourceBusy,
	}

	for _, code := range codes {
		if code == "" {
			t.Errorf("error code should not be empty")
		}
		// Codes should be SCREAMING_SNAKE_CASE
		for _, c := range code {
			if c >= 'a' && c <= 'z' {
				t.Errorf("error code %q should be uppercase", code)
				break
			}
		}
	}
}

func TestRobotError(t *testing.T) {
	// RobotError should output JSON and return the error
	testErr := errors.New("test error message")

	// Note: In a real test we'd capture stdout to verify JSON output
	// For now, just verify it returns the error correctly
	returnedErr := RobotError(testErr, ErrCodeSessionNotFound, "test hint")
	if returnedErr != testErr {
		t.Errorf("RobotError should return the original error, got %v", returnedErr)
	}
}

func TestNotImplementedResponse(t *testing.T) {
	t.Run("response structure", func(t *testing.T) {
		resp := NotImplementedResponse{
			RobotResponse: RobotResponse{
				Success:   false,
				Timestamp: "2025-12-15T10:30:00Z",
				Error:     "Feature not available",
				ErrorCode: ErrCodeNotImplemented,
				Hint:      "Try later",
			},
			Feature:        "robot-assign",
			PlannedVersion: "v1.3",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		// Verify required fields
		if parsed["success"] != false {
			t.Error("expected success to be false")
		}
		if parsed["error_code"] != ErrCodeNotImplemented {
			t.Errorf("expected error_code %q, got %v", ErrCodeNotImplemented, parsed["error_code"])
		}
		if parsed["feature"] != "robot-assign" {
			t.Errorf("expected feature 'robot-assign', got %v", parsed["feature"])
		}
		if parsed["planned_version"] != "v1.3" {
			t.Errorf("expected planned_version 'v1.3', got %v", parsed["planned_version"])
		}
	})

	t.Run("omits empty planned_version", func(t *testing.T) {
		resp := NotImplementedResponse{
			RobotResponse: NewErrorResponse(
				errors.New("not available"),
				ErrCodeNotImplemented,
				"",
			),
			Feature: "some-feature",
			// PlannedVersion intentionally empty
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if _, ok := parsed["planned_version"]; ok {
			t.Error("planned_version should be omitted when empty")
		}
	})
}

func TestTailAgentHints(t *testing.T) {
	t.Run("all idle agents", func(t *testing.T) {
		panes := map[string]PaneOutput{
			"0": {Type: "claude", State: "idle"},
			"1": {Type: "codex", State: "idle"},
		}
		hints := generateTailHints(panes)
		if hints == nil {
			t.Fatal("expected hints, got nil")
		}
		if len(hints.IdleAgents) != 2 {
			t.Errorf("expected 2 idle agents, got %d", len(hints.IdleAgents))
		}
		if len(hints.ActiveAgents) != 0 {
			t.Errorf("expected 0 active agents, got %d", len(hints.ActiveAgents))
		}
		// Should have suggestion about all idle
		found := false
		for _, s := range hints.Suggestions {
			if s == "All 2 agents idle - ready for new prompts" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected 'all idle' suggestion, got %v", hints.Suggestions)
		}
	})

	t.Run("mixed idle and active", func(t *testing.T) {
		panes := map[string]PaneOutput{
			"0": {Type: "claude", State: "idle"},
			"1": {Type: "codex", State: "active"},
			"2": {Type: "gemini", State: "active"},
		}
		hints := generateTailHints(panes)
		if hints == nil {
			t.Fatal("expected hints, got nil")
		}
		if len(hints.IdleAgents) != 1 {
			t.Errorf("expected 1 idle agent, got %d", len(hints.IdleAgents))
		}
		if len(hints.ActiveAgents) != 2 {
			t.Errorf("expected 2 active agents, got %d", len(hints.ActiveAgents))
		}
	})

	t.Run("error state includes suggestion", func(t *testing.T) {
		panes := map[string]PaneOutput{
			"0": {Type: "claude", State: "error"},
		}
		hints := generateTailHints(panes)
		if hints == nil {
			t.Fatal("expected hints, got nil")
		}
		// Should have error suggestion
		found := false
		for _, s := range hints.Suggestions {
			if s == "Pane 0 has an error - check output" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected error pane suggestion, got %v", hints.Suggestions)
		}
	})

	t.Run("unknown states return nil hints", func(t *testing.T) {
		panes := map[string]PaneOutput{
			"0": {Type: "shell", State: "unknown"},
		}
		hints := generateTailHints(panes)
		if hints != nil {
			t.Errorf("expected nil hints for unknown state, got %v", hints)
		}
	})
}
