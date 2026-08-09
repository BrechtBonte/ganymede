package repo

import (
	"slices"
	"strings"
)

// Caution is what a Main root's checkout is carrying that checking a PR out
// over it would disturb. It is read off git and shown on the root's row
// whatever state the root is in: a Free root is safe to check out in, and these
// are the things worth knowing first.
type Caution struct {
	// Branch is the branch the root has strayed to — the one it is on when
	// that is not the repository's default. Empty on the default branch, and
	// empty when nothing could say which branch that is: a caution nobody can
	// stand behind is worse than none.
	Branch string
	// Detached says the root is on no branch at all, which is what a PR checked
	// out by hash leaves behind — the state the Main root exists to be put in,
	// and one no branch name would ever mention.
	Detached bool
	// Dirty says the root's tree has work in it that is not committed —
	// git's own reading of it, untracked files included, so that the marker
	// never disagrees with what `git status` tells you in the same checkout.
	Dirty bool
}

// Any reports whether there is anything to caution about at all.
func (c Caution) Any() bool { return c.Branch != "" || c.Detached || c.Dirty }

// CautionOf reads the cautions off the checkout at root.
func CautionOf(root string) Caution {
	branch := Branch(root)
	return Caution{
		Branch:   strayed(root, branch),
		Detached: branch == "" && adrift(root),
		Dirty:    dirty(root),
	}
}

// dirty says the checkout at root has uncommitted work in it.
//
// A clean tree and a directory git will not answer about come back the same
// way, which is exactly right here: neither is a tree with something in it to
// warn about.
//
// --no-optional-locks because this is read over and over, in the background,
// in checkouts you are working in. A status that refreshed the index would take
// the lock out from under the git command you just ran in that shell.
func dirty(root string) bool {
	_, changed := git(root, "--no-optional-locks", "status", "--porcelain")
	return changed
}

// strayed is branch when that is not the repository's default, and empty when
// it is on it — or when the checkout is on no branch at all, which is a caution
// of its own rather than a branch to name.
func strayed(root, branch string) string {
	if branch == "" {
		return ""
	}
	if usual, known := usual(root); !known || branch == usual {
		return ""
	}
	return branch
}

// adrift says the checkout at root has a commit checked out and no branch to
// call it — which is the difference between a Main root holding a PR and a
// directory that is no repository at all. Both of those have no branch, and
// only one of them is worth a word.
func adrift(root string) bool {
	_, checked := git(root, "rev-parse", "--verify", "--quiet", "HEAD")
	return checked
}

// usual is the repository's default branch — the one a Main root sits on when
// it is not in the middle of anything — and whether anything could say which it
// is.
//
// The remote's own answer comes first: it is the one thing that actually knows,
// and it is right about the repositories where the default is neither main nor
// master. It is also the one thing that can be missing — a clone made without
// it, or a repository with no remote at all — so a repository that has one of
// the two conventional names falls back on it.
//
// The pointer has to still lead somewhere. git writes it when the clone is made
// and never touches it again, so a repository whose upstream has renamed its
// default branch since is left pointing at a branch that has been pruned away —
// and taking that name would put a caution on the row about a root sitting
// exactly where it should be, for as long as the checkout exists. A pointer to
// nothing is not the remote answering; it is the remote's answer having gone.
//
// A repository that answers neither way has no default this can name, and gets
// no caution rather than a made-up one.
func usual(root string) (string, bool) {
	if head, ok := git(root, "rev-parse", "--abbrev-ref", "refs/remotes/origin/HEAD"); ok {
		// The answer has the remote on the front: origin/main.
		if _, branch, named := strings.Cut(head, "/"); named {
			return branch, true
		}
	}
	branches, ok := Branches(root)
	if !ok {
		return "", false
	}
	for _, name := range []string{"main", "master"} {
		if slices.Contains(branches, name) {
			return name, true
		}
	}
	return "", false
}
