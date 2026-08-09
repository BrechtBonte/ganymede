package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// The branch is where a Session's ticket is read from, so a checkout sitting on
// one has to say which.
func TestBranchIsTheOneTheCheckoutIsOn(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "checkout", "-q", "-b", "feat/FIRE-2841-paging")

	if got, want := repo.Branch(root), "feat/FIRE-2841-paging"; got != want {
		t.Errorf("Branch(%q) = %q, want %q", root, got, want)
	}
}

// A Worktree session is on a branch of its own, which is the whole reason it
// has a ticket the Main root beside it does not.
func TestWorktreeIsOnItsOwnBranch(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	worktree := filepath.Join(root, ".claude", "worktrees", "FIRE-2841-paging")
	git(t, root, "worktree", "add", "-q", "-b", "FIRE-2841-paging", worktree)

	if got, want := repo.Branch(worktree), "FIRE-2841-paging"; got != want {
		t.Errorf("Branch(%q) = %q, want %q", worktree, got, want)
	}
}

// Everything that is not a checkout on a branch is a checkout with no branch to
// read a ticket off. None of them is an error: a Session is perfectly entitled
// to be working in any of them, and the derivation just goes on to the next
// thing it knows.
func TestCheckoutWithNoBranchHasNone(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	// A PR checked out by hash in the Main root, which is exactly what the
	// Main root is for.
	git(t, root, "checkout", "-q", "--detach")

	outside := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{
		root,
		outside,
		// A worktree removed underneath the harness between the registry being
		// written and being read.
		filepath.Join(t.TempDir(), "removed"),
	} {
		if got := repo.Branch(dir); got != "" {
			t.Errorf("Branch(%q) = %q, want no branch", dir, got)
		}
	}
}
