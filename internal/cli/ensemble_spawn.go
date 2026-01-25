//go:build ensemble_experimental
// +build ensemble_experimental

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

type ensembleSpawnOptions struct {
	Session       string
	Question      string
	Preset        string
	Modes         []string
	AllowAdvanced bool
	AgentMix      string
	Assignment    string
	Synthesis     string
	BudgetTotal   int
	BudgetPerMode int
	NoQuestions   bool
	NoCache       bool
	NoInject      bool
	Project       string
}

type ensembleSpawnOutput struct {
	Success     bool                  `json:"success"`
	GeneratedAt time.Time             `json:"generated_at"`
	Session     string                `json:"session"`
	ProjectDir  string                `json:"project_dir"`
	Question    string                `json:"question"`
	Preset      string                `json:"preset,omitempty"`
	Modes       []string              `json:"modes"`
	Assignment  string                `json:"assignment"`
	AgentMix    map[string]int        `json:"agent_mix,omitempty"`
	Synthesis   string                `json:"synthesis"`
	Budget      ensemble.BudgetConfig `json:"budget"`
	Status      string                `json:"status"`
	Injected    bool                  `json:"injected"`
	Error       string                `json:"error,omitempty"`
}

func newEnsembleSpawnCmd() *cobra.Command {
	opts := ensembleSpawnOptions{
		Assignment: "affinity",
	}

	cmd := &cobra.Command{
		Use:   "spawn <session>",
		Short: "Spawn a reasoning ensemble session",
		Long: `Spawn a reasoning ensemble session.

For the primary shorthand UX, prefer:
  ntm ensemble <ensemble-name> "<question>"`,
		Example: `  ntm ensemble spawn mysession --preset project-diagnosis --question "What are the main issues?"
  ntm ensemble spawn mysession --modes deductive,bayesian --question "Review this spec"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Session = args[0]
			return runEnsembleSpawn(cmd, opts)
		},
	}

	bindEnsembleSpawnFlags(cmd, &opts)
	return cmd
}

func bindEnsembleSpawnFlags(cmd *cobra.Command, opts *ensembleSpawnOptions) {
	bindEnsembleSharedFlags(cmd, opts)
	cmd.Flags().StringVarP(&opts.Question, "question", "q", "", "Question for agents to analyze (required)")
	cmd.Flags().StringVarP(&opts.Preset, "preset", "p", "", "Use pre-configured ensemble (preferred)")
	cmd.Flags().StringSliceVarP(&opts.Modes, "modes", "m", nil, "Explicit mode IDs or codes (advanced usage)")
}

func bindEnsembleSharedFlags(cmd *cobra.Command, opts *ensembleSpawnOptions) {
	cmd.Flags().BoolVar(&opts.AllowAdvanced, "allow-advanced", false, "Allow advanced/experimental modes")
	cmd.Flags().StringVar(&opts.AgentMix, "agent-mix", "", "Agent distribution (e.g., 'cc=3,cod=2,gmi=1')")
	cmd.Flags().StringVar(&opts.Assignment, "assignment", "affinity", "Assignment strategy: round-robin, affinity, category, explicit")
	cmd.Flags().StringVar(&opts.Synthesis, "synthesis", "", "Synthesis strategy override")
	cmd.Flags().IntVar(&opts.BudgetTotal, "budget-total", 0, "Override total token budget")
	cmd.Flags().IntVar(&opts.BudgetPerMode, "budget-per-agent", 0, "Override per-agent token cap")
	cmd.Flags().BoolVar(&opts.NoQuestions, "no-questions", false, "Skip targeted questions (future)")
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Bypass context pack cache")
	cmd.Flags().BoolVar(&opts.NoInject, "no-inject", false, "Create session without injecting prompts")
	cmd.Flags().StringVar(&opts.Project, "project", "", "Project directory (default: current dir)")
}

func applyEnsembleConfigDefaults(cmd *cobra.Command, opts *ensembleSpawnOptions) {
	effectiveCfg := cfg
	if effectiveCfg == nil {
		effectiveCfg = config.Default()
	}
	ensCfg := effectiveCfg.Ensemble

	flags := cmd.Flags()

	if opts.Preset == "" && len(opts.Modes) == 0 && ensCfg.DefaultEnsemble != "" {
		opts.Preset = ensCfg.DefaultEnsemble
	}
	if !flags.Changed("agent-mix") && strings.TrimSpace(opts.AgentMix) == "" && strings.TrimSpace(ensCfg.AgentMix) != "" {
		opts.AgentMix = ensCfg.AgentMix
	}
	if !flags.Changed("assignment") && strings.TrimSpace(ensCfg.Assignment) != "" {
		opts.Assignment = ensCfg.Assignment
	}
	if !flags.Changed("allow-advanced") {
		allow := ensCfg.AllowAdvanced
		switch strings.ToLower(strings.TrimSpace(ensCfg.ModeTierDefault)) {
		case "advanced", "experimental":
			allow = true
		}
		opts.AllowAdvanced = allow
	}
	if !flags.Changed("synthesis") && strings.TrimSpace(opts.Synthesis) == "" && strings.TrimSpace(ensCfg.Synthesis.Strategy) != "" {
		opts.Synthesis = ensCfg.Synthesis.Strategy
	}
	if !flags.Changed("budget-total") && opts.BudgetTotal == 0 && ensCfg.Budget.Total > 0 {
		opts.BudgetTotal = ensCfg.Budget.Total
	}
	if !flags.Changed("budget-per-agent") && opts.BudgetPerMode == 0 && ensCfg.Budget.PerAgent > 0 {
		opts.BudgetPerMode = ensCfg.Budget.PerAgent
	}
	if !flags.Changed("no-cache") && !ensCfg.Cache.Enabled {
		opts.NoCache = true
	}
}

func applyEnsembleConfigOverrides(target *ensemble.EnsembleConfig, ensCfg config.EnsembleConfig) {
	if target == nil {
		return
	}

	if target.Synthesis.Strategy == "" && strings.TrimSpace(ensCfg.Synthesis.Strategy) != "" {
		target.Synthesis.Strategy = ensemble.SynthesisStrategy(strings.TrimSpace(ensCfg.Synthesis.Strategy))
	}
	if ensCfg.Synthesis.MinConfidence > 0 {
		target.Synthesis.MinConfidence = ensCfg.Synthesis.MinConfidence
	}
	if ensCfg.Synthesis.MaxFindings > 0 {
		target.Synthesis.MaxFindings = ensCfg.Synthesis.MaxFindings
	}
	if ensCfg.Synthesis.IncludeRawOutputs {
		target.Synthesis.IncludeRawOutputs = true
	}
	if strings.TrimSpace(ensCfg.Synthesis.ConflictResolution) != "" {
		target.Synthesis.ConflictResolution = strings.TrimSpace(ensCfg.Synthesis.ConflictResolution)
	}

	if ensCfg.Budget.Synthesis > 0 {
		target.Budget.SynthesisReserveTokens = ensCfg.Budget.Synthesis
	}
	if ensCfg.Budget.ContextPack > 0 {
		target.Budget.ContextReserveTokens = ensCfg.Budget.ContextPack
	}

	target.Cache.Enabled = ensCfg.Cache.Enabled
	if ensCfg.Cache.TTLMinutes > 0 {
		target.Cache.TTL = time.Duration(ensCfg.Cache.TTLMinutes) * time.Minute
	}
	if strings.TrimSpace(ensCfg.Cache.CacheDir) != "" {
		target.Cache.CacheDir = config.ExpandHome(strings.TrimSpace(ensCfg.Cache.CacheDir))
	}
	if ensCfg.Cache.MaxEntries > 0 {
		target.Cache.MaxEntries = ensCfg.Cache.MaxEntries
	}
	target.Cache.ShareAcrossModes = ensCfg.Cache.ShareAcrossModes
	if ensCfg.Cache.CacheDir != "" || ensCfg.Cache.TTLMinutes > 0 || ensCfg.Cache.MaxEntries > 0 || !ensCfg.Cache.Enabled {
		target.CacheOverride = true
	}

	target.EarlyStop = ensemble.EarlyStopConfig{
		Enabled:             ensCfg.EarlyStop.Enabled,
		MinAgentsBeforeStop: ensCfg.EarlyStop.MinAgents,
		FindingsThreshold:   ensCfg.EarlyStop.FindingsThreshold,
		SimilarityThreshold: ensCfg.EarlyStop.SimilarityThreshold,
		WindowSize:          ensCfg.EarlyStop.WindowSize,
	}
}

func runEnsembleSpawn(cmd *cobra.Command, opts ensembleSpawnOptions) error {
	outputError := func(err error) error {
		if IsJSONOutput() {
			_ = output.PrintJSON(output.NewError(err.Error()))
			return err
		}
		return err
	}

	applyEnsembleConfigDefaults(cmd, &opts)

	if err := tmux.EnsureInstalled(); err != nil {
		return outputError(err)
	}

	opts.Session = strings.TrimSpace(opts.Session)
	if opts.Session == "" {
		return outputError(fmt.Errorf("session name is required"))
	}
	if err := tmux.ValidateSessionName(opts.Session); err != nil {
		return outputError(err)
	}
	if tmux.SessionExists(opts.Session) {
		return outputError(fmt.Errorf("session '%s' already exists", opts.Session))
	}

	opts.Question = strings.TrimSpace(opts.Question)
	if opts.Question == "" {
		return outputError(fmt.Errorf("question is required"))
	}

	opts.Preset = strings.TrimSpace(opts.Preset)
	if opts.Preset == "" && len(opts.Modes) == 0 {
		return outputError(fmt.Errorf("either --preset or --modes is required"))
	}
	if opts.Preset != "" && len(opts.Modes) > 0 {
		return outputError(fmt.Errorf("--preset and --modes are mutually exclusive"))
	}

	assignment := strings.ToLower(strings.TrimSpace(opts.Assignment))
	if assignment == "" {
		assignment = "affinity"
	}
	if !isValidEnsembleAssignment(assignment) {
		return outputError(fmt.Errorf("invalid assignment strategy %q (use round-robin, affinity, category, or explicit)", assignment))
	}
	if assignment == "explicit" && len(opts.Modes) == 0 {
		return outputError(fmt.Errorf("explicit assignment requires --modes mode:agent specs"))
	}

	if opts.BudgetPerMode < 0 || opts.BudgetTotal < 0 {
		return outputError(fmt.Errorf("budget overrides must be non-negative"))
	}

	projectDir, err := resolveEnsembleProjectDir(opts.Project)
	if err != nil {
		return outputError(err)
	}
	opts.Project = projectDir

	agentMix, err := parseAgentMix(opts.AgentMix)
	if err != nil {
		return outputError(err)
	}

	manager, err := buildEnsembleManager(projectDir)
	if err != nil {
		return outputError(err)
	}

	ensembleCfg := &ensemble.EnsembleConfig{
		SessionName:   opts.Session,
		Question:      opts.Question,
		Ensemble:      opts.Preset,
		Modes:         opts.Modes,
		AllowAdvanced: opts.AllowAdvanced,
		ProjectDir:    projectDir,
		AgentMix:      agentMix,
		Assignment:    assignment,
		SkipInject:    opts.NoInject,
	}

	ensDefaults := config.Default().Ensemble
	if cfg != nil {
		ensDefaults = cfg.Ensemble
	}
	applyEnsembleConfigOverrides(ensembleCfg, ensDefaults)

	if opts.NoCache {
		ensembleCfg.Cache = ensemble.CacheConfig{Enabled: false}
		ensembleCfg.CacheOverride = true
	}

	if strings.TrimSpace(opts.Synthesis) != "" {
		strategy, err := ensemble.ValidateOrMigrateStrategy(strings.TrimSpace(opts.Synthesis))
		if err != nil {
			return outputError(err)
		}
		ensembleCfg.Synthesis.Strategy = strategy
	}

	if opts.BudgetPerMode > 0 {
		ensembleCfg.Budget.MaxTokensPerMode = opts.BudgetPerMode
	}
	if opts.BudgetTotal > 0 {
		ensembleCfg.Budget.MaxTotalTokens = opts.BudgetTotal
	}

	state, err := manager.SpawnEnsemble(context.Background(), ensembleCfg)
	if err != nil && state == nil {
		return outputError(err)
	}

	out := buildEnsembleSpawnOutput(state, ensembleCfg, manager.Registry)
	if err != nil {
		out.Success = false
		out.Error = err.Error()
	}

	if IsJSONOutput() {
		_ = output.PrintJSON(out)
		if err != nil {
			return err
		}
		return nil
	}

	if err := renderEnsembleSpawnText(cmd.OutOrStdout(), out); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	return nil
}

func buildEnsembleManager(projectDir string) (*ensemble.EnsembleManager, error) {
	modeLoader := ensemble.NewModeLoader()
	if projectDir != "" {
		modeLoader.ProjectDir = projectDir
	}
	catalog, err := modeLoader.Load()
	if err != nil {
		return nil, err
	}

	ensembleLoader := ensemble.NewEnsembleLoader(catalog)
	if projectDir != "" {
		ensembleLoader.ProjectDir = projectDir
	}
	presets, err := ensembleLoader.Load()
	if err != nil {
		return nil, err
	}

	manager := ensemble.NewEnsembleManager()
	manager.Catalog = catalog
	manager.Registry = ensemble.NewEnsembleRegistry(presets, catalog)
	return manager, nil
}

func buildEnsembleSpawnOutput(state *ensemble.EnsembleSession, cfg *ensemble.EnsembleConfig, registry *ensemble.EnsembleRegistry) ensembleSpawnOutput {
	out := ensembleSpawnOutput{
		GeneratedAt: output.Timestamp(),
		Modes:       []string{},
		Injected:    !cfg.SkipInject,
		ProjectDir:  cfg.ProjectDir,
		Question:    cfg.Question,
		Assignment:  cfg.Assignment,
		AgentMix:    cfg.AgentMix,
		Budget:      resolveEnsembleSpawnBudget(cfg, registry),
	}

	if state == nil {
		out.Session = cfg.SessionName
		return out
	}

	out.Success = true
	out.Session = state.SessionName
	out.Preset = state.PresetUsed
	out.Status = state.Status.String()
	out.Synthesis = state.SynthesisStrategy.String()
	out.Modes = modesFromAssignments(state.Assignments)
	if out.Preset == "" {
		out.Preset = cfg.Ensemble
	}

	return out
}

func resolveEnsembleSpawnBudget(cfg *ensemble.EnsembleConfig, registry *ensemble.EnsembleRegistry) ensemble.BudgetConfig {
	budget := ensemble.DefaultBudgetConfig()
	if registry != nil && cfg.Ensemble != "" {
		if preset := registry.Get(cfg.Ensemble); preset != nil {
			budget = mergeBudgetDefaults(preset.Budget, budget)
		}
	}
	if cfg.Budget.MaxTokensPerMode > 0 {
		budget.MaxTokensPerMode = cfg.Budget.MaxTokensPerMode
	}
	if cfg.Budget.MaxTotalTokens > 0 {
		budget.MaxTotalTokens = cfg.Budget.MaxTotalTokens
	}
	return budget
}

func modesFromAssignments(assignments []ensemble.ModeAssignment) []string {
	modes := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.ModeID != "" {
			modes = append(modes, assignment.ModeID)
		}
	}
	return modes
}

func renderEnsembleSpawnText(w io.Writer, out ensembleSpawnOutput) error {
	if !out.Success && out.Error != "" {
		_, _ = fmt.Fprintf(w, "Ensemble spawn failed: %s\n", out.Error)
	}
	if out.Session != "" {
		_, _ = fmt.Fprintf(w, "Session: %s\n", out.Session)
	}
	if out.Preset != "" {
		_, _ = fmt.Fprintf(w, "Ensemble: %s\n", out.Preset)
	}
	if len(out.Modes) > 0 {
		_, _ = fmt.Fprintf(w, "Modes: %s\n", strings.Join(out.Modes, ", "))
	}
	if out.Assignment != "" {
		_, _ = fmt.Fprintf(w, "Assignment: %s\n", out.Assignment)
	}
	if out.Synthesis != "" {
		_, _ = fmt.Fprintf(w, "Synthesis: %s\n", out.Synthesis)
	}
	if out.Budget.MaxTokensPerMode > 0 || out.Budget.MaxTotalTokens > 0 {
		_, _ = fmt.Fprintf(w, "Budget: per-mode=%d total=%d\n", out.Budget.MaxTokensPerMode, out.Budget.MaxTotalTokens)
	}
	if out.Status != "" {
		_, _ = fmt.Fprintf(w, "Stage: %s\n", out.Status)
	}
	if !out.Injected {
		_, _ = fmt.Fprintln(w, "Prompts not injected (--no-inject)")
	}
	return nil
}

func resolveEnsembleProjectDir(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		dir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		return dir, nil
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	return abs, nil
}

var sessionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func defaultEnsembleSessionName(projectDir string) string {
	base := filepath.Base(projectDir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "ensemble"
	}
	sanitized := sessionNameSanitizer.ReplaceAllString(base, "-")
	sanitized = strings.Trim(sanitized, "-_")
	if sanitized == "" {
		sanitized = "ensemble"
	}
	return sanitized
}

func uniqueEnsembleSessionName(base string) string {
	name := base
	for i := 1; tmux.SessionExists(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func isValidEnsembleAssignment(value string) bool {
	switch value {
	case "round-robin", "affinity", "explicit", "category":
		return true
	default:
		return false
	}
}

func parseAgentMix(value string) (map[string]int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	mix := make(map[string]int)
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("agent-mix entry %q must be type=count", part)
		}
		agentType := normalizeEnsembleAgentType(kv[0])
		if agentType == "" {
			return nil, fmt.Errorf("agent-mix entry %q has invalid agent type", part)
		}
		count, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			return nil, fmt.Errorf("agent-mix entry %q has invalid count: %v", part, err)
		}
		if count < 1 {
			return nil, fmt.Errorf("agent-mix entry %q must be >= 1", part)
		}
		mix[agentType] += count
	}
	if len(mix) == 0 {
		return nil, nil
	}
	return mix, nil
}

func normalizeEnsembleAgentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cc", "claude", "claude-code":
		return "cc"
	case "cod", "codex":
		return "cod"
	case "gmi", "gemini":
		return "gmi"
	default:
		return ""
	}
}
