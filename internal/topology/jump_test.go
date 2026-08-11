package topology_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// paneCommand keeps a pane alive with a process nested inside it, the way a
// Session sits below the pane's shell. The trailing `true` stops the shell
// exec'ing straight into the command and collapsing the nesting.
const paneCommand = `sh -c 'sleep 300; true'; true`

// deepestChild follows the process tree down from pid to its leaf — where a
// Session's own process sits relative to the shell tmux started in the pane.
func deepestChild(t *testing.T, pid int) int {
	t.Helper()
	leaf := pid
	// The pane's processes appear a moment after tmux reports the pane.
	settles(func() bool {
		for {
			out, err := exec.Command("pgrep", "-P", strconv.Itoa(leaf)).Output()
			if err != nil {
				break
			}
			child, err := strconv.Atoi(strings.Fields(string(out))[0])
			if err != nil {
				break
			}
			leaf = child
		}
		return leaf != pid
	})
	if leaf == pid {
		t.Fatalf("no process below the pane's own %d to stand in for a Session", pid)
	}
	return leaf
}

// panePID is the pid tmux started in a pane.
func panePID(t *testing.T, socket, target string) int {
	t.Helper()
	pid, err := strconv.Atoi(tmuxOn(t, socket, "display-message", "-p", "-t", target, "#{pane_pid}"))
	if err != nil {
		t.Fatalf("read the pane's pid: %v", err)
	}
	return pid
}

// panePIDInSession is the pid tmux started in the one pane of a named session.
func panePIDInSession(t *testing.T, socket, session string) int {
	t.Helper()
	for _, line := range strings.Split(tmuxOn(t, socket, "list-panes", "-a",
		"-F", "#{pane_pid}\t#{session_name}"), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 || fields[1] != session {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("read the pane's pid: %v", err)
		}
		return pid
	}
	t.Fatalf("no pane in the %q session", session)
	return 0
}

// workingClientShows reports which session and window the working client — the
// client running in the dock's right-hand pane — currently has on show. Both
// come back empty while that client is still attaching.
func workingClientShows(t *testing.T, h topology.Harness) (session, window string) {
	t.Helper()
	tty := tmuxOn(t, h.DockSocket, "display-message", "-p", "-t", "=dock:0.1", "#{pane_tty}")
	out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-c", tty,
		"#{session_name}\t#{window_index}").Output()
	if err != nil {
		return "", ""
	}
	fields := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(fields) != 2 {
		return "", ""
	}
	return fields[0], fields[1]
}

// jumpable brings the harness up with a window in view and a second repo
// Session to jump to, and returns the harness.
func jumpable(t *testing.T) topology.Harness {
	t.Helper()
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	_ = attachEmulator(t, h, 160, 45)
	if !settles(func() bool {
		session, _ := workingClientShows(t, h)
		return session == "service-ai-assistant"
	}) {
		t.Fatal("the working client never attached")
	}
	return h
}

// The tracer bullet's last step: pressing Enter on a Session's row has to put
// that Session in front of you, in the window it is actually running in.
func TestJumpPointsTheWorkingClientAtTheSessionsWindow(t *testing.T) {
	h := jumpable(t)
	// A second repo, with the Session in a window of its own the way a
	// Worktree session sits beside the main root.
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "service-billing", "-n", "main", paneCommand)
	tmuxOn(t, h.Socket, "new-window", "-d", "-t", "=service-billing", "-n", "FIRE-2841-paging", paneCommand)
	worktree := "=service-billing:FIRE-2841-paging"
	// Which index that window landed on is tmux's business, not the test's.
	wantWindow := tmuxOn(t, h.Socket, "display-message", "-p", "-t", worktree, "#{window_index}")
	session := deepestChild(t, panePID(t, h.Socket, worktree))

	if err := h.Jump(session); err != nil {
		t.Fatalf("Jump(%d): %v", session, err)
	}

	gotSession, gotWindow := workingClientShows(t, h)
	if gotSession != "service-billing" || gotWindow != wantWindow {
		t.Errorf("the working client shows %s window %s, want service-billing window %s",
			gotSession, gotWindow, wantWindow)
	}
}

// The Dashboard shows every Session the registry knows about, including ones
// in tmux sessions the harness never named. tmux splits a target on ":" and
// "." before the "=" exact-match prefix is considered, so a name carrying
// either is one the harness must not build a target out of.
func TestJumpReachesASessionWhoseTmuxNameTmuxCannotParse(t *testing.T) {
	h := jumpable(t)
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "api:v2", "-n", "main", paneCommand)
	// Found by listing rather than by target: the whole point of the name is
	// that no target can carry it.
	session := deepestChild(t, panePIDInSession(t, h.Socket, "api:v2"))

	if err := h.Jump(session); err != nil {
		t.Fatalf("Jump(%d): %v", session, err)
	}

	if got, _ := workingClientShows(t, h); got != "api:v2" {
		t.Errorf("the working client shows %q, want the Session's own tmux session %q", got, "api:v2")
	}
}

// The Dashboard keeps running in its own client: jumping steers the working
// client and nothing else.
func TestJumpLeavesTheDashboardWhereItIs(t *testing.T) {
	h := jumpable(t)
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "service-billing", "-n", "main", paneCommand)
	session := deepestChild(t, panePID(t, h.Socket, "=service-billing:main"))

	if err := h.Jump(session); err != nil {
		t.Fatalf("Jump(%d): %v", session, err)
	}

	clients := tmuxOn(t, h.Socket, "list-clients", "-F", "#{client_session}")
	if !strings.Contains(clients, topology.DashboardSession) {
		t.Errorf("the Dashboard client is showing %q, want it still on the Dashboard", clients)
	}
}

// A Session whose process the harness cannot place in any pane — started
// outside tmux, or already gone — is a jump that says so rather than steering
// the working client somewhere arbitrary.
func TestJumpToAProcessInNoPaneSaysSo(t *testing.T) {
	h := jumpable(t)

	// Our own process: alive, and running in no pane of this tmux server.
	err := h.Jump(os.Getpid())
	if err == nil {
		t.Fatal("Jump to a process in no pane reported success")
	}
	var gone topology.GoneError
	if errors.As(err, &gone) {
		t.Errorf("a live process with no pane was reported Gone: %v", err)
	}
	if session, _ := workingClientShows(t, h); session != "service-ai-assistant" {
		t.Errorf("the working client moved to %q on a jump that could not be made", session)
	}
}

// deadPID hands back a pid that used to be a process and now is not — the
// stand-in for a Session whose Claude process has already ended.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for the throwaway process: %v", err)
	}
	return pid
}

// A pid that no longer names any process at all is the one case worth acting
// on rather than just reporting: the Session is not merely unplaceable, it is
// Gone, and the Dashboard needs to be able to tell the two apart.
func TestJumpToAGoneProcessSaysSo(t *testing.T) {
	h := jumpable(t)
	pid := deadPID(t)

	err := h.Jump(pid)
	if err == nil {
		t.Fatal("Jump to a gone process reported success")
	}
	var gone topology.GoneError
	if !errors.As(err, &gone) || gone.PID != pid {
		t.Errorf("Jump(%d) error = %v, want a GoneError naming %d", pid, err, pid)
	}
	if session, _ := workingClientShows(t, h); session != "service-ai-assistant" {
		t.Errorf("the working client moved to %q on a jump that could not be made", session)
	}
}

// Focus lands on a pane, and the harness has to work out which Sessions that
// was. tmux knows only the process it started there; a Session is that
// process's descendant, so nothing but a walk up the process tree connects the
// two.
func TestUnderNamesTheProcessesInsideAPane(t *testing.T) {
	// A pane's shell with a Session nested inside it. The trailing `true`
	// stops the shell exec'ing straight into the command, the way a real pane
	// keeps its shell.
	pane := exec.Command("sh", "-c", "sh -c 'sleep 30; true'; true")
	if err := pane.Start(); err != nil {
		t.Fatalf("start a stand-in pane: %v", err)
	}
	t.Cleanup(func() {
		_ = pane.Process.Kill()
		_ = pane.Wait()
	})
	session := deepestChild(t, pane.Process.Pid)

	// The test's own process is this pane's parent, not one of its Sessions.
	found, err := topology.Under(pane.Process.Pid, []int{session, os.Getpid()})
	if err != nil {
		t.Fatalf("Under: %v", err)
	}

	if len(found) != 1 || found[0] != session {
		t.Errorf("the pane holds %v, want only the Session nested inside it (%d)", found, session)
	}
}

// Focus landing on a pane with no Session in it — the Dashboard's own, a shell
// you opened — is not something to report about anybody else.
func TestUnderNamesNothingWhenAPaneHoldsNoneOfThem(t *testing.T) {
	found, err := topology.Under(1, nil)
	if err != nil {
		t.Fatalf("Under: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("found %v inside a pane it was given no processes for", found)
	}
}

// idlePane starts a tmux session standing in for a Session's own pane, with a
// process nested inside it the way a Session sits below the pane's shell, and
// no mode held over it. It answers with the nested pid — the one the registry
// would name — rather than the pane's own, so what is asked about here is what
// gets asked about in earnest.
func idlePane(t *testing.T, h topology.Harness, name string) int {
	t.Helper()
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", name, paneCommand)
	return deepestChild(t, panePIDInSession(t, h.Socket, name))
}

// A pane in copy-mode is showing a held view: the program underneath goes on
// writing to its screen, and none of it reaches the client. That is the thing
// the rail has to be able to say, and pane_in_mode is tmux's answer to it.
func TestFrozenReportsOnlyThePaneHoldingAMode(t *testing.T) {
	h := guardable(t)
	held := idlePane(t, h, "held")
	live := idlePane(t, h, "live")

	tmuxOn(t, h.Socket, "copy-mode", "-t", "held")

	frozen, err := h.Frozen([]int{held, live})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if !frozen[held] {
		t.Errorf("the pane in copy-mode is not reported frozen: %v", frozen)
	}
	if frozen[live] {
		t.Errorf("a pane showing its live view is reported frozen: %v", frozen)
	}
}

// Leaving the mode is as much of an answer as entering it: a mark that only
// ever went on would be worse than no mark at all.
func TestFrozenClearsWhenTheModeIsCancelled(t *testing.T) {
	h := guardable(t)
	pid := idlePane(t, h, "held")
	tmuxOn(t, h.Socket, "copy-mode", "-t", "held")
	tmuxOn(t, h.Socket, "send-keys", "-X", "-t", "held", "cancel")

	frozen, err := h.Frozen([]int{pid})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if frozen[pid] {
		t.Error("a pane whose mode was cancelled is still reported frozen")
	}
}

// A Session in no pane at all — started outside tmux, or gone since the
// registry was read — is left out rather than answered false. The two are
// different answers, and only one of them is true.
func TestFrozenLeavesOutAProcessInNoPane(t *testing.T) {
	h := guardable(t)
	// A pane of some sort, so the server is actually up: what is being
	// asked here is what a live server says about a process none of its
	// panes is running, not what a server that is not there says at all.
	idlePane(t, h, "live")

	frozen, err := h.Frozen([]int{os.Getpid()})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if _, answered := frozen[os.Getpid()]; answered {
		t.Errorf("a process in no pane was given an answer: %v", frozen)
	}
}
