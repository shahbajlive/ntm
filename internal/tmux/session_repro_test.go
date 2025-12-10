package tmux

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// This test reproduces the issue where a colon in a pane title
// causes GetPanes to misparse the tmux output.
func TestGetPanesWithColonInTitle(t *testing.T) {
	if !IsInstalled() {
		t.Skip("tmux not installed")
	}

	// Create a session
	name := fmt.Sprintf("ntm_repro_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = KillSession(name) })

	if err := CreateSession(name, os.TempDir()); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Get the first pane
	panes, err := GetPanes(name)
	if err != nil {
		t.Fatalf("GetPanes failed: %v", err)
	}
	if len(panes) == 0 {
		t.Fatal("no panes found")
	}
	paneID := panes[0].ID

	// Set a title containing a colon
	badTitle := "title:with:colons"
	if err := SetPaneTitle(paneID, badTitle); err != nil {
		t.Fatalf("SetPaneTitle failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Try to get panes again
	panes, err = GetPanes(name)
	if err != nil {
		t.Fatalf("GetPanes failed after setting bad title: %v", err)
	}

	// Verify parsing
	if len(panes) == 0 {
		t.Fatal("no panes found after setting bad title")
	}

	p := panes[0]
	t.Logf("Parsed pane: %+v", p)

	if p.Title != badTitle {
		t.Errorf("Title mismatch. Got %q, want %q", p.Title, badTitle)
	}

	// If parsing shifted, Width/Height might be 0
	if p.Width == 0 || p.Height == 0 {
		t.Error("Pane dimensions parsed as 0, likely due to split error")
	}
}
