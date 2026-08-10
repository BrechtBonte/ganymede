package topology

import (
	"fmt"
	"strings"
	"time"
)

// Interrupt stops the Session running as pid's current turn with a bare
// guarded Esc — no text follows it, unlike InterruptAndSend. It is the "x"
// row (§7.3), with no confirmation dialog of its own: located's own
// precondition, an empty input box, is already "no dialog is visible in
// capture-pane" — a dialog draws over that box rather than leaving it empty,
// the same reading InterruptAndSend's own interrupt half (escaped) relies
// on.
func (h Harness) Interrupt(pid int) error {
	_, err := h.escaped(pid)
	return err
}

// End exits the Session running as pid gracefully: /exit pasted and
// submitted into its own input box — the "q" row (§7.3), reached only once
// the Dashboard's own confirmation has been answered.
//
// It does not reuse Send: Send's own postcondition is the box going back to
// empty, which is what a prompt Claude Code keeps running looks like once
// submitted — but /exit's whole point is that Claude Code stops running, so
// the box it hands back is never that one. exited is End's own reading of
// "it worked".
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
// read to keep polling through, unlike every other guarded check in this
// package.
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
