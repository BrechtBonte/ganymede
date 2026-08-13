package topology_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// exitPane starts a tmux session standing in for a Session's own pane during
// a real /exit: the same empty box every prompt-send starts from, and, once
// Enter lands after the text, the input marker gone from the pane for good
// rather than merely cleared — the way Claude Code actually quitting hands
// the pane back to whatever shell hosted it (a Main root's own Session,
// where claude runs as a command inside an already-running shell), never the
// ❯ box redrawn empty the way every other submit leaves it. resolves says
// whether Enter actually clears the marker for good; left false, the box is
// left showing the unsent text, the way a submit that never took would.
// label tells apart more than one exitPane started within the same test.
func exitPane(t *testing.T, h topology.Harness, resolves bool, label string) (pid int, textlog string) {
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

	session := "exit-" + label + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script, clears)
	return panePIDInSession(t, h.Socket, session), textlog
}

// End is /exit pasted and submitted at the prompt, once the Dashboard's own
// Takeover confirmation has already been answered (claim.go). Its own
// success reading is the ❯ marker leaving for good, not reappearing.
func TestEndPastesExitAndPressesEnter(t *testing.T) {
	h := promptable(t)
	pid, textlog := exitPane(t, h, true, "")

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
	pid, textlog := exitPane(t, h, false, "")

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
