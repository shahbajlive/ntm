// Package prompt provides utilities for building and manipulating prompts.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileSpec represents a parsed file specification with optional line range
type FileSpec struct {
	Path      string
	StartLine int // 0 = from beginning
	EndLine   int // 0 = to end
}

// MaxFileSize is the maximum file size allowed for context injection (1MB)
const MaxFileSize = 1 * 1024 * 1024

func isLineRangeSuffix(s string) bool {
	if s == "" {
		return false
	}
	hyphens := 0
	digits := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '-':
			hyphens++
			if hyphens > 1 {
				return false
			}
		default:
			return false
		}
	}

	// Must include at least one digit; a lone "-" is not a valid range.
	if digits == 0 {
		return false
	}

	// If a hyphen is present, at least one side must be non-empty.
	if hyphens == 1 {
		parts := strings.SplitN(s, "-", 2)
		return !(parts[0] == "" && parts[1] == "")
	}

	return true
}

// ParseFileSpec parses a file spec string like "path", "path:10-50", "path:10-", "path:-50"
func ParseFileSpec(spec string) (FileSpec, error) {
	fs := FileSpec{}

	// Check for line range suffix
	colonIdx := strings.LastIndex(spec, ":")
	if colonIdx == -1 || colonIdx == len(spec)-1 {
		// No line range, just path
		fs.Path = spec
		return fs, nil
	}

	after := spec[colonIdx+1:]
	if !isLineRangeSuffix(after) {
		// Not a strict line range suffix, treat whole thing as path.
		// This avoids mis-parsing paths like Windows drives: C:\foo\bar.go
		fs.Path = spec
		return fs, nil
	}

	fs.Path = spec[:colonIdx]

	// Parse line range (formats: "10-50", "10-", "-50")
	parts := strings.SplitN(after, "-", 2)
	if len(parts) == 1 {
		// Single line number
		line, err := strconv.Atoi(parts[0])
		if err != nil || line < 1 {
			return fs, fmt.Errorf("invalid line: %s", parts[0])
		}
		fs.StartLine = line
		fs.EndLine = line
	} else {
		// Range: "start-end", "start-", "-end"
		if parts[0] != "" {
			start, err := strconv.Atoi(parts[0])
			if err != nil || start < 1 {
				return fs, fmt.Errorf("invalid start line: %s", parts[0])
			}
			fs.StartLine = start
		}
		if parts[1] != "" {
			end, err := strconv.Atoi(parts[1])
			if err != nil || end < 1 {
				return fs, fmt.Errorf("invalid end line: %s", parts[1])
			}
			fs.EndLine = end
		}
	}

	if fs.StartLine > 0 && fs.EndLine > 0 && fs.StartLine > fs.EndLine {
		return fs, fmt.Errorf("invalid line range: %d-%d", fs.StartLine, fs.EndLine)
	}

	return fs, nil
}

// InjectFiles reads the specified files and prepends them to the prompt
// with proper formatting (code fences, language detection, headers).
func InjectFiles(specs []FileSpec, prompt string) (string, error) {
	if len(specs) == 0 {
		return prompt, nil
	}

	var parts []string

	for _, spec := range specs {
		// Check file size first
		info, err := os.Stat(spec.Path)
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", spec.Path, err)
		}
		if info.Size() > MaxFileSize {
			return "", fmt.Errorf("file %s is too large (%d bytes > %d limit)", spec.Path, info.Size(), MaxFileSize)
		}

		content, err := readFileRange(spec)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", spec.Path, err)
		}

		// Check for binary content
		if isBinary(content) {
			return "", fmt.Errorf("file %s appears to be binary (use text files only)", spec.Path)
		}

		lang := detectLanguage(spec.Path)
		header := fmt.Sprintf("# File: %s", spec.Path)
		if spec.StartLine > 0 || spec.EndLine > 0 {
			if spec.EndLine > 0 {
				header += fmt.Sprintf(" (lines %d-%d)", spec.StartLine, spec.EndLine)
			} else {
				header += fmt.Sprintf(" (from line %d)", spec.StartLine)
			}
		}

		block := fmt.Sprintf("%s\n```%s\n%s\n```", header, lang, content)
		parts = append(parts, block)
	}

	// Add separator and prompt
	parts = append(parts, "---\n\n"+prompt)

	return strings.Join(parts, "\n\n"), nil
}

// readFileRange reads a file, optionally extracting a specific line range.
func readFileRange(spec FileSpec) (string, error) {
	f, err := os.Open(spec.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// If no line range, read entire file (size already checked in InjectFiles)
	if spec.StartLine == 0 && spec.EndLine == 0 {
		content, err := os.ReadFile(spec.Path)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(content), "\n"), nil
	}

	// Read line range using bufio.Scanner to handle long lines gracefully
	var lines []string
	scanner := bufio.NewScanner(f)
	// Set max token size to MaxFileSize (1MB) to prevent OOM on single huge line
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, MaxFileSize)

	lineNum := 0
	startLine := spec.StartLine
	if startLine == 0 {
		startLine = 1
	}

	for scanner.Scan() {
		lineNum++
		if lineNum >= startLine {
			if spec.EndLine > 0 && lineNum > spec.EndLine {
				break
			}
			lines = append(lines, scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scanning file: %w", err)
	}

	return strings.Join(lines, "\n"), nil
}

// detectLanguage maps file extensions to markdown language identifiers.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	langMap := map[string]string{
		".py":         "python",
		".go":         "go",
		".js":         "javascript",
		".jsx":        "javascript",
		".ts":         "typescript",
		".tsx":        "typescript",
		".rs":         "rust",
		".rb":         "ruby",
		".java":       "java",
		".c":          "c",
		".cpp":        "cpp",
		".cc":         "cpp",
		".h":          "c",
		".hpp":        "cpp",
		".cs":         "csharp",
		".swift":      "swift",
		".kt":         "kotlin",
		".scala":      "scala",
		".php":        "php",
		".sh":         "bash",
		".bash":       "bash",
		".zsh":        "bash",
		".fish":       "fish",
		".sql":        "sql",
		".json":       "json",
		".yaml":       "yaml",
		".yml":        "yaml",
		".toml":       "toml",
		".xml":        "xml",
		".html":       "html",
		".css":        "css",
		".scss":       "scss",
		".sass":       "sass",
		".less":       "less",
		".md":         "markdown",
		".r":          "r",
		".R":          "r",
		".lua":        "lua",
		".pl":         "perl",
		".pm":         "perl",
		".ex":         "elixir",
		".exs":        "elixir",
		".erl":        "erlang",
		".hs":         "haskell",
		".ml":         "ocaml",
		".vim":        "vim",
		".el":         "elisp",
		".clj":        "clojure",
		".tf":         "terraform",
		".vue":        "vue",
		".svelte":     "svelte",
		".dockerfile": "dockerfile",
		".make":       "makefile",
		".cmake":      "cmake",
		".proto":      "protobuf",
		".graphql":    "graphql",
		".gql":        "graphql",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}

	// Check for Dockerfile (no extension)
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" {
		return "dockerfile"
	}
	if base == "makefile" || base == "gnumakefile" {
		return "makefile"
	}

	return "" // No language hint
}

// isBinary checks if content appears to be binary (contains null bytes or
// high proportion of non-printable characters).
func isBinary(content string) bool {
	if len(content) == 0 {
		return false
	}

	// Check first 8KB for null bytes or high non-printable ratio
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}

	nonPrintable := 0
	for i := 0; i < checkLen; i++ {
		b := content[i]
		if b == 0 {
			return true // Null byte = definitely binary
		}
		// Count non-printable (excluding common whitespace)
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			nonPrintable++
		}
	}

	// If more than 10% non-printable, likely binary
	return float64(nonPrintable)/float64(checkLen) > 0.1
}
