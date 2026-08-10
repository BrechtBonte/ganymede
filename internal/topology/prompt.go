package topology

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// bufferSeq tells apart every named buffer sendInto ever creates within this
// process, so two guarded sends racing each other — a real shape, since the
// Dashboard fires each one off the main loop as its own goroutine — can
// never end up naming the same buffer even when the clock they would
// otherwise be told apart by reads the same instant for both.
var bufferSeq atomic.Uint64

// inputMarker is what Claude Code draws at its own input box, whether the
// turn is Idle, Ready or still Working — verified against the real CLI,
// which draws the box identically in all three; only what sits above it
// differs. It is the guard's own signal that the box a prompt-send reaches
// for is actually there, and empty, before anything is typed into it
// (§7.2).
const inputMarker = "❯"

// emptyInputLine reports whether pane shows the input box with nothing typed
// into it: the marker alone on a line of its own.
func emptyInputLine(pane string) bool {
	return hasInputLine(pane, "")
}

// hasInputLine reports whether pane shows the input box carrying exactly
// text — empty for the box with nothing typed into it, or the box showing
// the text a paste has just landed. Claude Code's own input box redraws live
// as text lands, before Enter ever submits it (verified against the real
// CLI), which is what lets the guard check the paste actually arrived rather
// than trusting tmux's own report that it sent the bytes. pane must come
// from capturePaneJoined, not capturePane: a prompt long enough to wrap
// across screen rows is still one logical line to Claude Code's own input
// box, and only the joined capture reads it back as one.
func hasInputLine(pane, text string) bool {
	want := inputMarker
	if text != "" {
		want = inputMarker + " " + text
	}
	for _, line := range strings.Split(pane, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// capturePaneJoined is capturePane with wrapped screen rows joined back into
// the one logical line they really are (tmux's own capture-pane -J). The
// input box's own text is exactly this kind of line — Claude Code just keeps
// printing past the pane's width rather than wrapping words itself — so
// hasInputLine needs the join to still recognise a prompt long enough to
// wrap as the one line it types into, not several fragments.
func (h Harness) capturePaneJoined(target string) (string, error) {
	out, err := exec.Command("tmux", h.sessions().args("capture-pane", "-p", "-J", "-t", target)...).Output()
	if err != nil {
		return "", fmt.Errorf("capture the Session's pane: %w", err)
	}
	return string(out), nil
}

// located resolves pid's own pane and checks it shows an empty input box:
// the guard's shared precondition for every prompt-send action (§7.2),
// unified the same way guard.go's answer unifies Approve and Deny. action
// names what the empty box is needed for, in the word the caller's own
// mismatch error reads with.
func (h Harness) located(pid int, action string) (string, error) {
	target, err := h.locate(pid)
	if err != nil {
		return "", err
	}
	pane, err := h.capturePaneJoined(target)
	if err != nil {
		return "", err
	}
	if !emptyInputLine(pane) {
		return "", fmt.Errorf("pane %s does not show an empty input box to %s", target, action)
	}
	return target, nil
}

// Send delivers text into the Session running as pid's own input box: the
// guard's prompt-send (§7.2, §7.3). It is what "p" then Enter does on an
// Idle or Ready Session, and what the same key does on a Working one — the
// box reads empty the same way in all three, so it is Claude Code's own
// queuing that tells the two apart, not anything the guard has to know
// about.
func (h Harness) Send(pid int, text string) error {
	target, err := h.located(pid, "send into")
	if err != nil {
		return err
	}
	return h.sendInto(target, text)
}

// sendInto is Send's own work once a pane already known to show an empty
// input box has been resolved — shared with InterruptAndSend, which resolves
// and clears that box itself rather than paying locate's process-table walk
// a second time over.
//
// Its own postcondition — the box goes back to empty — only holds for a
// prompt Claude Code keeps running to act on: Working reads with the same
// empty box Idle and Ready do (prompt.go's own note), so a submit that
// started a turn looks identical to one sitting there unsent. End does not
// share this: /exit's own postcondition is the marker leaving for good
// (stop.go's exited), not reappearing, so it pastes and submits through
// pasted rather than calling sendInto.
func (h Harness) sendInto(target, text string) error {
	if err := h.pasted(target, text); err != nil {
		return err
	}
	if !h.settledOn(target, "") {
		return fmt.Errorf("pane %s still shows the prompt after Enter was sent", target)
	}
	return nil
}

// pasted lands text in target's own input box and submits it, verifying only
// that the paste itself landed before Enter goes out — the half of a guarded
// send every submit shares, whatever its own postcondition for "it worked"
// turns out to be once Enter has gone out (sendInto's own empty box, or
// End's exited).
func (h Harness) pasted(target, text string) error {
	// A named buffer of its own, deleted the moment paste-buffer is done with
	// it: tmux's unnamed set-buffer/paste-buffer pair is one global slot per
	// server, and every Session's guarded send shares the same server
	// (topology.Harness.sessions). Two sends racing on the unnamed buffer can
	// paste one Session's text into another's pane; a buffer named for this
	// one call, referenced by name on both ends, cannot collide with one
	// named for any other.
	buffer := fmt.Sprintf("ganymede-prompt-%d-%d", os.Getpid(), bufferSeq.Add(1))
	if err := h.sessions().run("set-buffer", "-b", buffer, "--", text); err != nil {
		return fmt.Errorf("buffer the prompt: %w", err)
	}
	if err := h.sessions().run("paste-buffer", "-d", "-p", "-b", buffer, "-t", target); err != nil {
		return fmt.Errorf("paste the prompt into the Session's pane: %w", err)
	}
	if !h.settledOn(target, text) {
		return fmt.Errorf("pane %s does not show the prompt that was sent", target)
	}

	if err := h.sessions().run("send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("submit the prompt: %w", err)
	}
	return nil
}

// InterruptAndSend interrupts the Session running as pid's current turn and
// then delivers text into its input box: the guard's Ctrl+Enter row on a
// Working Session (§7.2, §7.3, "alt+⏎" here — see prompt.go's own note on
// why Ctrl+Enter is not the literal key bound). Escape only ever interrupts
// a Working turn cleanly when there is no dialog it would close instead, so
// the same empty-box check the plain send starts with is what the interrupt
// gates on too — and an interrupted turn hands the pane back with its own
// prompt sitting in the box, verified against the real CLI, which Ctrl+U
// clears before the send itself ever reaches that pane.
func (h Harness) InterruptAndSend(pid int, text string) error {
	target, err := h.escaped(pid)
	if err != nil {
		return err
	}

	if err := h.sessions().run("send-keys", "-t", target, "C-u"); err != nil {
		return fmt.Errorf("clear the interrupted prompt: %w", err)
	}
	if !h.settledOn(target, "") {
		return fmt.Errorf("pane %s still shows the interrupted prompt after it was cleared", target)
	}
	return h.sendInto(target, text)
}

// escaped sends Escape into the Session running as pid's own input box and
// verifies the turn it interrupted actually stopped — the guarded interrupt
// every caller shares: Interrupt's own whole job (stop.go), and
// InterruptAndSend's own interrupt half before it clears the box and sends
// on. It returns the resolved pane so a caller with more to do after the
// interrupt need not pay locate's process-table walk a second time over.
func (h Harness) escaped(pid int) (string, error) {
	target, err := h.located(pid, "interrupt")
	if err != nil {
		return "", err
	}
	if err := h.sessions().run("send-keys", "-t", target, "Escape"); err != nil {
		return "", fmt.Errorf("interrupt the Session's turn: %w", err)
	}
	if !h.interrupted(target) {
		return "", fmt.Errorf("pane %s still shows an empty input box after Escape was sent", target)
	}
	return target, nil
}

// interrupted polls the pane until it no longer shows the empty input box
// Escape was sent into, or redrawBudget runs out. An interrupted turn hands
// the pane back with the prompt it interrupted still sitting in the box,
// never an empty one — so "no longer empty" is what confirms Escape actually
// interrupted the turn, rather than tmux merely reporting that the key went
// out.
func (h Harness) interrupted(target string) bool {
	deadline := time.Now().Add(redrawBudget)
	for {
		pane, err := h.capturePaneJoined(target)
		if err == nil && !emptyInputLine(pane) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(redrawPoll)
	}
}

// settledOn polls the pane until it shows text in the input box, or
// redrawBudget runs out — the same budget and cadence the guard's approve
// and deny already give a pane to redraw (guard.go). Polling rather than a
// single check is what a live-redrawing box needs: the box the paste just
// landed in, the box a submit just cleared, and the box Ctrl+U just cleared
// all take a moment to actually redraw.
func (h Harness) settledOn(target, text string) bool {
	deadline := time.Now().Add(redrawBudget)
	for {
		pane, err := h.capturePaneJoined(target)
		if err == nil && hasInputLine(pane, text) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(redrawPoll)
	}
}
