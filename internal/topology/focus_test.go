package topology_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// activePane is which of the dock's two panes currently has keyboard focus.
func activePane(t *testing.T, h topology.Harness) string {
	t.Helper()
	for _, line := range strings.Split(tmuxOn(t, h.DockSocket, "list-panes", "-t", "=dock",
		"-F", "#{pane_index} #{pane_active}"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "1" {
			return fields[0]
		}
	}
	return ""
}

// Focus is the dock-level move alt+g makes — Enter's own way of finishing
// what Jump or Open already started.
func TestFocusMovesKeyboardFocusToTheWorkingClient(t *testing.T) {
	h := jumpable(t)

	// Away from the working client the dock started on, so Focus has
	// something to do.
	tmuxOn(t, h.DockSocket, "select-pane", "-t", "=dock:0.0")
	if got := activePane(t, h); got != "0" {
		t.Fatalf("active dock pane is %q, want the Dashboard's own pane (0)", got)
	}

	if err := h.Focus(); err != nil {
		t.Fatalf("Focus: %v", err)
	}

	if got := activePane(t, h); got != "1" {
		t.Errorf("active dock pane is %q, want the working client's pane (1)", got)
	}
}
