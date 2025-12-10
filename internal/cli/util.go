package cli

import (
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// isInteractive returns true when both stdin and stdout are TTYs. Used to guard
// prompts in commands that would otherwise block automated runs (tests/CI).
func isInteractive() bool {
	return (isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())) &&
		(isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()))
}

// parseEditorCommand splits the editor string into command and arguments.
// It handles simple spaces.
func parseEditorCommand(editor string) (string, []string) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}
