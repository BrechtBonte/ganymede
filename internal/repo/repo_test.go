package repo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// git runs a git command in dir, failing the test if it does not work.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	// The identity is supplied here so the test does not depend on whoever is
	// running it having configured one.
	full := append([]string{"-C", dir, "-c", "user.email=test@example.com", "-c", "user.name=Test"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mainRoot makes dir a repository with a commit in it — enough for a worktree
// to be spawned from — and returns the one name a root goes by.
func mainRoot(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	return resolved(t, dir)
}

// resolved is the path with its symlinks followed: on macOS a temporary
// directory is reached through /var, which is a link to /private/var, and both
// git and the harness name a root by where it really is.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	return real
}

// Sessions group under their repo, wherever inside it they are working.
func TestRootIsTheRepositoryContainingTheDirectory(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	sub := filepath.Join(root, "src", "handlers")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := repo.Root(sub); got != root {
		t.Errorf("Root(%q) = %q, want the Main root %q", sub, got, root)
	}
}

// A Worktree session belongs to the repo it was spawned from, so its row sits
// under that repo's header rather than starting a repo of its own.
func TestWorktreeSessionGroupsUnderTheRepositoryItCameFrom(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	// Where `claude --worktree` puts one.
	worktree := filepath.Join(root, ".claude", "worktrees", "FIRE-2841-paging")
	git(t, root, "worktree", "add", "-q", "-b", "FIRE-2841-paging", worktree)

	if got := repo.Root(worktree); got != root {
		t.Errorf("Root(%q) = %q, want the Main root %q", worktree, got, root)
	}
}

// Sessions outside any repository still get a row, grouped under the directory
// they are working in — the registry's cwd is ground truth.
func TestDirectoryOutsideAnyRepositoryIsItsOwnRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got, want := repo.Root(dir), resolved(t, dir); got != want {
		t.Errorf("Root(%q) = %q, want %q", dir, got, want)
	}
}

// A submodule's git directory lives inside its superproject's, so the
// directory above it is no checkout at all: a Session working in a submodule
// belongs under the submodule, not under a repo called "modules".
func TestSubmoduleSessionGroupsUnderTheSubmodule(t *testing.T) {
	root := t.TempDir()
	library := mainRoot(t, filepath.Join(root, "shared-library"))
	super := mainRoot(t, filepath.Join(root, "service-ai-assistant"))
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "libs/shared-library")

	submodule := filepath.Join(super, "libs", "shared-library")

	if got := repo.Root(submodule); got != resolved(t, submodule) {
		t.Errorf("Root(%q) = %q, want the submodule checkout %q", submodule, got, resolved(t, submodule))
	}
}

// A Session's directory can go away underneath the harness — a worktree
// removed, a checkout deleted — between the registry being written and being
// read. That is a row to group somewhere sensible, not a crash.
func TestDirectoryThatIsNoLongerThereIsStillItsOwnRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "removed")

	if got := repo.Root(dir); got != dir {
		t.Errorf("Root(%q) = %q, want %q", dir, got, dir)
	}
}
