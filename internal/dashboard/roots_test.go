package dashboard_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// git runs a git command in dir, failing the test if it does not work. The
// identity is supplied here so the test does not depend on whoever is running
// it having configured one.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.email=test@example.com", "-c", "user.name=Test"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mainRoot makes a repository with a commit in it — enough for a worktree to be
// spawned from — and returns the one name the root goes by. A root state is
// about which real checkout a Session has its hands on, so the Dashboard has to
// be shown real ones.
func mainRoot(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := exec.Command("mkdir", "-p", dir).Run(); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// worktree spawns one from root, the way `claude --worktree` does.
func worktree(t *testing.T, root, name string) string {
	t.Helper()
	at := filepath.Join(root, ".claude", "worktrees", name)
	git(t, root, "worktree", "add", "-q", "-b", name, at)
	return at
}

// headerOf is the drawn line for a repo's header row.
func headerOf(t *testing.T, model tea.Model, root string) string {
	t.Helper()
	line, ok := lineWith(tree(model), filepath.Base(root))
	if !ok {
		t.Fatalf("no header row for %q:\n%s", root, tree(model))
	}
	return line
}

// A Session working in the Main root holds it, whatever it is doing: an Idle
// agent still has the context it built up in that checkout. The rail says so on
// the repo's own row, because a PR is checked out in the Main root and whether
// anything is in the way should never be a keystroke away.
func TestRepoHeaderMarksAMainRootASessionIsWorkingIn(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	model := sidepanel(&jumps{}, live("ai-assistant-b3", root, session.Idle))

	if line := headerOf(t, model, root); !strings.Contains(line, repo.InUse.Glyph()) {
		t.Errorf("header = %q, want the mark of a root in use by an agent", line)
	}
}

// A mark in a column is a legend you have to have learned first. The box under
// the rail is where a row says what it had no room for, so a selected repo says
// its root's state in the words the vocabulary gives it.
func TestSelectedBoxNamesTheMainRootState(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	// The selection opens on the first row, which is the repo's header.
	model := sidepanel(&jumps{}, live("ai-assistant-b3", root, session.Idle))

	if box := detail(model); !strings.Contains(box, string(repo.InUse)) {
		t.Errorf("SELECTED = %q, want it to say the Main root is %q", box, repo.InUse)
	}
}

// A Worktree session is spawned so that the Main root is left alone, and the
// rail has to agree with that or the whole flow is pointless. The worktree
// lives inside the root's own directory, so a rail that went by paths would
// call every repo with a background session in use and never let a PR be
// checked out anywhere.
func TestRepoHeaderLeavesAMainRootWithOnlyAWorktreeSessionFree(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	model := sidepanel(&jumps{}, live("FIRE-2841-paging", worktree(t, root, "FIRE-2841-paging"), session.Idle))

	if line := headerOf(t, model, root); !strings.Contains(line, repo.Free.Glyph()) {
		t.Errorf("header = %q, want the mark of a free root", line)
	}
}
