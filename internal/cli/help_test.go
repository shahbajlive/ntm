package cli

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

func stripANSI(str string) string {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansi.ReplaceAllString(str, "")
}

func TestPrintStunningHelp(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	// Run function
	PrintStunningHelp()

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	output := stripANSI(string(out))

	// Verify key components exist
	expected := []string{
		"Named Tmux Session Manager for AI Agents", // Subtitle
		"SESSION CREATION",                         // Section 1
		"AGENT MANAGEMENT",                         // Section 2
		"spawn",                                    // Command
		"Create session and launch agents",         // Description
		"Aliases:",                                 // Footer
	}

	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("Expected help output to contain %q, but it didn't", exp)
		}
	}
}

func TestPrintCompactHelp(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	// Run function
	PrintCompactHelp()

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	output := stripANSI(string(out))

	// Verify key components exist
	expected := []string{
		"NTM - Named Tmux Manager",
		"Commands:",
		"spawn",
		"Send prompts to agents",
		"Shell setup:",
	}

	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("Expected compact help output to contain %q, but it didn't", exp)
		}
	}
}
