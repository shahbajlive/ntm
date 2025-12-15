// Package robot provides machine-readable output for AI agents.
// types.go defines the standardized response structures for robot commands.
package robot

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Error codes for programmatic handling by AI agents.
// These codes allow agents to handle specific error types without parsing error messages.
const (
	// ErrCodeSessionNotFound indicates the requested session doesn't exist.
	ErrCodeSessionNotFound = "SESSION_NOT_FOUND"

	// ErrCodePaneNotFound indicates the requested pane doesn't exist.
	ErrCodePaneNotFound = "PANE_NOT_FOUND"

	// ErrCodeInvalidFlag indicates a flag value is invalid or malformed.
	ErrCodeInvalidFlag = "INVALID_FLAG"

	// ErrCodeTimeout indicates the operation timed out.
	ErrCodeTimeout = "TIMEOUT"

	// ErrCodeNotImplemented indicates a feature is planned but not yet available.
	ErrCodeNotImplemented = "NOT_IMPLEMENTED"

	// ErrCodeDependencyMissing indicates a required external tool is not installed.
	ErrCodeDependencyMissing = "DEPENDENCY_MISSING"

	// ErrCodeInternalError indicates an unexpected internal error.
	ErrCodeInternalError = "INTERNAL_ERROR"

	// ErrCodePermissionDenied indicates insufficient permissions.
	ErrCodePermissionDenied = "PERMISSION_DENIED"

	// ErrCodeResourceBusy indicates a resource is locked or in use.
	ErrCodeResourceBusy = "RESOURCE_BUSY"
)

// RobotResponse is the base structure for all robot command outputs.
// All robot commands should embed this or include these fields.
//
// Design Philosophy:
// AI coding agents consume this output. They don't read external documentation
// before using commands - they parse JSON and make decisions based on what
// they see. Every response must be understandable WITHOUT external docs.
type RobotResponse struct {
	// Success indicates whether the operation completed successfully.
	// This is the first field agents should check.
	Success bool `json:"success"`

	// Timestamp is when this response was generated (RFC3339 format, UTC).
	Timestamp string `json:"timestamp"`

	// Error contains the human-readable error message when success=false.
	Error string `json:"error,omitempty"`

	// ErrorCode is a machine-readable error code for programmatic handling.
	// See ErrCode* constants for defined codes.
	ErrorCode string `json:"error_code,omitempty"`

	// Hint provides actionable guidance for resolving errors.
	// Example: "Use 'ntm list' to see available sessions"
	Hint string `json:"hint,omitempty"`
}

// AgentHints provides optional guidance for AI agents consuming robot output.
// This is included in complex responses (status, snapshot, dashboard) to help
// agents make decisions without needing to implement complex logic themselves.
//
// The underscore prefix in JSON (_agent_hints) indicates this is meta-information
// that agents can safely ignore if they just want the raw data.
type AgentHints struct {
	// Summary is a human-readable one-liner describing the response.
	// Example: "2 sessions, 6 agents total (4 working, 2 idle)"
	Summary string `json:"summary,omitempty"`

	// SuggestedActions are actions the agent might want to take.
	SuggestedActions []RobotAction `json:"suggested_actions,omitempty"`

	// Warnings are non-fatal issues the agent should be aware of.
	// Example: "Agent in pane 3 approaching context limit (85%)"
	Warnings []string `json:"warnings,omitempty"`

	// Notes are informational messages that may be useful.
	Notes []string `json:"notes,omitempty"`
}

// RobotAction represents a recommended action for an AI agent in JSON output.
// This is different from SuggestedAction in markdown.go which is for markdown rendering.
type RobotAction struct {
	// Action is the type of action (e.g., "send_prompt", "wait", "spawn").
	Action string `json:"action"`

	// Target describes what the action should be applied to.
	// Example: "idle agents", "pane 2", "session myproject"
	Target string `json:"target,omitempty"`

	// Reason explains why this action is suggested.
	// Example: "2 agents available", "context at 95%"
	Reason string `json:"reason,omitempty"`

	// Priority indicates relative importance (higher = more important).
	Priority int `json:"priority,omitempty"`
}

// NewRobotResponse creates a new RobotResponse with current timestamp.
func NewRobotResponse(success bool) RobotResponse {
	return RobotResponse{
		Success:   success,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// NewErrorResponse creates an error RobotResponse with the given details.
func NewErrorResponse(err error, code, hint string) RobotResponse {
	resp := NewRobotResponse(false)
	if err != nil {
		resp.Error = err.Error()
	}
	resp.ErrorCode = code
	resp.Hint = hint
	return resp
}

// RobotError outputs a standardized error response as JSON and returns the original error.
// Use this when you want structured JSON output but need to return an error to the caller.
// This is useful for testing and for callers that want to handle errors themselves.
//
// Example usage:
//
//	if !tmux.SessionExists(session) {
//	    return RobotError(
//	        fmt.Errorf("session '%s' not found", session),
//	        ErrCodeSessionNotFound,
//	        "Use 'ntm list' to see available sessions",
//	    )
//	}
func RobotError(err error, code, hint string) error {
	resp := NewErrorResponse(err, code, hint)
	outputJSON(resp)
	return err
}

// PrintRobotError outputs a standardized error response and exits with code 1.
// Use this for actual errors that indicate something went wrong when you want
// to exit immediately. For testable code, prefer RobotError instead.
//
// Example usage:
//
//	if !tmux.SessionExists(session) {
//	    PrintRobotError(
//	        fmt.Errorf("session '%s' not found", session),
//	        ErrCodeSessionNotFound,
//	        "Use 'ntm list' to see available sessions",
//	    )
//	    return
//	}
func PrintRobotError(err error, code, hint string) {
	resp := NewErrorResponse(err, code, hint)
	outputJSON(resp)
	os.Exit(1)
}

// NotImplementedResponse is the structured output for unavailable features.
type NotImplementedResponse struct {
	RobotResponse
	Feature        string `json:"feature"`                   // The unavailable feature name
	PlannedVersion string `json:"planned_version,omitempty"` // Version where feature is planned
}

// PrintRobotUnavailable outputs a response for unavailable/unimplemented features
// and exits with code 2. Use this when a feature doesn't exist yet or a
// dependency is missing - it's not an error, just unavailable.
//
// Exit code 2 signals "unavailable" to agents, distinct from error (1) or success (0).
//
// Example usage:
//
//	robot.PrintRobotUnavailable(
//	    "robot-assign",
//	    "Work assignment is planned for a future release",
//	    "v1.3",
//	    "Use manual work distribution in the meantime",
//	)
func PrintRobotUnavailable(feature, message, plannedVersion, hint string) {
	resp := NotImplementedResponse{
		RobotResponse: RobotResponse{
			Success:   false,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     message,
			ErrorCode: ErrCodeNotImplemented,
			Hint:      hint,
		},
		Feature:        feature,
		PlannedVersion: plannedVersion,
	}
	outputJSON(resp)
	os.Exit(2)
}

// ErrorResponse is a complete error output structure that can be embedded
// in more specific response types or used standalone.
type ErrorResponse struct {
	RobotResponse
}

// SuccessResponse is a minimal success response.
type SuccessResponse struct {
	RobotResponse
}

// outputJSON encodes the value as pretty-printed JSON to stdout.
// This is the internal helper used by Print* functions.
func outputJSON(v interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// WithAgentHints is a wrapper that adds _agent_hints to any response.
// Use this to add agent guidance to existing response types.
type WithAgentHints struct {
	// Embed the original response data
	Data interface{} `json:"-"`

	// AgentHints provides guidance for AI agents
	AgentHints *AgentHints `json:"_agent_hints,omitempty"`
}

// MarshalJSON implements custom JSON marshaling to flatten the Data field.
func (w WithAgentHints) MarshalJSON() ([]byte, error) {
	// First marshal the data
	dataBytes, err := json.Marshal(w.Data)
	if err != nil {
		return nil, err
	}

	// If no hints, just return the data
	if w.AgentHints == nil {
		return dataBytes, nil
	}

	// Parse data as a map
	var dataMap map[string]interface{}
	if err := json.Unmarshal(dataBytes, &dataMap); err != nil {
		return nil, fmt.Errorf("data must be a JSON object: %w", err)
	}

	// Add agent hints
	dataMap["_agent_hints"] = w.AgentHints

	return json.Marshal(dataMap)
}

// AddAgentHints wraps a response with agent hints.
func AddAgentHints(data interface{}, hints *AgentHints) WithAgentHints {
	return WithAgentHints{
		Data:       data,
		AgentHints: hints,
	}
}
