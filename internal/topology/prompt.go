package topology

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// bufferSeq tells apart every named buffer pasted ever creates within this
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
// from capturePaneJoined: a prompt long enough to wrap across screen rows is
// still one logical line to Claude Code's own input box, and only the
// joined capture reads it back as one.
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

// capturePaneJoined is the pane's whole visible screen, with wrapped screen
// rows joined back into the one logical line they really are (tmux's own
// capture-pane -J). The input box's own text is exactly this kind of line —
// Claude Code just keeps printing past the pane's width rather than wrapping
// words itself — so hasInputLine needs the join to still recognise a prompt
// long enough to wrap as the one line it types into, not several fragments.
func (h Harness) capturePaneJoined(target string) (string, error) {
	out, err := exec.Command("tmux", h.sessions().args("capture-pane", "-p", "-J", "-t", target)...).Output()
	if err != nil {
		return "", fmt.Errorf("capture the Session's pane: %w", err)
	}
	return string(out), nil
}

// located resolves pid's own pane and checks it shows an empty input box:
// the precondition End needs before it pastes anything into it (§7.2).
// action names what the empty box is needed for, in the word the caller's
// own mismatch error reads with.
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

// pasted lands text in target's own input box and submits it, verifying only
// that the paste itself landed before Enter goes out. End is its only
// caller, and exited (stop.go) — not the box going back to empty — is its
// own postcondition for "it worked" once Enter has gone out.
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

// settledOn polls the pane until it shows text in the input box, or
// redrawBudget runs out — the same budget and cadence End's own exited
// check gives a pane to redraw (stop.go). Polling rather than a single
// check is what a live-redrawing box needs: the box pasted's paste just
// landed in takes a moment to actually redraw before it can be read back.
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
