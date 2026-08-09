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
func (s Scan) Repos() ([]string, error) {
	depth := s.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}

	found := map[string]bool{}
	for _, root := range s.roots() {
		if err := walkRoot(root, depth, found); err != nil {
			return nil, err
		}
	}

	repos := make([]string, 0, len(found))
	for repo := range found {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	return repos, nil
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
			found[name(dir)] = true
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

// name is the one name a repository goes by: where it really is, with the
// symlinks on the way to it followed. It has to be the name repo.Root gives
// the same directory, or the working set would draw one repo twice — once for
// the Sessions running in it and once for the picker having been there.
func name(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
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
