package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// defaulting is a Main root whose default branch is named, since git names the
// first branch after whatever the machine running the test has configured.
func defaulting(t *testing.T, name string) string {
	t.Helper()
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "branch", "-M", name)
	return root
}

// write puts a file in the checkout at root.
func write(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The Main root is where a PR is checked out, so a root already sitting on a
// branch of its own is worth a word before you check one out on top of it. The
// branch is named: which one it is is the difference between somebody's PR from
// Tuesday and the thing you were in the middle of.
func TestCautionNamesTheBranchAMainRootHasStrayedTo(t *testing.T) {
	root := defaulting(t, "main")
	git(t, root, "checkout", "-q", "-b", "fix/toolbar-focus")

	if got := repo.CautionOf(root); got.Branch != "fix/toolbar-focus" {
		t.Errorf("CautionOf(%q) = %+v, want it to name the branch the root strayed to", root, got)
	}
}

// A root back on its default branch is a root with nothing to say. The markers
// have to clear as readily as they appear, or they are decoration.
func TestNoCautionOnTheDefaultBranch(t *testing.T) {
	root := defaulting(t, "main")

	if got := repo.CautionOf(root); got.Any() {
		t.Errorf("CautionOf(%q) = %+v, want nothing to caution about", root, got)
	}
}

// A PR checked out by hash leaves the Main root on no branch at all. It is the
// state the root is most often found in after a review — the one thing this
// harness says the Main root is for — and a caution that only read branch names
// would call it clean.
func TestCautionOnAMainRootLeftOnNoBranch(t *testing.T) {
	root := defaulting(t, "main")
	git(t, root, "checkout", "-q", "--detach")

	if got := repo.CautionOf(root); !got.Detached {
		t.Errorf("CautionOf(%q) = %+v, want it to caution that the root is on no branch", root, got)
	}
}

// Work sitting uncommitted in the Main root is the other half of the caution:
// checking a PR out over it is a conflict at best and somebody's afternoon at
// worst.
func TestCautionOnAMainRootWithUncommittedWork(t *testing.T) {
	root := defaulting(t, "main")
	write(t, root, "handlers.go", "package main\n")
	git(t, root, "add", "handlers.go")
	git(t, root, "commit", "-q", "-m", "handlers")
	write(t, root, "handlers.go", "package main // and then some\n")

	if got := repo.CautionOf(root); !got.Dirty {
		t.Errorf("CautionOf(%q) = %+v, want it to caution that the tree is dirty", root, got)
	}
}

// An untracked file counts as work in the tree. It is what git says about the
// same checkout, and a marker that quietly disagreed with `git status` would be
// one you stop believing.
func TestUntrackedWorkIsADirtyTree(t *testing.T) {
	root := defaulting(t, "main")
	write(t, root, "scratch.md", "half a thought\n")

	if got := repo.CautionOf(root); !got.Dirty {
		t.Errorf("CautionOf(%q) = %+v, want it to caution that the tree is dirty", root, got)
	}
}

// A directory that is no repository has no git state to be cautious about — a
// Session working in a notes folder, or in a checkout that went away underneath
// the harness. Neither of them is on a branch, and neither is a PR left checked
// out in a Main root.
func TestNoCautionOutsideARepository(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{outside, filepath.Join(t.TempDir(), "removed")} {
		if got := repo.CautionOf(dir); got.Any() {
			t.Errorf("CautionOf(%q) = %+v, want nothing to caution about", dir, got)
		}
	}
}

// Which branch is default is the repository's own answer, not a guess at it: a
// repository whose default is neither main nor master is one where guessing
// would put a caution on a root that is exactly where it should be.
func TestDefaultBranchIsTheOneTheRemoteNames(t *testing.T) {
	root := defaulting(t, "2.x")
	git(t, root, "remote", "add", "origin", root)
	git(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/2.x")
	// Named so that a harness guessing at the default would find this one and
	// have nothing to say.
	git(t, root, "checkout", "-q", "-b", "main")

	if got := repo.CautionOf(root); got.Branch != "main" {
		t.Errorf("CautionOf(%q) = %+v, want it to caution that the root is off 2.x", root, got)
	}
}
