package topology_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// guardable is a Harness on a throwaway server, with no dock and no working
// client — the guard only ever touches the one pane it is pointed at, not
// the topology Jump needs to steer anything.
func guardable(t *testing.T) topology.Harness {
	t.Helper()
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	return testHarness(t, repo)
}

// dialogPane starts a tmux session standing in for a Session's own pane while
// its permission dialog is up. It prints the tool line a permission reason
// is phrased from, reads exactly the one key the guard is allowed to send,
// and logs that key's byte in hex to keylog — the only way a test can tell a
// key that never went out from one the pane simply ignored. resolve says
// whether the dialog clears once a key arrives, the way a real one does when
// the guard's key actually answers it; left false, the pane is unchanged by
// whatever it read, the way a dialog that never saw the key would be.
func dialogPane(t *testing.T, h topology.Harness, resolve bool) (pid int, keylog string) {
	t.Helper()
	keylog = filepath.Join(t.TempDir(), "key")
	script := filepath.Join(t.TempDir(), "dialog.sh")
	clears := "no"
	if resolve {
		clears = "yes"
	}
	body := "#!/bin/bash\n" +
		// A real dialog is a full-screen TUI in raw mode, not a shell prompt: the
		// tty never echoes what is sent to it, and the pane changes only when the
		// program itself reacts. Without this, the pty's own canonical-mode echo
		// would put the sent key on screen whether or not the script below did
		// anything about it, and the "the pane never changed" case could never be
		// told apart from the one where it actually resolved.
		"stty -echo\n" +
		"printf 'Bash(echo hi)\\nDo you want to proceed?\\n'\n" +
		"read -n 1 key\n" +
		"printf '%s' \"$key\" | od -An -tx1 | tr -d ' \\n' > " + shellQuoted(keylog) + "\n" +
		"if [ \"$1\" = yes ]; then\n" +
		"  clear\n" +
		"  printf 'done\\n'\n" +
		"fi\n" +
		"sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the dialog script: %v", err)
	}

	session := "dialog-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script, clears)
	return panePIDInSession(t, h.Socket, session), keylog
}

// shellQuoted is dialogPane's own copy of the quoting harness.go keeps
// private: a path under t.TempDir() never carries a quote, but the script it
// is written into is still a shell command line.
func shellQuoted(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// readKeylog is the hex byte dialogPane logged, once there is one — settling
// rather than reading at once, since the pane's own read-and-log takes a
// moment to land after tmux delivers the keystroke.
func readKeylog(t *testing.T, path string) string {
	t.Helper()
	var body []byte
	if !settles(func() bool {
		var err error
		body, err = os.ReadFile(path)
		return err == nil && len(body) > 0
	}) {
		t.Fatalf("no key was ever logged to %s", path)
	}
	return strings.TrimSpace(string(body))
}

// Y is the dialog's own default row (§7.3): the guard's whole point is that
// it can be sent without you ever leaving the Dashboard.
func TestApproveSendsYAndTheDialogClears(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Approve(pid, "permission: Bash"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if got := readKeylog(t, keylog); got != "59" {
		t.Errorf("logged key %q, want Y (0x59)", got)
	}
}

// Esc, not N — the resolution's own choice (§7.3), since Esc closes any
// dialog the same way whatever N happens to mean inside it.
func TestDenySendsEscape(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Deny(pid, "permission: Bash"); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	if got := readKeylog(t, keylog); got != "1b" {
		t.Errorf("logged key %q, want Escape (0x1b)", got)
	}
}

// The guard's first real check: a pane that is not showing the tool the row
// says it is Blocked on is a pane something else has already got to, and
// nothing goes out to it at all.
func TestApproveRefusesAPaneShowingTheWrongTool(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Approve(pid, "permission: Deploy"); err == nil {
		t.Fatal("Approve reported success against a pane showing a different tool")
	}
	if _, err := os.Stat(keylog); err == nil {
		t.Error("Approve sent a key even though the pane did not show the expected dialog")
	}
}

// Sending is not the same as succeeding: a key that reached a pane whose
// dialog is still there afterwards is one the guard could not actually
// resolve, and it is reported rather than trusted.
func TestApproveReportsWhenTheDialogIsStillThereAfterTheSend(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, false)

	if err := h.Approve(pid, "permission: Bash"); err == nil {
		t.Fatal("Approve reported success even though the dialog never went away")
	}
	if got := readKeylog(t, keylog); got != "59" {
		t.Errorf("logged key %q, want the Y that went out before the mismatch was caught", got)
	}
}

// A Blocked reason with no tool name in it — an elicitation dialog, a bare
// notification — is the honest limit of what the guard can check without
// ever having seen that dialog rendered: it still verifies the reason's own
// text against the pane, rather than requiring a tool name it was never
// given.
func TestApproveWithNoToolNameMatchesTheReasonText(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Approve(pid, "Do you want to proceed?"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got := readKeylog(t, keylog); got != "59" {
		t.Errorf("logged key %q, want Y", got)
	}
}

// A reason with no tool name that never mentions anything the pane actually
// shows is refused rather than sent on faith — a non-empty pane is not by
// itself the dialog the row said it was waiting on.
func TestApproveWithNoToolNameRefusesAnUnrelatedPane(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	err := h.Approve(pid, "elicitation_dialog")
	if err == nil {
		t.Fatal("Approve reported success against a pane that never mentioned the reason")
	}
	if _, statErr := os.Stat(keylog); statErr == nil {
		t.Error("Approve sent a key even though the pane never showed the reason")
	}
}

// The same reason with nothing at all on the pane to check it against — the
// registry's own account of a Session it could never time — is refused
// rather than sent on faith.
func TestApproveWithNoToolNameRefusesAnEmptyPane(t *testing.T) {
	h := guardable(t)
	session := "empty-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "sleep", "300")
	pid := panePIDInSession(t, h.Socket, session)

	if err := h.Approve(pid, "elicitation_dialog"); err == nil {
		t.Fatal("Approve reported success against an empty pane")
	}
}

// hooks.permissionReason truncates a tool invocation over 120 characters and
// marks the cut with an ellipsis — text the pane, which always renders the
// real command in full, never contains. The guard has to match what survived
// the cut as a prefix, not the marker that something was cut from it.
func TestApproveMatchesATruncatedToolNameAsAPrefix(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)

	if err := h.Approve(pid, "permission: Bash(echo hi)…"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got := readKeylog(t, keylog); got != "59" {
		t.Errorf("logged key %q, want Y", got)
	}
}

// A Session the harness cannot place in any pane is a jump that says so
// (locate, shared with Jump) rather than a guard that guesses.
func TestApproveToAProcessInNoPaneSaysSo(t *testing.T) {
	h := guardable(t)

	if err := h.Approve(os.Getpid(), "permission: Bash"); err == nil {
		t.Fatal("Approve to a process in no pane reported success")
	}
}

// A pane holding a mode hands every keystroke to the mode's own key table, so
// a guarded send lands nowhere near the dialog. capture-pane cannot say so —
// it returns the live screen the mode is holding a view over, which still
// shows the dialog — so the pane passes the content check and the key goes
// out anyway, to be reported half a second later as a dialog that did not
// move. True, and useless: the dialog never got the key. The guard asks
// first instead.
func TestApproveRefusesAFrozenPaneAndSendsNothing(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)
	pane := "dialog-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "copy-mode", "-t", pane)

	err := h.Approve(pid, "permission: Bash")
	if err == nil {
		t.Fatal("Approve reported success against a frozen pane")
	}
	// This is what says nothing went out, and the keylog below cannot: a key
	// sent into a mode is swallowed by the mode rather than logged, so an
	// absent keylog reads the same either way. Only the check that happens
	// before the send produces this message — a guard that sent the key
	// fails later and differently, with "still shows the dialog after Y was
	// sent", which is what this test saw before the check existed.
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("Approve refused with %q, which does not say the pane is frozen", err)
	}
	if _, err := os.Stat(keylog); err == nil {
		t.Error("Approve sent a key into a frozen pane")
	}
	// The harness never clears the mode: a pane scrolled back on purpose is
	// in the same state as one frozen by accident, and it cannot tell them
	// apart. Refusing must leave the view exactly where you put it.
	if got := tmuxOn(t, h.Socket, "display-message", "-p", "-t", pane, "#{pane_in_mode}"); got != "1" {
		t.Errorf("pane_in_mode = %q after a refusal, want 1 — the guard disturbed a held view", got)
	}
}
