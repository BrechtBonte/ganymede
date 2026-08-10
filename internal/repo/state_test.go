package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// Which checkout a Session is working in is what tells a Session holding the
// Main root from one deliberately staying out of it. Both group under the same
// Main root — that is what Root is for — so the question has to be asked
// separately, and a worktree living inside the root's own directory is exactly
// why it cannot be answered by comparing paths.
func TestCheckoutIsTheMainRootWhereverInsideItASessionIsWorking(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	sub := filepath.Join(root, "src", "handlers")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, ".claude", "worktrees", "FIRE-2841-paging")
	git(t, root, "worktree", "add", "-q", "-b", "FIRE-2841-paging", worktree)

	for dir, want := range map[string]string{
		root:     root,
		sub:      root,
		worktree: worktree,
	} {
		if got := repo.Checkout(dir); got != want {
			t.Errorf("Checkout(%q) = %q, want %q", dir, got, want)
		}
	}
}

// A Main root with an agent in it is not safe to check a PR out in, and one
// with nobody in it is. What the agent is doing never comes into it: an Idle
// Session holds the checkout its context is bound to just as firmly as a
// Working one, and a Worktree session holds none of it.
func TestMainRootIsInUseByAgentWhileASessionIsWorkingInItAndFreeOtherwise(t *testing.T) {
	const root = "/repos/service-ai-assistant"
	const worktree = root + "/.claude/worktrees/FIRE-2841-paging"

	for _, working := range [][]string{nil, {worktree}} {
		if got := repo.StateOf(root, working, false); got != repo.Free {
			t.Errorf("StateOf(%q, %q, false) = %q, want %q", root, working, got, repo.Free)
		}
	}
	working := []string{worktree, root}
	if got := repo.StateOf(root, working, false); got != repo.InUse {
		t.Errorf("StateOf(%q, %q, false) = %q, want %q", root, working, got, repo.InUse)
	}
}

// A root reserved by you and held by nobody is Claimed — the third state,
// distinct from Free because a PR should not be checked out over a review
// already under way in it.
func TestMainRootIsClaimedWhenReservedAndHeldByNobody(t *testing.T) {
	const root = "/repos/billing"

	if got := repo.StateOf(root, nil, true); got != repo.Claimed {
		t.Errorf("StateOf(%q, nil, true) = %q, want %q", root, got, repo.Claimed)
	}
}

// A claim never overrules a live occupant: a Session actually holding the
// root is what "In use by agent" means, and it stays true whether or not you
// have also reserved the root — the one thing this must never say is that a
// root is safe to review in while an agent is still sitting in it.
func TestAClaimNeverOverrulesALiveOccupant(t *testing.T) {
	const root = "/repos/billing"

	if got := repo.StateOf(root, []string{root}, true); got != repo.InUse {
		t.Errorf("StateOf(%q, %q, true) = %q, want %q", root, []string{root}, got, repo.InUse)
	}
}
