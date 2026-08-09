// Package inventory finds the repositories the harness can reach: every Main
// root under the scan roots.
//
// This is the full inventory the picker offers, not the working set. The
// Dashboard shows a handful of repos and this walks hundreds, so the scan is
// bounded on both ends — it goes only so deep, and it stops the moment a path
// reaches a checkout — and it never runs git. Whether a directory is a Main
// root is a question the filesystem answers on its own: a checkout's ".git" is
// a directory, and a Worktree checkout's is a file naming the repository it
// came from.
package inventory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/repo"
)

// DefaultDepth is how many directories below a scan root a repository may sit
// and still be found. Three reaches the way projects are commonly filed —
// ~/Projects/<organisation>/<repo> — with a directory to spare, and stops the
// scan short of the trees vendored inside them.
const DefaultDepth = 3

// Scan is where the harness looks for repositories.
type Scan struct {
	// Roots are the directories scanned. Empty means the default.
	Roots []string
	// Depth is how many directories below a scan root a repository may sit.
	// Zero means DefaultDepth.
	Depth int
}

// Default is where the harness looks unless it is told otherwise.
func Default() (Scan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Scan{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Scan{Roots: []string{filepath.Join(home, "Projects")}, Depth: DefaultDepth}, nil
}

// Repos returns every Main root under the scan roots, sorted, and each one
// once however many roots reach it.
//
// A scan root that is not there is nothing to offer rather than a failure: the
// roots are configuration, and configuration outlives the directories it
// names. A root that is there and cannot be read is the other way round —
// the picker is quietly offering less than it should, which is worth saying.
// Directories below a root are skipped when they cannot be read, since one
// unreadable vendored tree is no reason to lose the whole picker.
//
// What was found comes back either way. A root on a mount that has gone away
// says so, and the repos under every other root are still the ones you were
// about to ask for — losing them as well would turn one unreachable directory
// into a picker with nothing in it.
func (s Scan) Repos() ([]string, error) {
	depth := s.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}

	found := map[string]bool{}
	var failed []error
	for _, root := range s.roots() {
		if err := walkRoot(root, depth, found); err != nil {
			failed = append(failed, err)
		}
	}

	repos := make([]string, 0, len(found))
	for repo := range found {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	return repos, errors.Join(failed...)
}

func (s Scan) roots() []string {
	if len(s.Roots) > 0 {
		return s.Roots
	}
	if fallback, err := Default(); err == nil {
		return fallback.Roots
	}
	return nil
}

// walk collects the Main roots at or below dir, descending no further than
// depth directories and no further than the first checkout on any path.
func walk(dir string, depth int, found map[string]bool) error {
	if checkout, root := checkoutAt(dir); checkout {
		if root {
			// Named the way repo.Root names the same directory, or the working
			// set would draw one repo twice — once for the Sessions running in
			// it and once for the picker having been there.
			found[repo.Absolute(dir)] = true
		}
		// Below a checkout is that checkout's own contents. A repository
		// vendored inside another is part of the repo it sits in, and a
		// Worktree checkout is somewhere its Main root already stands for.
		return nil
	}
	if depth == 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("scan %s for repositories: %w", dir, err)
	}
	for _, entry := range entries {
		// IsDir reports on the entry itself, so a symlink is not one: a link
		// into a tree already being scanned would walk it twice, and a link out
		// of the scan roots reaches where they deliberately do not.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		// One unreadable directory costs its own subtree and nothing else.
		_ = walk(filepath.Join(dir, entry.Name()), depth-1, found)
	}
	return nil
}

// checkoutAt reports whether dir is a git checkout, and whether it is a Main
// root — the primary checkout, whose ".git" is the repository itself. A
// Worktree checkout, a submodule, and a checkout made with --separate-git-dir
// all keep a ".git" file naming a git directory that lives elsewhere, and none
// of the three is a repo of its own to be taken to.
func checkoutAt(dir string) (checkout, root bool) {
	git, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false, false
	}
	return true, git.IsDir()
}

// walkRoot is walk over a scan root, where a directory that is not there is
// not an error.
func walkRoot(dir string, depth int, found map[string]bool) error {
	err := walk(dir, depth, found)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
