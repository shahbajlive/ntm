package cli

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

const (
	estimateNearBudgetThreshold = 0.85
	defaultModeTokenEstimate    = 2000
)

type ensembleEstimateMode struct {
	ModeID        string  `json:"mode_id" yaml:"mode_id"`
	ModeCode      string  `json:"mode_code,omitempty" yaml:"mode_code,omitempty"`
	ModeName      string  `json:"mode_name,omitempty" yaml:"mode_name,omitempty"`
	Category      string  `json:"category,omitempty" yaml:"category,omitempty"`
	Tier          string  `json:"tier,omitempty" yaml:"tier,omitempty"`
	TokenEstimate int     `json:"token_estimate" yaml:"token_estimate"`
	ValueScore    float64 `json:"value_score,omitempty" yaml:"value_score,omitempty"`
	ValuePerToken float64 `json:"value_per_token,omitempty" yaml:"value_per_token,omitempty"`
}

type ensembleEstimateBudget struct {
	MaxTokensPerMode       int `json:"max_tokens_per_mode" yaml:"max_tokens_per_mode"`
	MaxTotalTokens         int `json:"max_total_tokens" yaml:"max_total_tokens"`
	SynthesisReserveTokens int `json:"synthesis_reserve_tokens,omitempty" yaml:"synthesis_reserve_tokens,omitempty"`
	ContextReserveTokens   int `json:"context_reserve_tokens,omitempty" yaml:"context_reserve_tokens,omitempty"`
	EstimatedModeTokens    int `json:"estimated_mode_tokens" yaml:"estimated_mode_tokens"`
	EstimatedTotalTokens   int `json:"estimated_total_tokens" yaml:"estimated_total_tokens"`
	ModeCount              int `json:"mode_count" yaml:"mode_count"`
	BudgetOverride         int `json:"budget_override,omitempty" yaml:"budget_override,omitempty"`
}

type ensembleEstimateSuggestion struct {
	ReplaceModeID     string `json:"replace_mode_id" yaml:"replace_mode_id"`
	ReplaceModeName   string `json:"replace_mode_name,omitempty" yaml:"replace_mode_name,omitempty"`
	ReplaceTokens     int    `json:"replace_tokens" yaml:"replace_tokens"`
	SuggestedModeID   string `json:"suggested_mode_id" yaml:"suggested_mode_id"`
	SuggestedModeName string `json:"suggested_mode_name,omitempty" yaml:"suggested_mode_name,omitempty"`
	SuggestedTokens   int    `json:"suggested_tokens" yaml:"suggested_tokens"`
	SavingsTokens     int    `json:"savings_tokens" yaml:"savings_tokens"`
	Reason            string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type ensembleEstimateOutput struct {
	GeneratedAt time.Time                    `json:"generated_at" yaml:"generated_at"`
	PresetName  string                       `json:"preset_name,omitempty" yaml:"preset_name,omitempty"`
	PresetLabel string                       `json:"preset_label,omitempty" yaml:"preset_label,omitempty"`
	Modes       []ensembleEstimateMode       `json:"modes" yaml:"modes"`
	Budget      ensembleEstimateBudget       `json:"budget" yaml:"budget"`
	Warnings    []string                     `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Suggestions []ensembleEstimateSuggestion `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
}

func newEnsembleEstimateCmd() *cobra.Command {
	var (
		format         string
		presetName     string
		modesRaw       string
		budgetOverride int
	)

	cmd := &cobra.Command{
		Use:   "estimate [preset]",
		Short: "Estimate token usage for an ensemble before running it",
		Long: `Estimate token usage for an ensemble preset or explicit mode list.

Examples:
  ntm ensemble estimate project-diagnosis
  ntm ensemble estimate --preset idea-forge --format=json
  ntm ensemble estimate --modes=deductive,edge-case,root-cause --budget=12000`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if presetName == "" && len(args) > 0 {
				presetName = strings.TrimSpace(args[0])
			}
			if presetName == "" && strings.TrimSpace(modesRaw) == "" {
				return fmt.Errorf("preset name or --modes is required")
			}
			if presetName != "" && strings.TrimSpace(modesRaw) != "" {
				return fmt.Errorf("use either a preset name or --modes, not both")
			}

			modes := splitCommaSeparated(modesRaw)
			return runEnsembleEstimate(cmd.OutOrStdout(), presetName, modes, budgetOverride, format)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&presetName, "preset", "", "Ensemble preset name (alternative to positional arg)")
	cmd.Flags().StringVar(&modesRaw, "modes", "", "Explicit mode IDs or codes (comma-separated)")
	cmd.Flags().IntVar(&budgetOverride, "budget", 0, "Total token budget override for warnings")
	cmd.Flags().IntVar(&budgetOverride, "budget-total", 0, "Total token budget override for warnings (alias)")

	return cmd
}

func runEnsembleEstimate(w io.Writer, presetName string, modes []string, budgetOverride int, format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "table"
	}
	if jsonOutput {
		format = "json"
	}

	catalog, err := ensemble.GlobalCatalog()
	if err != nil {
		return fmt.Errorf("load mode catalog: %w", err)
	}

	var (
		modeIDs     []string
		presetLabel string
		presetUsed  string
		budget      = ensemble.DefaultBudgetConfig()
	)

	if presetName != "" {
		registry, err := ensemble.GlobalEnsembleRegistry()
		if err != nil {
			return fmt.Errorf("load ensemble registry: %w", err)
		}
		preset := registry.Get(presetName)
		if preset == nil {
			return fmt.Errorf("ensemble preset %q not found", presetName)
		}
		presetUsed = preset.Name
		if preset.DisplayName != "" {
			presetLabel = preset.DisplayName
		} else {
			presetLabel = preset.Name
		}
		modeIDs, err = preset.ResolveIDs(catalog)
		if err != nil {
			return fmt.Errorf("resolve preset modes: %w", err)
		}
		budget = mergeBudgetDefaults(preset.Budget, budget)
	} else {
		var err error
		modeIDs, err = resolveModeIDs(modes, catalog)
		if err != nil {
			return err
		}
	}

	payload, err := buildEnsembleEstimate(catalog, modeIDs, budget, budgetOverride)
	if err != nil {
		return err
	}
	payload.PresetName = presetUsed
	payload.PresetLabel = presetLabel

	return renderEnsembleEstimate(w, payload, format)
}

func buildEnsembleEstimate(catalog *ensemble.ModeCatalog, modeIDs []string, budget ensemble.BudgetConfig, budgetOverride int) (ensembleEstimateOutput, error) {
	if catalog == nil {
		return ensembleEstimateOutput{}, fmt.Errorf("mode catalog is nil")
	}
	if len(modeIDs) == 0 {
		return ensembleEstimateOutput{}, fmt.Errorf("no modes to estimate")
	}

	rows, modeTokens, err := buildEstimateRows(catalog, modeIDs)
	if err != nil {
		return ensembleEstimateOutput{}, err
	}

	effectiveBudget := budget.MaxTotalTokens
	if budgetOverride > 0 {
		effectiveBudget = budgetOverride
	}

	total := modeTokens + budget.SynthesisReserveTokens + budget.ContextReserveTokens
	warnings := estimateWarnings(total, effectiveBudget, budget.MaxTokensPerMode, rows)
	suggestions := suggestModeReplacements(catalog, rows, effectiveBudget, total)

	payload := ensembleEstimateOutput{
		GeneratedAt: time.Now().UTC(),
		Modes:       rows,
		Budget: ensembleEstimateBudget{
			MaxTokensPerMode:       budget.MaxTokensPerMode,
			MaxTotalTokens:         effectiveBudget,
			SynthesisReserveTokens: budget.SynthesisReserveTokens,
			ContextReserveTokens:   budget.ContextReserveTokens,
			EstimatedModeTokens:    modeTokens,
			EstimatedTotalTokens:   total,
			ModeCount:              len(rows),
			BudgetOverride:         budgetOverride,
		},
		Warnings:    warnings,
		Suggestions: suggestions,
	}

	return payload, nil
}

func buildEstimateRows(catalog *ensemble.ModeCatalog, modeIDs []string) ([]ensembleEstimateMode, int, error) {
	rows := make([]ensembleEstimateMode, 0, len(modeIDs))
	total := 0

	for _, modeID := range modeIDs {
		cost, mode, err := estimateModeCost(catalog, modeID)
		if err != nil {
			return nil, 0, err
		}

		valueScore := modeValueScore(mode)
		valuePerToken := 0.0
		if cost > 0 {
			valuePerToken = valueScore / float64(cost)
		}

		rows = append(rows, ensembleEstimateMode{
			ModeID:        mode.ID,
			ModeCode:      mode.Code,
			ModeName:      mode.Name,
			Category:      mode.Category.String(),
			Tier:          mode.Tier.String(),
			TokenEstimate: cost,
			ValueScore:    valueScore,
			ValuePerToken: valuePerToken,
		})
		total += cost
	}

	return rows, total, nil
}

func estimateModeCost(catalog *ensemble.ModeCatalog, modeID string) (int, *ensemble.ReasoningMode, error) {
	mode := catalog.GetMode(modeID)
	if mode == nil {
		return 0, nil, fmt.Errorf("mode %q not found in catalog", modeID)
	}

	card, err := catalog.GetModeCard(modeID)
	if err != nil {
		slog.Debug("mode card lookup failed", "mode", modeID, "err", err)
	}
	if card != nil && card.TypicalCost > 0 {
		return card.TypicalCost, mode, nil
	}

	return defaultModeTokenEstimate, mode, nil
}

func modeValueScore(mode *ensemble.ReasoningMode) float64 {
	if mode == nil {
		return 0
	}

	score := 1.0
	switch mode.Tier {
	case ensemble.TierAdvanced:
		score = 1.1
	case ensemble.TierExperimental:
		score = 1.2
	}
	return score
}

func estimateWarnings(total, budgetTotal, perModeBudget int, rows []ensembleEstimateMode) []string {
	var warnings []string

	if budgetTotal > 0 {
		if total > budgetTotal {
			warnings = append(warnings, fmt.Sprintf("estimated tokens (%d) exceed budget (%d)", total, budgetTotal))
		} else if float64(total) >= estimateNearBudgetThreshold*float64(budgetTotal) {
			warnings = append(warnings, fmt.Sprintf("estimated tokens (%d) are near budget (%d)", total, budgetTotal))
		}
	}

	if perModeBudget > 0 {
		for _, row := range rows {
			if row.TokenEstimate > perModeBudget {
				warnings = append(warnings, fmt.Sprintf("mode %q estimate (%d) exceeds per-mode budget (%d)", row.ModeID, row.TokenEstimate, perModeBudget))
			}
		}
	}

	return warnings
}

func suggestModeReplacements(catalog *ensemble.ModeCatalog, rows []ensembleEstimateMode, budgetTotal, estimatedTotal int) []ensembleEstimateSuggestion {
	if budgetTotal <= 0 || estimatedTotal <= budgetTotal {
		return nil
	}

	sorted := make([]ensembleEstimateMode, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].TokenEstimate > sorted[j].TokenEstimate
	})

	var suggestions []ensembleEstimateSuggestion
	remaining := estimatedTotal

	for _, row := range sorted {
		if remaining <= budgetTotal || len(suggestions) >= 3 {
			break
		}
		mode := catalog.GetMode(row.ModeID)
		if mode == nil {
			continue
		}
		replacement := bestReplacement(mode, row.TokenEstimate, row.ValuePerToken, catalog)
		if replacement == nil || replacement.TypicalCost <= 0 {
			continue
		}

		savings := row.TokenEstimate - replacement.TypicalCost
		if savings <= 0 {
			continue
		}

		suggestions = append(suggestions, ensembleEstimateSuggestion{
			ReplaceModeID:     row.ModeID,
			ReplaceModeName:   row.ModeName,
			ReplaceTokens:     row.TokenEstimate,
			SuggestedModeID:   replacement.ModeID,
			SuggestedModeName: replacement.Name,
			SuggestedTokens:   replacement.TypicalCost,
			SavingsTokens:     savings,
			Reason:            "higher value/cost in same category",
		})

		remaining -= savings
	}

	return suggestions
}

func bestReplacement(mode *ensemble.ReasoningMode, currentCost int, currentRatio float64, catalog *ensemble.ModeCatalog) *ensemble.ModeCard {
	if mode == nil || catalog == nil {
		return nil
	}

	candidates := catalog.ListByCategory(mode.Category)
	var best *ensemble.ModeCard
	bestRatio := currentRatio

	for _, cand := range candidates {
		if cand.ID == mode.ID {
			continue
		}
		card, err := catalog.GetModeCard(cand.ID)
		if err != nil || card == nil {
			continue
		}
		if card.TypicalCost <= 0 || card.TypicalCost >= currentCost {
			continue
		}

		ratio := modeValueScore(&cand) / float64(card.TypicalCost)
		if ratio > bestRatio {
			bestRatio = ratio
			best = card
		}
	}

	return best
}

func renderEnsembleEstimate(w io.Writer, payload ensembleEstimateOutput, format string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml", "yml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			_, err = w.Write([]byte("\n"))
			return err
		}
		return nil
	case "table", "text":
		if payload.PresetLabel != "" {
			fmt.Fprintf(w, "Preset: %s\n", payload.PresetLabel)
		}
		if payload.PresetName != "" && payload.PresetName != payload.PresetLabel {
			fmt.Fprintf(w, "Preset ID: %s\n", payload.PresetName)
		}
		fmt.Fprintf(w, "Modes: %d\n\n", len(payload.Modes))

		table := output.NewTable(w, "MODE", "CODE", "TIER", "EST TOKENS", "VALUE/TOKEN")
		for _, row := range payload.Modes {
			table.AddRow(
				row.ModeID,
				row.ModeCode,
				row.Tier,
				fmt.Sprintf("%d", row.TokenEstimate),
				fmt.Sprintf("%.4f", row.ValuePerToken),
			)
		}
		table.Render()

		fmt.Fprintf(w, "\nEstimated total: %d tokens\n", payload.Budget.EstimatedTotalTokens)
		if payload.Budget.MaxTotalTokens > 0 {
			fmt.Fprintf(w, "Budget total:    %d tokens\n", payload.Budget.MaxTotalTokens)
		}
		if payload.Budget.SynthesisReserveTokens > 0 || payload.Budget.ContextReserveTokens > 0 {
			fmt.Fprintf(w, "Reserves:        synthesis %d, context %d\n",
				payload.Budget.SynthesisReserveTokens, payload.Budget.ContextReserveTokens)
		}

		if len(payload.Warnings) > 0 {
			fmt.Fprintln(w, "\nWarnings:")
			for _, warn := range payload.Warnings {
				fmt.Fprintf(w, "  - %s\n", warn)
			}
		}

		if len(payload.Suggestions) > 0 {
			fmt.Fprintln(w, "\nSuggestions:")
			sTable := output.NewTable(w, "REPLACE", "WITH", "SAVINGS", "REASON")
			for _, s := range payload.Suggestions {
				replace := s.ReplaceModeID
				if s.ReplaceModeName != "" {
					replace = fmt.Sprintf("%s (%s)", s.ReplaceModeID, s.ReplaceModeName)
				}
				with := s.SuggestedModeID
				if s.SuggestedModeName != "" {
					with = fmt.Sprintf("%s (%s)", s.SuggestedModeID, s.SuggestedModeName)
				}
				sTable.AddRow(
					replace,
					with,
					fmt.Sprintf("%d", s.SavingsTokens),
					s.Reason,
				)
			}
			sTable.Render()
		}

		return nil
	default:
		return fmt.Errorf("invalid format %q (expected table, json, yaml)", format)
	}
}

func resolveModeIDs(inputs []string, catalog *ensemble.ModeCatalog) ([]string, error) {
	if catalog == nil {
		return nil, fmt.Errorf("mode catalog is nil")
	}

	seen := make(map[string]bool, len(inputs))
	result := make([]string, 0, len(inputs))

	for _, raw := range inputs {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		modeID, _, err := resolveModeID(token, catalog)
		if err != nil {
			return nil, err
		}
		if seen[modeID] {
			return nil, fmt.Errorf("duplicate mode %q", modeID)
		}
		seen[modeID] = true
		result = append(result, modeID)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid modes provided")
	}

	return result, nil
}

func resolveModeID(raw string, catalog *ensemble.ModeCatalog) (string, *ensemble.ReasoningMode, error) {
	if catalog == nil {
		return "", nil, fmt.Errorf("mode catalog is nil")
	}

	if mode := catalog.GetMode(raw); mode != nil {
		return mode.ID, mode, nil
	}
	// Try as code (case-insensitive)
	if mode := catalog.GetModeByCode(strings.ToUpper(raw)); mode != nil {
		return mode.ID, mode, nil
	}
	return "", nil, fmt.Errorf("mode %q not found (id or code)", raw)
}

func splitCommaSeparated(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}
