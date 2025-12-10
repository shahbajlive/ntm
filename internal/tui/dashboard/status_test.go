package dashboard

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestStatusUpdateSetsPaneStateAndTimestamp(t *testing.T) {
	t.Parallel()

	m := New("session")
	m.panes = []tmux.Pane{
		{ID: "%1", Index: 0, Title: "session__cod_1", Type: tmux.AgentCodex},
	}
	m.paneStatus[0] = PaneStatus{}

	now := time.Now()
	msg := StatusUpdateMsg{
		Statuses: []status.AgentStatus{
			{PaneID: "%1", State: status.StateIdle, UpdatedAt: now},
		},
		Time: now,
	}

	updated, _ := m.Update(msg)
	m2 := updated.(Model)

	if got := m2.paneStatus[0].State; got != "idle" {
		t.Fatalf("expected pane state idle, got %q", got)
	}
	if m2.lastRefresh.IsZero() {
		t.Fatalf("expected lastRefresh to be set")
	}
}
