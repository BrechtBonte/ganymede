package topology_test

import (
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
