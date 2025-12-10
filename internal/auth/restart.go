package auth

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/rotation"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// Orchestrator manages the restart process
type Orchestrator struct {
	cfg           *config.Config
	authFlows     map[string]AuthFlow
	captureOutput func(string, int) (string, error)
}

// AuthFlow interface for provider-specific auth actions
type AuthFlow interface {
	InitiateAuth(paneID string) error
	// Add other methods as needed
}

// NewOrchestrator creates a new Orchestrator
func NewOrchestrator(cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		cfg:           cfg,
		authFlows:     make(map[string]AuthFlow),
		captureOutput: tmux.CapturePaneOutput,
	}
}

// RegisterAuthFlow registers a flow for a provider
func (o *Orchestrator) RegisterAuthFlow(provider string, flow AuthFlow) {
	o.authFlows[provider] = flow
}

// ExecuteRestartStrategy performs the terminate-switch-restart flow
func (o *Orchestrator) ExecuteRestartStrategy(paneID string, provider string, targetEmail string) error {
	// 1. Terminate existing session gracefully
	if err := o.TerminateSession(paneID, provider); err != nil {
		return fmt.Errorf("terminating session: %w", err)
	}

	// 2. Wait for shell prompt
	if err := o.WaitForShellPrompt(paneID, 10*time.Second); err != nil {
		return fmt.Errorf("session did not terminate: %w", err)
	}

	// 3. Prompt user for browser auth (simulated here, would interact with UI/TUI in real app)
	o.PromptBrowserAuth(targetEmail)

	// 4. Start new agent session
	return o.StartNewAgentSession(paneID, provider)
}

// TerminateSession tries to gracefully stop the agent, then force kills if needed
func (o *Orchestrator) TerminateSession(paneID string, provider string) error {
	prov := rotation.GetProvider(provider)

	// Try provider-specific exit command first if available
	if prov != nil && prov.ExitCommand() != "" {
		_ = tmux.SendKeys(paneID, prov.ExitCommand(), true)
		time.Sleep(1 * time.Second)
	}

	// Try graceful exit (Ctrl+C)
	if err := tmux.SendInterrupt(paneID); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)

	// Check if still active (heuristic: check process or output)
	// For now, assume we need a second Ctrl+C or explicit exit
	if err := tmux.SendInterrupt(paneID); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)

	return nil
}

// WaitForShellPrompt waits until the pane shows a shell prompt
func (o *Orchestrator) WaitForShellPrompt(paneID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	promptPatterns := []string{
		`\$\s*$`, // bash prompt
		`%\s*$`,  // zsh prompt
		`>\s*$`,  // generic prompt
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			output, _ := o.captureOutput(paneID, 5) // Capture last 5 lines
			for _, pattern := range promptPatterns {
				if regexp.MustCompile(pattern).MatchString(output) {
					return nil
				}
			}
		}
	}
}

// PromptBrowserAuth simulates prompting the user
func (o *Orchestrator) PromptBrowserAuth(email string) {
	// In a real CLI/TUI, this might print to the user pane or show a dialog.
	// For now, we assume the caller handles the UI part or we log it.
	fmt.Printf("Please log into %s in your browser, then press Enter (if interactive).\n", email)
}

// StartNewAgentSession launches the agent command in the pane
func (o *Orchestrator) StartNewAgentSession(paneID string, provider string) error {
	prov := rotation.GetProvider(provider)
	if prov == nil {
		return fmt.Errorf("unknown provider: %s", provider)
	}

	var agentCmd string
	switch prov.Name() {
	case "Claude":
		agentCmd = o.cfg.Agents.Claude
	case "Codex":
		agentCmd = o.cfg.Agents.Codex
	case "Gemini":
		agentCmd = o.cfg.Agents.Gemini
	default:
		return fmt.Errorf("unsupported provider: %s", prov.Name())
	}

	// For now, just run the agent command
	return tmux.SendKeys(paneID, agentCmd, true)
}
