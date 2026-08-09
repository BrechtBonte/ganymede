package repo

import "strings"

// Branch is the branch the checkout at dir is on, and empty for every checkout
// that is not on one — a detached HEAD, a directory outside any repository, a
// worktree that has been removed underneath us.
//
// None of those is an error worth returning. The branch is read to find the
// ticket a Session is working on, and a checkout that cannot name one has not
// failed at anything: it simply has no branch to read, and the derivation goes
// on to the next thing it knows.
//
// symbolic-ref rather than rev-parse --abbrev-ref, which answers the literal
// word "HEAD" on a detached checkout — a branch name no repository ever has,
// and one that would read as a ticketless branch rather than as no branch.
func Branch(dir string) string {
	branch, _ := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	return branch
}

// Branches are the branches the repository at root has, and whether it could be
// asked at all. The two are worth telling apart: a repository that has just lost
// a branch and a directory that is not a repository this morning look identical
// from the outside, and only the first of them has said anything.
//
// A repository with no branches whatsoever — one initialised a minute ago, with
// nothing committed — reads here as one that could not be asked. That is the
// cautious end of the distinction, and the only caller is a caution.
func Branches(root string) ([]string, bool) {
	// for-each-ref rather than `branch --list`, whose output is decorated for
	// somebody reading it and can be configured to be decorated further.
	out, ok := git(root, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !ok {
		return nil, false
	}
	return strings.Split(out, "\n"), true
}
