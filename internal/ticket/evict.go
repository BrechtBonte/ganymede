package ticket

import (
	"errors"
	"os"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// surviving is the overrides whose checkout is still there. An override is a
// correction you made by hand, so it is only ever dropped on something the
// harness can see for itself: a branch a repo it just read no longer has, or a
// directory that is not there any more. A repo that cannot be asked at all has
// said nothing, and nothing is not evidence.
func surviving(kept []override) []override {
	// One question per repo rather than one per override: a repo with a dozen
	// corrections in it is still one branch list.
	branches := map[string]map[string]bool{}

	alive := make([]override, 0, len(kept))
	for _, held := range kept {
		if held.Worktree != "" {
			if gone(held.Worktree) {
				continue
			}
			alive = append(alive, held)
			continue
		}
		known, asked := branches[held.Root]
		if !asked {
			known = branchesIn(held.Root)
			branches[held.Root] = known
		}
		// A nil set is a repo that did not answer, which keeps everything in
		// it; the empty one belongs to a repo that is not there any more.
		if known != nil && !known[held.Branch] {
			continue
		}
		alive = append(alive, held)
	}
	return alive
}

// branchesIn is every branch the repository at root has, and nil when it could
// not be asked.
func branchesIn(root string) map[string]bool {
	if gone(root) {
		// A repo that is not there has no branches, which is the one way of
		// not answering that is an answer.
		return map[string]bool{}
	}
	named, ok := repo.Branches(root)
	if !ok {
		return nil
	}
	known := make(map[string]bool, len(named))
	for _, branch := range named {
		known[branch] = true
	}
	return known
}

// gone reports whether a directory has been removed. A directory the harness
// cannot stat for any other reason is taken as still there, for the same reason
// nothing else here guesses.
func gone(dir string) bool {
	_, err := os.Stat(dir)
	return errors.Is(err, os.ErrNotExist)
}
