package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/recipe"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/spf13/cobra"
)

func newSpawnCmd() *cobra.Command {
	var ccCount, codCount, gmiCount int
	var noUserPane bool
	var recipeName string

	cmd := &cobra.Command{
		Use:   "spawn <session-name>",
		Short: "Create session and spawn AI agents in panes",
		Long: `Create a new tmux session and launch AI coding agents in separate panes.

By default, the first pane is reserved for the user. Agent panes are created
and titled with their type (e.g., myproject__cc_1, myproject__cod_1).

You can use a recipe to quickly spawn a predefined set of agents:
  ntm spawn myproject -r full-stack    # Use the 'full-stack' recipe

Built-in recipes: quick-claude, full-stack, minimal, codex-heavy, balanced, review-team
Use 'ntm recipes list' to see all available recipes.

Examples:
  ntm spawn myproject --cc=2 --cod=2           # 2 Claude, 2 Codex + user pane
  ntm spawn myproject --cc=3 --cod=3 --gmi=1   # 3 Claude, 3 Codex, 1 Gemini
  ntm spawn myproject --cc=4 --no-user         # 4 Claude, no user pane
  ntm spawn myproject -r full-stack            # Use full-stack recipe
  ntm spawn myproject -r minimal               # Use minimal recipe`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If a recipe is specified, load it and use its agent counts
			if recipeName != "" {
				loader := recipe.NewLoader()
				r, err := loader.Get(recipeName)
				if err != nil {
					// List available recipes to help the user
					available := recipe.BuiltinNames()
					return fmt.Errorf("%w\n\nAvailable built-in recipes: %s",
						err, strings.Join(available, ", "))
				}

				// Validate the recipe
				if err := r.Validate(); err != nil {
					return fmt.Errorf("invalid recipe %q: %w", recipeName, err)
				}

				// Get counts from recipe (individual flags override if specified)
				counts := r.AgentCounts()
				if ccCount == 0 {
					ccCount = counts["cc"]
				}
				if codCount == 0 {
					codCount = counts["cod"]
				}
				if gmiCount == 0 {
					gmiCount = counts["gmi"]
				}

				fmt.Printf("Using recipe '%s': %s\n", r.Name, r.Description)
			}

			return runSpawn(args[0], ccCount, codCount, gmiCount, !noUserPane)
		},
	}

	cmd.Flags().IntVar(&ccCount, "cc", 0, "number of Claude agents")
	cmd.Flags().IntVar(&codCount, "cod", 0, "number of Codex agents")
	cmd.Flags().IntVar(&gmiCount, "gmi", 0, "number of Gemini agents")
	cmd.Flags().BoolVar(&noUserPane, "no-user", false, "don't reserve a pane for the user")
	cmd.Flags().StringVarP(&recipeName, "recipe", "r", "", "use a recipe for agent configuration")

	return cmd
}

func runSpawn(session string, ccCount, codCount, gmiCount int, userPane bool) error {
	// Helper for JSON error output
	outputError := func(err error) error {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return outputError(err)
	}

	if err := tmux.ValidateSessionName(session); err != nil {
		return outputError(err)
	}

	totalAgents := ccCount + codCount + gmiCount
	if totalAgents == 0 {
		return outputError(fmt.Errorf("no agents specified (use --cc, --cod, or --gmi)"))
	}

	dir := cfg.GetProjectDir(session)

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if IsJSONOutput() {
			// Auto-create directory without prompting in JSON mode
			if err := os.MkdirAll(dir, 0755); err != nil {
				return outputError(fmt.Errorf("creating directory: %w", err))
			}
		} else {
			fmt.Printf("Directory not found: %s\n", dir)
			if !confirm("Create it?") {
				fmt.Println("Aborted.")
				return nil
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}
			fmt.Printf("Created %s\n", dir)
		}
	}

	// Calculate total panes needed
	totalPanes := totalAgents
	if userPane {
		totalPanes++
	}

	// Create or use existing session
	if !tmux.SessionExists(session) {
		if !IsJSONOutput() {
			fmt.Printf("Creating session '%s' in %s...\n", session, dir)
		}
		if err := tmux.CreateSession(session, dir); err != nil {
			return outputError(fmt.Errorf("creating session: %w", err))
		}
	}

	// Get current pane count
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return outputError(err)
	}
	existingPanes := len(panes)

	// Add more panes if needed
	if existingPanes < totalPanes {
		toAdd := totalPanes - existingPanes
		if !IsJSONOutput() {
			fmt.Printf("Creating %d pane(s) (%d -> %d)...\n", toAdd, existingPanes, totalPanes)
		}
		for i := 0; i < toAdd; i++ {
			if _, err := tmux.SplitWindow(session, dir); err != nil {
				return outputError(fmt.Errorf("creating pane: %w", err))
			}
		}
	}

	// Get updated pane list
	panes, err = tmux.GetPanes(session)
	if err != nil {
		return outputError(err)
	}

	// Start assigning agents (skip first pane if user pane)
	startIdx := 0
	if userPane {
		startIdx = 1
	}

	agentNum := startIdx
	if !IsJSONOutput() {
		fmt.Printf("Launching agents: %dx cc, %dx cod, %dx gmi...\n", ccCount, codCount, gmiCount)
	}

	// Launch Claude agents
	for i := 0; i < ccCount && agentNum < len(panes); i++ {
		pane := panes[agentNum]
		title := fmt.Sprintf("%s__cc_%d", session, i+1)
		if err := tmux.SetPaneTitle(pane.ID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}
		cmd := fmt.Sprintf("cd %q && %s", dir, cfg.Agents.Claude)
		if err := tmux.SendKeys(pane.ID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching claude agent: %w", err))
		}
		agentNum++
	}

	// Launch Codex agents
	for i := 0; i < codCount && agentNum < len(panes); i++ {
		pane := panes[agentNum]
		title := fmt.Sprintf("%s__cod_%d", session, i+1)
		if err := tmux.SetPaneTitle(pane.ID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}
		cmd := fmt.Sprintf("cd %q && %s", dir, cfg.Agents.Codex)
		if err := tmux.SendKeys(pane.ID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching codex agent: %w", err))
		}
		agentNum++
	}

	// Launch Gemini agents
	for i := 0; i < gmiCount && agentNum < len(panes); i++ {
		pane := panes[agentNum]
		title := fmt.Sprintf("%s__gmi_%d", session, i+1)
		if err := tmux.SetPaneTitle(pane.ID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}
		cmd := fmt.Sprintf("cd %q && %s", dir, cfg.Agents.Gemini)
		if err := tmux.SendKeys(pane.ID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching gemini agent: %w", err))
		}
		agentNum++
	}

	// Get final pane list for output
	finalPanes, _ := tmux.GetPanes(session)

	// JSON output mode
	if IsJSONOutput() {
		paneResponses := make([]output.PaneResponse, len(finalPanes))
		agentCounts := output.AgentCountsResponse{}
		for i, p := range finalPanes {
			paneResponses[i] = output.PaneResponse{
				Index:   p.Index,
				Title:   p.Title,
				Type:    agentTypeToString(p.Type),
				Active:  p.Active,
				Width:   p.Width,
				Height:  p.Height,
				Command: p.Command,
			}
			switch p.Type {
			case tmux.AgentClaude:
				agentCounts.Claude++
			case tmux.AgentCodex:
				agentCounts.Codex++
			case tmux.AgentGemini:
				agentCounts.Gemini++
			default:
				agentCounts.User++
			}
		}
		agentCounts.Total = agentCounts.Claude + agentCounts.Codex + agentCounts.Gemini

		return output.PrintJSON(output.SpawnResponse{
			TimestampedResponse: output.NewTimestamped(),
			Session:             session,
			Created:             true, // spawn always creates or reuses
			WorkingDirectory:    dir,
			Panes:               paneResponses,
			AgentCounts:         agentCounts,
		})
	}

	fmt.Printf("✓ Launched %d agent(s)\n", totalAgents)

	// Register session as Agent Mail agent (non-blocking)
	registerSessionAgent(session, dir)

	return tmux.AttachOrSwitch(session)
}

// registerSessionAgent registers the session with Agent Mail.
// This is non-blocking and logs but does not fail if unavailable.
func registerSessionAgent(sessionName, workingDir string) {
	client := agentmail.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := client.RegisterSessionAgent(ctx, sessionName, workingDir)
	if err != nil {
		// Log but don't fail
		fmt.Printf("⚠ Agent Mail registration failed: %v\n", err)
		return
	}
	if info != nil {
		fmt.Printf("✓ Registered with Agent Mail as %s\n", info.AgentName)
	}
}
