package inventory_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/inventory"
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

// plainDir makes dir without making it a repository.
func plainDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return resolved(t, dir)
}

// resolved is the path with its symlinks followed: on macOS a temporary
// directory is reached through /var, which is a link to /private/var, and the
// harness names a root by where it really is — the same name repo.Root gives
// it, or the working set would show one repo twice.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	return real
}

// found runs the scan, failing the test if it could not be run at all.
func found(t *testing.T, scan inventory.Scan) []string {
	t.Helper()
	repos, err := scan.Repos()
	if err != nil {
		t.Fatalf("Repos(): %v", err)
	}
	return repos
}

func want(t *testing.T, got, wanted []string) {
	t.Helper()
	if !slices.Equal(got, wanted) {
		t.Errorf("Repos() = %v, want %v", got, wanted)
	}
}

// The picker offers the repos you keep under the scan roots, however deeply
// they are filed there — one per organisation is the shape ~/Projects takes.
func TestRepositoriesUnderAScanRootAreDiscovered(t *testing.T) {
	root := t.TempDir()
	loose := mainRoot(t, filepath.Join(root, "dotfiles"))
	filed := mainRoot(t, filepath.Join(root, "BrechtBonte", "ganymede"))

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{filed, loose})
}

// Discovery is bounded, because a scan root is somebody's whole projects
// directory and the vendored trees inside it go down forever.
func TestRepositoriesBelowTheDepthLimitAreNotDiscovered(t *testing.T) {
	root := t.TempDir()
	reachable := mainRoot(t, filepath.Join(root, "one", "two", "three"))
	mainRoot(t, filepath.Join(root, "one", "two", "three-deep", "four"))

	want(t, found(t, inventory.Scan{Roots: []string{root}, Depth: 3}), []string{reachable})
}

// A repository inside a repository — a vendored dependency, a fixture — is
// part of the repo it sits in, not a repo of its own. Stopping at the first
// checkout is also what keeps the scan cheap.
func TestScanStopsAtTheFirstRepositoryOnAPath(t *testing.T) {
	root := t.TempDir()
	outer := mainRoot(t, filepath.Join(root, "service-ai-assistant"))
	mainRoot(t, filepath.Join(outer, "vendor", "shared-library"))

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{outer})
}

// The working set is made of Main roots. A Worktree checkout is somewhere the
// repo it came from is already on the Dashboard for, so offering it in the
// picker would be offering the same repo twice.
func TestWorktreeCheckoutsAreNotDiscovered(t *testing.T) {
	root := t.TempDir()
	repo := mainRoot(t, filepath.Join(root, "ganymede"))
	// Worktrees are commonly kept beside their repo rather than inside it,
	// which puts them on the scan's path in their own right.
	worktree := filepath.Join(root, "worktrees", "FIRE-2841-paging")
	git(t, repo, "worktree", "add", "-q", "-b", "FIRE-2841-paging", worktree)

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{repo})
}

// A directory that is not a checkout is not a repo, however much else is
// filed under it.
func TestDirectoriesThatAreNotRepositoriesAreNotDiscovered(t *testing.T) {
	root := t.TempDir()
	plainDir(t, filepath.Join(root, "notes"))

	want(t, found(t, inventory.Scan{Roots: []string{root}}), nil)
}

// The scan roots are configuration, and configuration outlives the directory
// it names. A root that is not there is nothing to offer, not a Dashboard that
// cannot open its picker.
func TestScanRootThatIsNotThereIsEmptyRatherThanAnError(t *testing.T) {
	root := t.TempDir()
	repo := mainRoot(t, filepath.Join(root, "ganymede"))

	want(t, found(t, inventory.Scan{Roots: []string{root, filepath.Join(root, "gone")}}), []string{repo})
}

// A scan root that is there and cannot be read is worth reporting — the picker
// is quietly offering less than it should. It is not worth losing the repos
// under every other root over: one unreachable directory would turn into a
// picker with nothing in it.
func TestScanRootThatCannotBeReadKeepsWhatTheOthersFound(t *testing.T) {
	root := t.TempDir()
	reachable := mainRoot(t, filepath.Join(root, "ganymede"))
	closed := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(closed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Fatal(err)
	}
	// Put it back, or the directory cannot be cleaned up with the test.
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	repos, err := inventory.Scan{Roots: []string{root, closed}}.Repos()

	if err == nil {
		t.Error("Repos() said nothing about a scan root it could not read")
	}
	want(t, repos, []string{reachable})
}

// Two scan roots reaching the same repository is one repository: the picker
// would otherwise offer a choice with no difference in it.
func TestRepositoryReachedTwiceIsOfferedOnce(t *testing.T) {
	root := t.TempDir()
	repo := mainRoot(t, filepath.Join(root, "work", "ganymede"))

	want(t, found(t, inventory.Scan{Roots: []string{root, filepath.Join(root, "work")}}), []string{repo})
}

// Hidden directories are the harness's own worktrees, editor state and caches.
// None of them is a repo you would ask to be taken to.
func TestHiddenDirectoriesAreNotDescendedInto(t *testing.T) {
	root := t.TempDir()
	repo := mainRoot(t, filepath.Join(root, "ganymede"))
	mainRoot(t, filepath.Join(root, ".cache", "stale-clone"))

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{repo})
}

// A link into a tree already being scanned would walk it twice, and a link
// into one that is not is somewhere the scan roots deliberately do not reach.
func TestSymlinkedDirectoriesAreNotFollowed(t *testing.T) {
	root := t.TempDir()
	elsewhere := mainRoot(t, filepath.Join(t.TempDir(), "archived"))
	repo := mainRoot(t, filepath.Join(root, "ganymede"))
	if err := os.Symlink(elsewhere, filepath.Join(root, "archived")); err != nil {
		t.Fatal(err)
	}

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{repo})
}

// A scan root that is itself a checkout is a repo, so that pointing the
// harness straight at one repository still offers it.
func TestScanRootThatIsItselfARepositoryIsDiscovered(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "ganymede"))

	want(t, found(t, inventory.Scan{Roots: []string{root}}), []string{root})
}
