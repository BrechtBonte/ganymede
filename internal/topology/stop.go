package topology

import (
	"fmt"
	"strings"
	"time"
)

// redrawBudget is how long the guard gives a pane to redraw after a key is
// sent before it gives up and reports a mismatch, polling rather than
// pausing once: tmux delivers the keystroke to the pty at once, but the
// shell or TUI underneath still has to read it, act on it and redraw, and a
// machine under load can take longer than any single fixed pause would
// assume. redrawPoll is how often it looks again while it waits.
const (
	redrawBudget = 500 * time.Millisecond
	redrawPoll   = 20 * time.Millisecond
)

// End exits the Session running as pid gracefully: /exit pasted and
// submitted into its own input box. Takeover (internal/dashboard's
// claim.go) is its only caller, ending an Idle session's occupant before
// claiming its root.
//
// exited, not the box going back to empty, is End's own reading of "it
// worked": /exit's whole point is that Claude Code stops running, so the
// box it hands back is never one a submit that kept running would leave.
func (h Harness) End(pid int) error {
	target, err := h.located(pid, "end")
	if err != nil {
		return err
	}
	if err := h.pasted(target, "/exit"); err != nil {
		return err
	}
	if !h.exited(target) {
		return fmt.Errorf("pane %s still shows Claude Code's own prompt after /exit was sent", target)
	}
	return nil
}

// exited polls the pane until it no longer shows Claude Code's own input
// marker anywhere on it, or the pane itself is gone — either is "Claude Code
// is no longer here" — or redrawBudget runs out.
//
// A Session quitting hands its pane back to whatever shell hosted it (a
// bare shell prompt, never the ❯ marker) when Claude Code was started as a
// command inside an already-running shell, the way a Main root's own
// Session is; a Worktree session, started with `claude` as the pane's own
// command (WorktreeCommand), takes the pane down with it the moment that
// process quits, since nothing else is left running in it — which is why a
// capture-pane error counts as "gone" here rather than as an inconclusive
// read to keep polling through.
func (h Harness) exited(target string) bool {
	deadline := time.Now().Add(redrawBudget)
	for {
		pane, err := h.capturePaneJoined(target)
		if err != nil || !strings.Contains(pane, inputMarker) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(redrawPoll)
	}
}
