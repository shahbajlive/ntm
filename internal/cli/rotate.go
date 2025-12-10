package cli

import (
	"fmt"

	"github.com/Dicklesworthstone/ntm/internal/auth"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/spf13/cobra"
)

func newRotateCmd() *cobra.Command {
	var paneIndex int
	var preserveContext bool
	var targetAccount string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "rotate [session]",
		Short: "Rotate to a different account when rate limited",
		Long: `Helps switch AI agent accounts when hitting rate limits.

By default, uses the restart strategy (quit session, switch browser account, start fresh).
Use --preserve-context to re-authenticate the existing session instead.

Examples:
  ntm rotate myproject --pane=0
  ntm rotate myproject --pane=0 --preserve-context
  ntm rotate myproject --pane=0 --account=backup1@gmail.com`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var session string
			if len(args) > 0 {
				session = args[0]
			} else {
				if !tmux.InTmux() {
					return fmt.Errorf("session name required when not in tmux")
				}
				session = tmux.GetCurrentSession()
			}

			if paneIndex < 0 {
				// Default to current pane if in tmux and no pane specified
				// But we need to map current pane ID to index
				// For MVP, just require --pane
				return fmt.Errorf("pane index required (use --pane=N)")
			}

			if dryRun {
				fmt.Printf("Dry run: rotate session=%s pane=%d preserve=%v account=%s\n", session, paneIndex, preserveContext, targetAccount)
				return nil
			}

			if preserveContext {
				return executeReauthRotation(session, paneIndex)
			}
			return executeRestartRotation(session, paneIndex, targetAccount)
		},
	}

	cmd.Flags().IntVar(&paneIndex, "pane", -1, "Pane index to rotate")
	cmd.Flags().BoolVar(&preserveContext, "preserve-context", false, "Re-authenticate existing session instead of restarting")
	cmd.Flags().StringVar(&targetAccount, "account", "", "Target account email (optional)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print action without executing")

	return cmd
}

func executeRestartRotation(session string, paneIdx int, targetAccount string) error {
	// 1. Get pane ID
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return err
	}
	var paneID string
	var provider string
	for _, p := range panes {
		if p.Index == paneIdx {
			paneID = p.ID
			provider = string(p.Type)
			break
		}
	}
	if paneID == "" {
		return fmt.Errorf("pane %d not found in session %s", paneIdx, session)
	}

	// 2. Initialize Orchestrator
	orchestrator := auth.NewOrchestrator(cfg)

	// 3. Execute strategy
	if targetAccount == "" {
		targetAccount = "<your other account>"
	}

	fmt.Printf("Rotating pane %d (%s) using restart strategy...\n", paneIdx, provider)
	if err := orchestrator.ExecuteRestartStrategy(paneID, provider, targetAccount); err != nil {
		return output.PrintJSON(output.NewError(err.Error()))
	}

	return nil
}

func executeReauthRotation(session string, paneIdx int) error {
	// Re-auth strategy implementation will go here (Phase 1 MVP focuses on restart)
	return fmt.Errorf("re-auth strategy not yet implemented")
}
