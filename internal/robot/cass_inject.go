// Package robot provides machine-readable output for AI agents.
// cass_inject.go provides CASS (Cross-Agent Search) query functionality
// for injecting relevant historical context into agent prompts.
package robot

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// CASSConfig holds configuration for CASS queries.
type CASSConfig struct {
	// Enabled controls whether CASS queries are performed.
	Enabled bool `json:"enabled"`

	// MaxResults limits the number of CASS hits to return.
	MaxResults int `json:"max_results"`

	// MaxAgeDays filters results to those within this many days.
	MaxAgeDays int `json:"max_age_days"`

	// MinRelevance is the minimum relevance score (0.0-1.0) to include results.
	// Note: CASS doesn't currently return relevance scores, so this is for future use.
	MinRelevance float64 `json:"min_relevance"`

	// PreferSameProject gives preference to results from the current workspace.
	PreferSameProject bool `json:"prefer_same_project"`

	// AgentFilter limits results to specific agent types (e.g., "claude", "codex").
	// Empty means all agents.
	AgentFilter []string `json:"agent_filter,omitempty"`
}

// DefaultCASSConfig returns sensible defaults for CASS queries.
func DefaultCASSConfig() CASSConfig {
	return CASSConfig{
		Enabled:           true,
		MaxResults:        5,
		MaxAgeDays:        30,
		MinRelevance:      0.0,
		PreferSameProject: true,
		AgentFilter:       nil,
	}
}

// CASSHit represents a single search result from CASS.
type CASSHit struct {
	// SourcePath is the path to the session file.
	SourcePath string `json:"source_path"`

	// LineNumber is the line in the session file.
	LineNumber int `json:"line_number"`

	// Agent is the agent type (e.g., "claude", "codex", "gemini").
	Agent string `json:"agent"`

	// Content is the matched content snippet (if available).
	Content string `json:"content,omitempty"`

	// Score is the relevance score (if available from CASS).
	Score float64 `json:"score,omitempty"`
}

// CASSQueryResult holds the results of a CASS query.
type CASSQueryResult struct {
	// Success indicates whether the query completed successfully.
	Success bool `json:"success"`

	// Query is the search query that was executed.
	Query string `json:"query"`

	// Hits contains the matching results.
	Hits []CASSHit `json:"hits"`

	// TotalMatches is the total number of matches (may be > len(Hits) if limited).
	TotalMatches int `json:"total_matches"`

	// QueryTime is how long the query took.
	QueryTime time.Duration `json:"query_time_ms"`

	// Error contains any error message.
	Error string `json:"error,omitempty"`

	// Keywords are the extracted keywords from the original prompt.
	Keywords []string `json:"keywords,omitempty"`
}

// cassSearchResponse matches the JSON structure returned by `cass search --json`.
type cassSearchResponse struct {
	Query        string `json:"query"`
	TotalMatches int    `json:"total_matches"`
	Hits         []struct {
		SourcePath string `json:"source_path"`
		LineNumber int    `json:"line_number"`
		Agent      string `json:"agent"`
		Content    string `json:"content,omitempty"`
		Score      float64 `json:"score,omitempty"`
	} `json:"hits"`
}

// QueryCASS queries CASS for relevant historical context based on the prompt.
// It extracts keywords from the prompt and searches for relevant past sessions.
func QueryCASS(prompt string, config CASSConfig) CASSQueryResult {
	start := time.Now()
	result := CASSQueryResult{
		Success: false,
		Query:   "",
		Hits:    []CASSHit{},
	}

	if !config.Enabled {
		result.Success = true
		return result
	}

	// Extract keywords from the prompt
	keywords := ExtractKeywords(prompt)
	result.Keywords = keywords

	if len(keywords) == 0 {
		result.Success = true
		result.Error = "no keywords extracted from prompt"
		return result
	}

	// Build the search query
	query := strings.Join(keywords, " ")
	result.Query = query

	// Check if CASS is available
	if !isCASSAvailable() {
		result.Error = "cass command not found"
		return result
	}

	// Build the cass search command
	args := []string{"search", query, "--json"}

	// Add limit
	if config.MaxResults > 0 {
		args = append(args, "--limit", itoa(config.MaxResults))
	}

	// Add age filter
	if config.MaxAgeDays > 0 {
		args = append(args, "--days", itoa(config.MaxAgeDays))
	}

	// Add agent filter
	for _, agent := range config.AgentFilter {
		args = append(args, "--agent", agent)
	}

	// Execute CASS search
	cmd := exec.Command("cass", args...)
	output, err := cmd.Output()
	result.QueryTime = time.Since(start)

	if err != nil {
		// Check if it's just no results vs actual error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Exit code 1 often means no results, which is fine
			result.Success = true
			return result
		}
		result.Error = err.Error()
		return result
	}

	// Parse the response
	var resp cassSearchResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		result.Error = "failed to parse CASS response: " + err.Error()
		return result
	}

	// Convert to our format
	result.TotalMatches = resp.TotalMatches
	for _, hit := range resp.Hits {
		result.Hits = append(result.Hits, CASSHit{
			SourcePath: hit.SourcePath,
			LineNumber: hit.LineNumber,
			Agent:      hit.Agent,
			Content:    hit.Content,
			Score:      hit.Score,
		})
	}

	result.Success = true
	return result
}

// ExtractKeywords extracts meaningful keywords from a prompt for CASS search.
// It filters out common stop words and short words, focusing on technical terms.
func ExtractKeywords(prompt string) []string {
	// Convert to lowercase for processing
	text := strings.ToLower(prompt)

	// Remove code blocks to avoid searching for code syntax
	text = removeCodeBlocks(text)

	// Tokenize: split on non-alphanumeric characters
	words := tokenize(text)

	// Filter words
	var keywords []string
	seen := make(map[string]bool)

	for _, word := range words {
		// Skip short words
		if len(word) < 3 {
			continue
		}

		// Skip stop words
		if isStopWord(word) {
			continue
		}

		// Skip if already seen (deduplicate)
		if seen[word] {
			continue
		}
		seen[word] = true

		keywords = append(keywords, word)
	}

	// Limit to most relevant keywords (first 10)
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}

	return keywords
}

// tokenize splits text into words, handling code identifiers like snake_case.
func tokenize(text string) []string {
	// Split on whitespace and punctuation, but keep underscores in identifiers
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// removeCodeBlocks removes markdown code blocks from text.
func removeCodeBlocks(text string) string {
	// Remove fenced code blocks
	re := regexp.MustCompile("(?s)```.*?```")
	text = re.ReplaceAllString(text, " ")

	// Remove inline code
	re = regexp.MustCompile("`[^`]+`")
	text = re.ReplaceAllString(text, " ")

	return text
}

// isStopWord returns true if the word is a common stop word.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		// Common English stop words
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"as": true, "is": true, "was": true, "are": true, "were": true,
		"been": true, "be": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true,
		"this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "they": true, "them": true, "their": true,
		"we": true, "you": true, "your": true, "our": true, "my": true,
		"me": true, "him": true, "her": true, "his": true, "she": true,
		"he": true, "i": true, "all": true, "each": true, "every": true,
		"both": true, "few": true, "more": true, "most": true, "other": true,
		"some": true, "such": true, "no": true, "nor": true, "not": true,
		"only": true, "own": true, "same": true, "so": true, "than": true,
		"too": true, "very": true, "just": true, "also": true, "now": true,
		"can": true, "get": true, "got": true, "how": true, "what": true,
		"when": true, "where": true, "which": true, "who": true, "why": true,
		"new": true, "use": true, "used": true, "using": true,
		"make": true, "made": true, "like": true, "want": true, "need": true,
		"please": true, "help": true, "here": true, "there": true,

		// Common coding task words (too generic to search)
		"code": true, "file": true, "function": true, "method": true,
		"class": true, "variable": true, "add": true, "create": true,
		"update": true, "delete": true, "remove": true, "change": true,
		"fix": true, "bug": true, "error": true, "test": true, "write": true,
		"read": true, "run": true, "start": true, "stop": true,
	}

	return stopWords[word]
}

// isCASSAvailable checks if the cass command is available.
func isCASSAvailable() bool {
	_, err := exec.LookPath("cass")
	return err == nil
}

// itoa converts int to string (simple helper to avoid strconv import for small use).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var result []byte
	neg := i < 0
	if neg {
		i = -i
	}

	for i > 0 {
		result = append([]byte{byte('0' + i%10)}, result...)
		i /= 10
	}

	if neg {
		result = append([]byte{'-'}, result...)
	}

	return string(result)
}
