// Package repo locates the Main root a directory belongs to — the repository's
// primary checkout, which is what the Dashboard groups Sessions under.
package repo

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Root returns the Main root of the repository containing dir. A Worktree
// session's directory resolves to the repository it was spawned from rather
// than to the worktree, so both rows land under the same header. A directory
// outside any repository is its own root.
func Root(dir string) string {
	// The common git directory belongs to the Main root even when dir is a
	// worktree, which is what makes this the main-root question rather than
	// the toplevel one. Absolute paths are asked for explicitly: git answers
	// a bare ".git" relative to dir otherwise.
	//
	// Only a common directory actually called ".git" says where a checkout
	// is. A submodule's is "<superproject>/.git/modules/<name>", and a
	// checkout made with --separate-git-dir keeps its elsewhere entirely —
	// neither has a Main root one directory above it.
	if common, ok := gitDir(dir, "--path-format=absolute", "--git-common-dir"); ok &&
		filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	// Everything else — a submodule, a detached git directory, or a git too
	// old for --path-format — falls back to the checkout dir is in. Grouping
	// a worktree under itself is worse than grouping it under its repo, and
	// better than not grouping it at all.
	if toplevel, ok := gitDir(dir, "--show-toplevel"); ok {
		return toplevel
	}
	return Absolute(dir)
}

func gitDir(dir string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", dir, "rev-parse"}, args...)...).Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", false
	}
	return path, true
}

// Absolute is the one name for a directory, so that two Sessions reaching the
// same place by different paths still group together. Symlinks are resolved
// because git resolves them, and a root only half the Sessions agree on would
// draw the repo twice. A directory that is no longer there cannot be resolved
// and is taken as written.
//
// It is exported because everything that names a directory has to name it the
// same way — discovery, the topology and this package all compare their
// answers with one another, and three spellings of "where is this really"
// would be three chances for one repo to appear as two.
func Absolute(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}
