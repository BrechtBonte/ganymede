package topology_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// promptable is a Harness on a throwaway server, the same shape guardable
// uses: the guard only ever touches the one pane it is pointed at.
func promptable(t *testing.T) topology.Harness {
	t.Helper()
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	return testHarness(t, repo)
}

// promptPane starts a tmux session standing in for a Session sitting at its
// own input box: the empty prompt marker Claude Code draws there whether the
// turn is Idle, Ready or still Working (verified against the real CLI — the
// box reads identically in all three, only what is drawn above it differs),
// and — also verified against the real CLI — redraws live to show text as it
// lands, before Enter ever submits it. It reads exactly text's own length off
// the pane without waiting for a line terminator, the same way a live text
// box would already be showing it by the time Enter arrives, and logs what
// it read to textlog — the only way a test can tell text that actually
// landed from text that never went out. resolves says whether the box goes
// back to empty once Enter arrives, the way a real submit clears it; left
// false, the text is left sitting in the box, the way a submit that never
// took would. cols is the pane's own width — narrow enough, text wraps
// across screen rows the same way a long real prompt does. label tells apart
// more than one promptPane started within the same test.
func promptPane(t *testing.T, h topology.Harness, text string, resolves bool, cols int, label string) (pid int, textlog string) {
	t.Helper()
	textlog = filepath.Join(t.TempDir(), "text")
	script := filepath.Join(t.TempDir(), "prompt.sh")
	clears := "no"
	if resolves {
		clears = "yes"
	}
	n := strconv.Itoa(len([]rune(text)))
	body := "#!/bin/bash\n" +
		"stty -echo\n" +
		"clear; printf '❯ \\n'\n" +
		"IFS= read -r -n " + n + " chunk\n" +
		"printf '%s' \"$chunk\" > " + shellQuoted(textlog) + "\n" +
		"clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"read -n 1 enterkey\n" +
		"if [ \"$1\" = yes ]; then\n" +
		"  clear; printf '❯ \\n'\n" +
		"else\n" +
		"  clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"fi\n" +
		"sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the prompt script: %v", err)
	}

	session := "prompt-" + label + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-x", strconv.Itoa(cols), "-y", "24", "-s", session, "bash", script, clears)
	return panePIDInSession(t, h.Socket, session), textlog
}

// Sending is pasting text into an empty box and submitting it, exactly the
// way a real prompt goes in (§7.2, §7.3).
func TestSendPastesTheTextAndPressesEnter(t *testing.T) {
	h := promptable(t)
	pid, textlog := promptPane(t, h, "fix the paging bug", true, 80, "")

	if err := h.Send(pid, "fix the paging bug"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := readKeylog(t, textlog); got != "fix the paging bug" {
		t.Errorf("pasted %q, want %q", got, "fix the paging bug")
	}
}

// A prompt long enough to wrap across screen rows in a narrow pane is still
// one logical line to Claude Code's own input box — the guard's own
// verification has to read it back as one, not as fragments split at the
// pane's own column width.
func TestSendVerifiesAPromptThatWrapsInANarrowPane(t *testing.T) {
	h := promptable(t)
	text := "fix the paging bug that keeps happening whenever a session goes idle"
	pid, textlog := promptPane(t, h, text, true, 20, "")

	if err := h.Send(pid, text); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := readKeylog(t, textlog); got != text {
		t.Errorf("pasted %q, want %q", got, text)
	}
}

// The guard's own check: a pane that is not showing an empty box — already
// carrying text, or something else altogether — is a box something else has
// already got its hands on, and nothing is pasted into it.
func TestSendRefusesAPaneWithoutAnEmptyInputBox(t *testing.T) {
	h := promptable(t)
	session := "busy-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "sleep", "300")
	pid := panePIDInSession(t, h.Socket, session)

	if err := h.Send(pid, "fix the paging bug"); err == nil {
		t.Fatal("Send reported success against a pane with no empty input box")
	}
}

// Pasting is not the same as submitting: text that is still sitting in the
// box after Enter was sent is a submit that never actually took, and it is
// reported rather than trusted.
func TestSendReportsWhenTheBoxStillShowsTheTextAfterEnter(t *testing.T) {
	h := promptable(t)
	pid, textlog := promptPane(t, h, "fix the paging bug", false, 80, "")

	if err := h.Send(pid, "fix the paging bug"); err == nil {
		t.Fatal("Send reported success even though the box never went back to empty")
	}
	if got := readKeylog(t, textlog); got != "fix the paging bug" {
		t.Errorf("pasted %q, want the text that went out before the mismatch was caught", got)
	}
}

// A Session the harness cannot place in any pane is a jump that says so
// (locate, shared with Jump and Approve) rather than a guard that guesses.
func TestSendToAProcessInNoPaneSaysSo(t *testing.T) {
	h := promptable(t)

	if err := h.Send(os.Getpid(), "fix the paging bug"); err == nil {
		t.Fatal("Send to a process in no pane reported success")
	}
}

// interruptPane starts a tmux session standing in for a Working Session's
// pane: an empty box (verified against the real CLI — Working reads the same
// as Idle and Ready there), Escape restoring the turn it interrupted as text
// sitting in the box (verified against the real CLI — an interrupted turn is
// handed back with its own prompt still there, never an empty box), and
// Ctrl+U clearing that back out (also verified against the real CLI, which
// names the key "Ctrl+Y to paste deleted text" in its own footer once it has
// been pressed) before the box behaves exactly like promptPane's own for
// whatever is typed and sent next.
func interruptPane(t *testing.T, h topology.Harness, text string, resolves bool) (pid int, textlog string) {
	t.Helper()
	textlog = filepath.Join(t.TempDir(), "text")
	script := filepath.Join(t.TempDir(), "interrupt.sh")
	clears := "no"
	if resolves {
		clears = "yes"
	}
	n := strconv.Itoa(len([]rune(text)))
	body := "#!/bin/bash\n" +
		"stty -echo\n" +
		"clear; printf '❯ \\n'\n" +
		"read -n 1 esc\n" +
		"clear; printf '❯ the interrupted prompt\\n'\n" +
		"read -n 1 ctrlu\n" +
		"clear; printf '❯ \\n'\n" +
		"IFS= read -r -n " + n + " chunk\n" +
		"printf '%s' \"$chunk\" > " + shellQuoted(textlog) + "\n" +
		"clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"read -n 1 enterkey\n" +
		"if [ \"$1\" = yes ]; then\n" +
		"  clear; printf '❯ \\n'\n" +
		"else\n" +
		"  clear; printf '❯ %s\\n' \"$chunk\"\n" +
		"fi\n" +
		"sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the interrupt script: %v", err)
	}

	session := "interrupt-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "bash", script, clears)
	return panePIDInSession(t, h.Socket, session), textlog
}

// Interrupting a Working turn and sending straight after is Escape, a clear
// of whatever Escape handed back, then the same guarded send (§7.3: the
// Ctrl+Enter row on a Working Session).
func TestInterruptAndSendInterruptsClearsAndSends(t *testing.T) {
	h := promptable(t)
	pid, textlog := interruptPane(t, h, "fix the paging bug", true)

	if err := h.InterruptAndSend(pid, "fix the paging bug"); err != nil {
		t.Fatalf("InterruptAndSend: %v", err)
	}

	if got := readKeylog(t, textlog); got != "fix the paging bug" {
		t.Errorf("pasted %q, want %q", got, "fix the paging bug")
	}
}

// The guard's own check on the interrupt half: a pane not showing an empty
// box before Escape is sent is one Escape would do something other than
// interrupt to — closing a dialog, say — so nothing is sent to it at all.
func TestInterruptAndSendRefusesAPaneWithoutAnEmptyInputBox(t *testing.T) {
	h := promptable(t)
	session := "busy-" + strings.ReplaceAll(t.Name(), "/", "-")
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", session, "sleep", "300")
	pid := panePIDInSession(t, h.Socket, session)

	if err := h.InterruptAndSend(pid, "fix the paging bug"); err == nil {
		t.Fatal("InterruptAndSend reported success against a pane with no empty input box")
	}
}

// Escape reaching a pane is not the same as it having interrupted anything:
// a box still empty afterwards — no turn was actually running there to
// interrupt — is reported rather than pasted into on the assumption that it
// worked.
func TestInterruptAndSendReportsAnUnresponsiveEscape(t *testing.T) {
	h := promptable(t)
	script := filepath.Join(t.TempDir(), "unresponsive.sh")
	body := "#!/bin/bash\nstty -echo\nclear; printf '❯ \\n'\nsleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write the script: %v", err)
	}
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "unresponsive", "bash", script)
	pid := panePIDInSession(t, h.Socket, "unresponsive")

	if err := h.InterruptAndSend(pid, "fix the paging bug"); err == nil {
		t.Fatal("InterruptAndSend reported success even though the box never left empty")
	}
}

// Two Sessions guarded-sending at once must never share so much as a byte:
// tmux's unnamed set-buffer/paste-buffer pair is one global slot per server,
// and every Session here shares the one server a Harness carries, so a send
// racing another's paste-buffer call the way the Dashboard's own goroutines
// can is exactly what a shared buffer would garble.
func TestConcurrentSendsNeverCrossPanes(t *testing.T) {
	h := promptable(t)
	pidA, textlogA := promptPane(t, h, "prompt for session A", true, 80, "a-")
	pidB, textlogB := promptPane(t, h, "prompt for session B", true, 80, "b-")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = h.Send(pidA, "prompt for session A")
	}()
	go func() {
		defer wg.Done()
		errs[1] = h.Send(pidB, "prompt for session B")
	}()
	wg.Wait()

	if errs[0] != nil {
		t.Errorf("Send to A: %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("Send to B: %v", errs[1])
	}
	if got := readKeylog(t, textlogA); got != "prompt for session A" {
		t.Errorf("session A's pane received %q, want its own prompt", got)
	}
	if got := readKeylog(t, textlogB); got != "prompt for session B" {
		t.Errorf("session B's pane received %q, want its own prompt", got)
	}
}
