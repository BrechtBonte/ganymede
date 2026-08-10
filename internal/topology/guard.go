package topology

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/BrechtBonte/ganymede/internal/session"
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

// ellipsis is what oneLine (internal/hooks) appends to a Reason it had to
// truncate. The pane a Blocked Session is waiting on always renders the real,
// untruncated text — so matching a truncated Reason against it has to drop
// the mark that text was cut, and treat what is left as a prefix rather than
// the whole of what the pane must say.
const ellipsis = "…"

// Approve answers the Session running as pid's dialog with the guard's
// default row: Y, the one keystroke every dialog gives stable, documented
// meaning whatever it is asking about (§7.2, §7.3). reason is the row's own
// account of what the Session is waiting for — "permission: <tool>" when the
// registry or the PermissionRequest hook could name one — and is what the
// pane is checked against before and after the key goes out.
func (h Harness) Approve(pid int, reason string) error {
	return h.answer(pid, reason, "Y")
}

// Deny declines the dialog: Esc, not N — the resolution's own choice, since
// Esc closes any dialog the same way whatever N happens to mean inside it
// (§7.3).
func (h Harness) Deny(pid int, reason string) error {
	return h.answer(pid, reason, "Escape")
}

// answer is the guarded send-keys engine, steps 2 through 4 of the guard
// (§7.2): capture-pane and check it still shows what reason says the Session
// is waiting for, send the one key, then give the pane up to redrawBudget to
// show that the send actually resolved it. A mismatch on either side sends
// no more than it already has — a dialog the guard could not verify keeps
// whatever it was already showing, rather than a stray key it never asked
// for.
//
// Step 1, the registry gate, is not here: it is the Dashboard's own row that
// carries the state and the timestamp to gate on, and asking tmux anything
// at all is already more than a row that fails that gate should cost.
func (h Harness) answer(pid int, reason, key string) error {
	target, err := h.locate(pid)
	if err != nil {
		return err
	}
	before, err := h.capturePane(target)
	if err != nil {
		return err
	}
	if !expected(before, reason) {
		return fmt.Errorf("pane %s does not show the dialog it was reported waiting on", target)
	}

	if err := h.sessions().run("send-keys", "-t", target, key); err != nil {
		return fmt.Errorf("send %s to the Session's pane: %w", key, err)
	}

	if !h.settled(target, reason) {
		return fmt.Errorf("pane %s still shows the dialog after %s was sent", target, key)
	}
	return nil
}

// settled polls the pane until it no longer shows the dialog reason
// describes, or redrawBudget runs out. Watching for the dialog to go rather
// than for the pane to merely look different keeps the check honest about
// what it is actually asking: not "did anything happen" but "did the thing
// the guard was told is blocking this Session stop blocking it".
func (h Harness) settled(target, reason string) bool {
	deadline := time.Now().Add(redrawBudget)
	for {
		after, err := h.capturePane(target)
		if err == nil && !expected(after, reason) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(redrawPoll)
	}
}

// capturePane is the pane's whole visible screen, which is what the guard
// checks before sending anything and again once it has.
func (h Harness) capturePane(target string) (string, error) {
	out, err := exec.Command("tmux", h.sessions().args("capture-pane", "-p", "-t", target)...).Output()
	if err != nil {
		return "", fmt.Errorf("capture the Session's pane: %w", err)
	}
	return string(out), nil
}

// expected reports whether a captured pane shows what a Blocked Session's
// own reason says it is waiting for: the tool name out of "permission:
// <tool>" when the registry or the PermissionRequest hook could name one
// (session.ToolOf), and the reason's own text otherwise — a reason the guard
// cannot place anywhere on the pane at all is the honest limit of what it
// can check without ever having seen that dialog rendered, and is treated as
// a mismatch rather than a license to send on faith.
func expected(pane, reason string) bool {
	text, ok := dialogText(reason)
	return ok && strings.Contains(pane, text)
}

// dialogText is the substring expected checks a pane for, and whether reason
// gave it one to look for at all.
func dialogText(reason string) (string, bool) {
	if tool, ok := session.ToolOf(reason); ok {
		return strings.TrimSuffix(tool, ellipsis), true
	}
	return strings.TrimSuffix(reason, ellipsis), reason != ""
}
