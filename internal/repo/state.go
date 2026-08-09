package repo

import "slices"

// State is what a Main root is doing, in the sense that decides whether a PR
// can be checked out in it. The vocabulary is CONTEXT.md's, which is normative.
//
// Claimed — the root reserved by you rather than held by an agent — is the
// third of them, and is not here yet.
type State string

const (
	// Free: no live Session is working in the Main root. Safe to check a PR
	// out in.
	Free State = "Free"
	// InUse: a live Session has the Main root as its checkout. Strict, and
	// softened only by a Takeover.
	InUse State = "In use by agent"
)

// Glyph is how a State reads at a glance: one column wide, from the validated
// sidepanel mock. A filled mark is a root with somebody in it, an empty one a
// root you can have.
func (s State) Glyph() string {
	switch s {
	case InUse:
		return "▣"
	case Free:
		return "▢"
	}
	return ""
}

// StateOf is the state of the Main root root, given the checkouts every live
// Session is working in — Checkout's answer for each of them.
//
// What those Sessions are doing is deliberately not asked for. An Idle agent
// still holds the context it built up in that checkout, so a root with one in
// it is no more available than a root with a turn running, and a state model
// that softened for Idle would be inviting exactly the collision this exists to
// prevent. Ending an Idle Session to take its root is a Takeover: a thing you
// do on purpose, not a thing the state does quietly on your behalf.
func StateOf(root string, working []string) State {
	if slices.Contains(working, root) {
		return InUse
	}
	return Free
}

// Checkout is the checkout dir is working in: the Main root itself, wherever
// inside it dir is, or the worktree when dir is in one of those instead.
//
// It is the question Root deliberately does not answer. Root groups a Worktree
// session under the repository it came from, which is what the tree is for;
// this says which of that repository's checkouts the Session actually has its
// hands on, which is what tells a root in use from a free one. Comparing paths
// could not do it: `claude --worktree` puts its worktrees under the Main root's
// own directory, so a Worktree session's cwd is inside the root it is staying
// out of.
//
// A directory outside every repository is its own checkout, for the same reason
// it is its own root: the registry's cwd is ground truth, and a Session working
// there still holds the directory it is in.
func Checkout(dir string) string {
	if toplevel, ok := gitDir(dir, "--show-toplevel"); ok {
		return toplevel
	}
	return absolute(dir)
}
