package robot

import (
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		minWords int // minimum expected keywords
		maxWords int // maximum expected keywords
		contains []string
		excludes []string
	}{
		{
			name:     "simple prompt",
			prompt:   "Fix the authentication bug in the login handler",
			minWords: 2,
			maxWords: 5,
			contains: []string{"authentication", "login", "handler"},
			excludes: []string{"the", "in", "fix", "bug"}, // stop words
		},
		{
			name:     "technical prompt",
			prompt:   "Implement retry logic with exponential backoff for database connections",
			minWords: 3,
			maxWords: 8,
			contains: []string{"retry", "logic", "exponential", "backoff", "database", "connections"},
			excludes: []string{"with", "for"},
		},
		{
			name:     "prompt with code block",
			prompt:   "Fix this function:\n```go\nfunc hello() { return }\n```\nThe return statement is wrong",
			minWords: 1,
			maxWords: 5,
			contains: []string{"return", "statement", "wrong"},
			excludes: []string{"func", "hello"}, // code block content should be removed
		},
		{
			name:     "prompt with inline code",
			prompt:   "The `getUserByID` function returns nil when user is not found",
			minWords: 2,
			maxWords: 6,
			contains: []string{"returns", "nil", "user", "found"},
			excludes: []string{"getuserbyid"}, // inline code should be removed
		},
		{
			name:     "empty prompt",
			prompt:   "",
			minWords: 0,
			maxWords: 0,
		},
		{
			name:     "only stop words",
			prompt:   "the and or but",
			minWords: 0,
			maxWords: 0,
		},
		{
			name:     "snake_case identifiers",
			prompt:   "Update the user_profile and order_items tables",
			minWords: 2,
			maxWords: 5,
			contains: []string{"user_profile", "order_items", "tables"},
		},
		{
			name:     "kebab-case identifiers",
			prompt:   "Check the api-gateway and load-balancer configs",
			minWords: 2,
			maxWords: 5,
			contains: []string{"api-gateway", "load-balancer", "configs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := ExtractKeywords(tt.prompt)

			// Check count bounds
			if len(keywords) < tt.minWords {
				t.Errorf("ExtractKeywords() got %d keywords, want at least %d\nKeywords: %v",
					len(keywords), tt.minWords, keywords)
			}
			if len(keywords) > tt.maxWords {
				t.Errorf("ExtractKeywords() got %d keywords, want at most %d\nKeywords: %v",
					len(keywords), tt.maxWords, keywords)
			}

			// Check required keywords
			keywordSet := make(map[string]bool)
			for _, k := range keywords {
				keywordSet[k] = true
			}

			for _, required := range tt.contains {
				if !keywordSet[required] {
					t.Errorf("ExtractKeywords() missing required keyword %q\nKeywords: %v",
						required, keywords)
				}
			}

			// Check excluded keywords (stop words)
			for _, excluded := range tt.excludes {
				if keywordSet[excluded] {
					t.Errorf("ExtractKeywords() should not contain stop word %q\nKeywords: %v",
						excluded, keywords)
				}
			}
		})
	}
}

func TestExtractKeywords_Deduplication(t *testing.T) {
	prompt := "user user user authentication authentication"
	keywords := ExtractKeywords(prompt)

	// Count occurrences
	counts := make(map[string]int)
	for _, k := range keywords {
		counts[k]++
	}

	for word, count := range counts {
		if count > 1 {
			t.Errorf("ExtractKeywords() has duplicate keyword %q (count: %d)", word, count)
		}
	}
}

func TestExtractKeywords_MaxLimit(t *testing.T) {
	// Generate a prompt with many unique words
	prompt := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen"
	keywords := ExtractKeywords(prompt)

	if len(keywords) > 10 {
		t.Errorf("ExtractKeywords() returned %d keywords, should be limited to 10", len(keywords))
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "simple words",
			text: "hello world",
			want: []string{"hello", "world"},
		},
		{
			name: "with punctuation",
			text: "hello, world!",
			want: []string{"hello", "world"},
		},
		{
			name: "snake_case",
			text: "user_profile",
			want: []string{"user_profile"},
		},
		{
			name: "kebab-case",
			text: "api-gateway",
			want: []string{"api-gateway"},
		},
		{
			name: "mixed",
			text: "user_profile api-gateway normalWord",
			want: []string{"user_profile", "api-gateway", "normalWord"},
		},
		{
			name: "with numbers",
			text: "error404 v2api",
			want: []string{"error404", "v2api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize() got %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRemoveCodeBlocks(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "fenced code block",
			text: "before ```go\ncode here\n``` after",
			want: "before   after",
		},
		{
			name: "inline code",
			text: "the `function` name",
			want: "the   name",
		},
		{
			name: "multiple code blocks",
			text: "start ```code1``` middle ```code2``` end",
			want: "start   middle   end",
		},
		{
			name: "no code",
			text: "plain text here",
			want: "plain text here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeCodeBlocks(tt.text)
			if got != tt.want {
				t.Errorf("removeCodeBlocks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsStopWord(t *testing.T) {
	// Test some stop words
	stopWords := []string{"the", "a", "is", "are", "and", "or", "but", "in", "on"}
	for _, word := range stopWords {
		if !isStopWord(word) {
			t.Errorf("isStopWord(%q) = false, want true", word)
		}
	}

	// Test some non-stop words
	nonStopWords := []string{"database", "authentication", "handler", "retry", "exponential"}
	for _, word := range nonStopWords {
		if isStopWord(word) {
			t.Errorf("isStopWord(%q) = true, want false", word)
		}
	}
}

func TestDefaultCASSConfig(t *testing.T) {
	config := DefaultCASSConfig()

	if !config.Enabled {
		t.Error("DefaultCASSConfig().Enabled should be true")
	}
	if config.MaxResults != 5 {
		t.Errorf("DefaultCASSConfig().MaxResults = %d, want 5", config.MaxResults)
	}
	if config.MaxAgeDays != 30 {
		t.Errorf("DefaultCASSConfig().MaxAgeDays = %d, want 30", config.MaxAgeDays)
	}
	if !config.PreferSameProject {
		t.Error("DefaultCASSConfig().PreferSameProject should be true")
	}
}

func TestQueryCASS_Disabled(t *testing.T) {
	config := CASSConfig{
		Enabled: false,
	}

	result := QueryCASS("test prompt", config)

	if !result.Success {
		t.Error("QueryCASS with disabled config should succeed")
	}
	if len(result.Hits) != 0 {
		t.Error("QueryCASS with disabled config should return no hits")
	}
}

func TestQueryCASS_EmptyKeywords(t *testing.T) {
	config := DefaultCASSConfig()

	// Prompt with only stop words should extract no keywords
	result := QueryCASS("the and or but", config)

	if !result.Success {
		t.Error("QueryCASS with no keywords should still succeed")
	}
	if result.Error != "no keywords extracted from prompt" {
		t.Errorf("QueryCASS error = %q, want 'no keywords extracted from prompt'", result.Error)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{100, "100"},
		{-1, "-1"},
		{-100, "-100"},
		{12345, "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := itoa(tt.input)
			if got != tt.want {
				t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
