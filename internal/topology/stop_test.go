package topology_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// interruptOnlyPane starts a tmux session standing in for a Working
// Session's own pane: the same empty input box promptPane and interruptPane
// both start from, Escape logged to keylog and restoring the interrupted
// turn's own prompt in the box (verified against the real CLI, the same way
// interruptPane's own note documents) — and nothing read after that, since a
// bare interrupt sends no more than the one key.
func interruptOnlyPane(t *testing.T, h topology.Harness) (pid int, keylog string) {
	t.Helper()
	keylog = filepath.Join(t.TempDir(), "key")
	script := filepath.Join(t.TempDir(), "interrupt-only.sh")
	body := "#!/bin/bash\n" +
		"stty -echo\n" +
		"clear; printf '❯ \\n'\n" +
		"read -n 1 esc\n" +
		"printf '%s' \"$esc\" | od -An -tx1 | tr -d ' \\n' > " + shellQuoted(keylog) + "\n" +
		"clear; printf '❯ the interrupted prompt\\n'\n" +
		"sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the interrupt-only script: %v", err)
	}

	session := "interrupt-only-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script)
	return panePIDInSession(t, h.Socket, session), keylog
}

// A bare interrupt is Escape and nothing else — no clear, no paste, no
// Enter — verified by the guard's own re-check that the turn actually
// stopped (§7.3's "x" row).
func TestInterruptSendsEscapeAndVerifiesTheTurnStopped(t *testing.T) {
	h := promptable(t)
	pid, keylog := interruptOnlyPane(t, h)

	if err := h.Interrupt(pid); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	if got := readKeylog(t, keylog); got != "1b" {
		t.Errorf("logged key %q, want Escape (0x1b)", got)
	}
}

// The guard's own precondition: a pane not showing an empty input box —
// because a dialog is up, say — is refused rather than sent Escape it might
// close instead of a turn it might interrupt. Only fires when no dialog is
// visible in capture-pane (§7.3).
func TestInterruptRefusesAPaneShowingADialog(t *testing.T) {
	h := promptable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Interrupt(pid); err == nil {
		t.Fatal("Interrupt reported success against a pane showing a dialog")
	}
	if _, err := os.Stat(keylog); err == nil {
		t.Error("Interrupt sent a key even though the pane showed a dialog, not an empty input box")
	}
}

// Escape reaching a pane is not the same as it having interrupted anything:
// a box still empty afterwards is reported rather than trusted.
func TestInterruptReportsAnUnresponsiveEscape(t *testing.T) {
	h := promptable(t)
	script := filepath.Join(t.TempDir(), "unresponsive.sh")
	body := "#!/bin/bash\nstty -echo\nclear; printf '❯ \\n'\nsleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the script: %v", err)
	}
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "unresponsive-"+strings.ReplaceAll(t.Name(), "/", "-"), "bash", script)
	pid := panePIDInSession(t, h.Socket, "unresponsive-"+strings.ReplaceAll(t.Name(), "/", "-"))

	if err := h.Interrupt(pid); err == nil {
		t.Fatal("Interrupt reported success even though the box never left empty")
	}
}

// A Session the harness cannot place in any pane is a jump that says so
// (locate, shared with Jump, Approve and Send) rather than a guard that
// guesses.
func TestInterruptToAProcessInNoPaneSaysSo(t *testing.T) {
	h := promptable(t)

	if err := h.Interrupt(os.Getpid()); err == nil {
		t.Fatal("Interrupt to a process in no pane reported success")
	}
}

// exitPane starts a tmux session standing in for a Session's own pane during
// a real /exit: the same empty box every prompt-send starts from, and, once
// Enter lands after the text, the input marker gone from the pane for good
// rather than merely cleared — the way Claude Code actually quitting hands
// the pane back to whatever shell hosted it (a Main root's own Session,
// where claude runs as a command inside an already-running shell), never the
// ❯ box redrawn empty the way every other submit leaves it. resolves says
// whether Enter actually clears the marker for good; left false, the box is
// left showing the unsent text, the way a submit that never took would.
func exitPane(t *testing.T, h topology.Harness, resolves bool) (pid int, textlog string) {
	t.Helper()
	textlog = filepath.Join(t.TempDir(), "text")
	script := filepath.Join(t.TempDir(), "exit.sh")
	clears := "no"
	if resolves {
		clears = "yes"
	}
	body := "#!/bin/bash\n" +
		"stty -echo\n" +
		"clear; printf '❯ \\n'\n" +
		"IFS= read -r -n 5 chunk\n" +
		"printf '%s' \"$chunk\" > " + shellQuoted(textlog) + "\n" +
		"clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"read -n 1 enterkey\n" +
		"if [ \"$1\" = yes ]; then\n" +
		"  clear; printf '$ \\n'\n" +
		"else\n" +
		"  clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"fi\n" +
		"sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the exit script: %v", err)
	}

	session := "exit-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script, clears)
	return panePIDInSession(t, h.Socket, session), textlog
}

// End is /exit pasted and submitted at the prompt — the "q" row (§7.3), once
// the Dashboard's own confirmation has already been answered. Its own
// success reading is the ❯ marker leaving for good, not reappearing the way
// Send's own postcondition expects: a Session that actually quit never hands
// that box back.
func TestEndPastesExitAndPressesEnter(t *testing.T) {
	h := promptable(t)
	pid, textlog := exitPane(t, h, true)

	if err := h.End(pid); err != nil {
		t.Fatalf("End: %v", err)
	}

	if got := readKeylog(t, textlog); got != "/exit" {
		t.Errorf("pasted %q, want %q", got, "/exit")
	}
}

// A Worktree session's own pane — launched with claude as the pane's own
// command (WorktreeCommand) — closes the moment that process quits, since
// tmux's default remain-on-exit takes the whole pane down with nothing else
// left running in it. End reads that as the session having ended, not as a
// mismatch it could not verify.
func TestEndReadsAClosedPaneAsAGracefulExit(t *testing.T) {
	h := promptable(t)
	textlog := filepath.Join(t.TempDir(), "text")
	script := filepath.Join(t.TempDir(), "quit.sh")
	body := "#!/bin/bash\n" +
		"stty -echo\n" +
		"clear; printf '❯ \\n'\n" +
		"IFS= read -r -n 5 chunk\n" +
		"printf '%s' \"$chunk\" > " + shellQuoted(textlog) + "\n" +
		"clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"read -n 1 enterkey\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the quit script: %v", err)
	}
	session := "quit-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script)
	pid := panePIDInSession(t, h.Socket, session)

	if err := h.End(pid); err != nil {
		t.Fatalf("End: %v", err)
	}
	if got := readKeylog(t, textlog); got != "/exit" {
		t.Errorf("pasted %q, want %q", got, "/exit")
	}
}

// /exit reaching a pane is not the same as Claude Code having actually
// quit: a pane still showing its own ❯ prompt afterwards is reported rather
// than trusted.
func TestEndReportsWhenClaudeCodeNeverActuallyQuit(t *testing.T) {
	h := promptable(t)
	pid, textlog := exitPane(t, h, false)

	if err := h.End(pid); err == nil {
		t.Fatal("End reported success even though the pane still showed Claude Code's own prompt")
	}
	if got := readKeylog(t, textlog); got != "/exit" {
		t.Errorf("pasted %q, want the text that went out before the mismatch was caught", got)
	}
}

// The same guard's precondition as every other prompt-send: a pane not
// showing an empty input box gets nothing pasted into it.
func TestEndRefusesAPaneWithoutAnEmptyInputBox(t *testing.T) {
	h := promptable(t)
	session := "busy-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "sleep", "300")
	pid := panePIDInSession(t, h.Socket, session)

	if err := h.End(pid); err == nil {
		t.Fatal("End reported success against a pane with no empty input box")
	}
}
