package topology_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// resolved is dir written the way the OS reports a process's real cwd — a
// temp dir under /var is really under /private/var on macOS, and
// pane_current_path answers with the resolved spelling.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	return real
}

// worktreeCalls records every name and prompt Spawn asked the Worktree
// command to build, standing in for claude actually running — a test has no
// business starting a real Claude Code session.
type worktreeCalls struct {
	names, prompts []string
}

// record is a Harness.Worktree that remembers what it was asked for and keeps
// the window alive with a plain shell, which is all a test needs to query it
// afterwards.
func (c *worktreeCalls) record(name, prompt string) []string {
	c.names = append(c.names, name)
	c.prompts = append(c.prompts, prompt)
	return []string{"sh", "-c", "sleep 300"}
}

// spawnable is a Harness on a throwaway server with no repo Session running
// yet, wired to a Worktree command that only remembers what it was asked for.
func spawnable(t *testing.T) (h topology.Harness, calls *worktreeCalls, repo string) {
	t.Helper()
	repo = initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h = testHarness(t, repo)
	calls = &worktreeCalls{}
	h.Worktree = calls.record
	return h, calls, repo
}

// windowNames is every window's name in a session.
func windowNames(t *testing.T, socket, session string) []string {
	t.Helper()
	out := tmuxOn(t, socket, "list-windows", "-t", "="+session, "-F", "#{window_name}")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// claude --worktree is adopted as-is (§6): the harness only has to ask for it
// named and permissioned right, never build it by hand.
func TestWorktreeCommandNamesTheSessionAfterTheWorktree(t *testing.T) {
	got := topology.WorktreeCommand("FIRE-2841-paging", "")
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--worktree FIRE-2841-paging") {
		t.Errorf("WorktreeCommand = %v, want --worktree FIRE-2841-paging", got)
	}
	if !strings.Contains(joined, "-n FIRE-2841-paging") {
		t.Errorf("WorktreeCommand = %v, want the Claude session named -n FIRE-2841-paging too", got)
	}
}

// Spawned Worktree sessions always start in auto permission mode — the
// worktree's isolation is what justifies it, whatever auto still gates
// simply surfaces as Blocked (§6, a standing decision).
func TestWorktreeCommandStartsInAutoPermissionMode(t *testing.T) {
	got := topology.WorktreeCommand("FIRE-2841-paging", "")
	if !strings.Contains(strings.Join(got, " "), "--permission-mode auto") {
		t.Errorf("WorktreeCommand = %v, want auto permission mode", got)
	}
}

// A first prompt is fire-and-forget: it rides along as the trailing argument,
// which is what starts the session Working immediately instead of leaving it
// to wait at its prompt.
func TestWorktreeCommandCarriesTheFirstPromptWhenThereIsOne(t *testing.T) {
	got := topology.WorktreeCommand("FIRE-2841-paging", "fix the pagination bug")
	if got[len(got)-1] != "fix the pagination bug" {
		t.Errorf("WorktreeCommand = %v, want the prompt as its last argument", got)
	}
}

// No prompt means no trailing argument at all — an empty one would be a
// prompt of nothing, not the absence of one.
func TestWorktreeCommandWithNoPromptAddsNoTrailingArgument(t *testing.T) {
	got := topology.WorktreeCommand("FIRE-2841-paging", "")
	for _, arg := range got {
		if arg == "" {
			t.Errorf("WorktreeCommand = %v, want no empty trailing argument", got)
		}
	}
}

// The whole point of the flow: w gets you a new window, named after the
// worktree, without you ever touching git or claude's flags yourself.
func TestSpawnCreatesANewWindowNamedAfterTheWorktree(t *testing.T) {
	h, calls, repo := spawnable(t)

	if err := h.Spawn(repo, "FIRE-2841-paging", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	session, err := topology.WorkingSessionName(repo)
	if err != nil {
		t.Fatalf("WorkingSessionName: %v", err)
	}
	if windows := windowNames(t, h.Socket, session); !slices.Contains(windows, "FIRE-2841-paging") {
		t.Errorf("windows = %v, want a window named after the worktree", windows)
	}
	if len(calls.names) != 1 || calls.names[0] != "FIRE-2841-paging" {
		t.Errorf("the Worktree command was asked to build %v, want [FIRE-2841-paging]", calls.names)
	}
}

// claude --worktree works out its own worktree from the directory it is
// started in, so the window has to open at the Main root — not at whatever
// worktree a previous Session left the harness sitting in.
func TestSpawnRunsTheWorktreeWindowAtTheMainRoot(t *testing.T) {
	h, _, repo := spawnable(t)

	if err := h.Spawn(repo, "FIRE-2841-paging", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	session, _ := topology.WorkingSessionName(repo)
	got := tmuxOn(t, h.Socket, "display-message", "-p", "-t", "="+session+":FIRE-2841-paging", "#{pane_current_path}")
	if want := resolved(t, repo); got != want {
		t.Errorf("the window's cwd is %q, want the Main root %q", got, want)
	}
}

// The first prompt typed into the dialog is what makes the spawn
// fire-and-forget, and it has to reach the actual command the window runs.
func TestSpawnPassesThePromptWhenThereIsOne(t *testing.T) {
	h, calls, repo := spawnable(t)

	if err := h.Spawn(repo, "FIRE-2841-paging", "fix the pagination bug"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if len(calls.prompts) != 1 || calls.prompts[0] != "fix the pagination bug" {
		t.Errorf("the Worktree command was asked for prompt %v, want the first prompt", calls.prompts)
	}
}

// Spawning into a repo from the picker has to work whether or not the repo
// has ever been opened — there may be no Session, and no tmux session at
// all, running in it yet.
func TestSpawnBringsUpTheReposSessionWhenNoneIsRunning(t *testing.T) {
	h, _, repo := spawnable(t)

	if err := h.Spawn(repo, "FIRE-2841-paging", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	session, _ := topology.WorkingSessionName(repo)
	if sessions := tmuxOn(t, h.Socket, "list-sessions", "-F", "#{session_name}"); !strings.Contains(sessions, session) {
		t.Errorf("no %q session; want Spawn to bring the repo's Session up", sessions)
	}
}

// A background session is not one that steals your eye the moment it starts:
// spawning must not switch the session's active window away from whatever
// you were already looking at in the Main root.
func TestSpawnLeavesTheMainRootsWindowInFront(t *testing.T) {
	h, _, repo := spawnable(t)

	if err := h.Spawn(repo, "FIRE-2841-paging", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	session, _ := topology.WorkingSessionName(repo)
	active := tmuxOn(t, h.Socket, "display-message", "-p", "-t", "="+session+":", "#{window_index}")
	if active != "0" {
		t.Errorf("the active window is %s, want the Main root's window 0 left in front", active)
	}
}
