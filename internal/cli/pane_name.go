package cli

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/Dicklesworthstone/ntm/internal/persona"
)

// PaneInfo represents parsed pane name information
type PaneInfo struct {
	Session string
	Type    AgentType // cc, cod, gmi
	Index   int
	Variant string // model alias OR persona name, may be empty
}

// VariantType indicates the type of variant in a pane name
type VariantType string

const (
	VariantNone    VariantType = "none"
	VariantModel   VariantType = "model"
	VariantPersona VariantType = "persona"
)

// paneNameRegex matches the NTM pane naming convention:
// session__type_index or session__type_index_variant
var paneNameRegex = regexp.MustCompile(`^(.+)__(\w+)_(\d+)(?:_(\w+))?$`)

// ParsePaneName parses an NTM pane title into its components
func ParsePaneName(title string) (*PaneInfo, error) {
	matches := paneNameRegex.FindStringSubmatch(title)
	if matches == nil {
		return nil, fmt.Errorf("invalid pane name format: %q", title)
	}

	index, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid pane index in %q: %w", title, err)
	}

	return &PaneInfo{
		Session: matches[1],
		Type:    AgentType(matches[2]),
		Index:   index,
		Variant: matches[4], // May be empty string
	}, nil
}

// FormatPaneName creates a pane title from components
// Format: {session}__{type}_{index} or {session}__{type}_{index}_{variant}
func FormatPaneName(session string, agentType AgentType, index int, variant string) string {
	if variant == "" {
		return fmt.Sprintf("%s__%s_%d", session, agentType, index)
	}
	return fmt.Sprintf("%s__%s_%d_%s", session, agentType, index, variant)
}

// VariantTypeFor determines whether a variant is a persona or model
// Priority: check personas first (since persona implies model)
// Uses the persona registry to check for known persona names.
func VariantTypeFor(variant, projectDir string) VariantType {
	if variant == "" {
		return VariantNone
	}

	// Try to load persona registry and check if it's a known persona
	if registry, err := persona.LoadRegistry(projectDir); err == nil {
		if _, found := registry.Get(variant); found {
			return VariantPersona
		}
	}

	// Otherwise assume it's a model alias
	return VariantModel
}

// VariantType determines whether the variant is a persona or model.
// Note: This method uses empty string for project dir. For accurate
// persona detection, use VariantTypeFor with the actual project directory.
func (p *PaneInfo) VariantType() VariantType {
	return VariantTypeFor(p.Variant, "")
}

// HasVariant returns true if a variant is set
func (p *PaneInfo) HasVariant() bool {
	return p.Variant != ""
}

// MatchesVariant checks if this pane matches a variant filter
// Empty filter matches all, otherwise requires exact match
func (p *PaneInfo) MatchesVariant(filter string) bool {
	if filter == "" {
		return true
	}
	return p.Variant == filter
}

// ResolveVariant determines the variant to use for a new pane
// Priority: persona > model > empty (for backwards compatibility)
func ResolveVariant(persona, model string) string {
	if persona != "" {
		return persona
	}
	return model
}
