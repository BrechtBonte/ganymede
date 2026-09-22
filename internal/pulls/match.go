package pulls

import (
	"os/exec"
	"strings"
	"sync"
)

// NameWithOwner reads owner/repo out of a git remote, in every spelling git
// hands one back: ssh, scp-style, https, and any of them with or without the
// .git suffix or a userinfo prefix.
//
// It is GitHub or nothing. A remote on another host answers with the empty
// string rather than with a guess — non-GitHub forges are out of scope, and a
// plausible-looking owner/repo from one would match a row it has nothing to do
// with.
func NameWithOwner(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	// URL-style first: scheme://[user@]host/owner/repo. It has to come first,
	// because an https remote also satisfies the scp-style shape — the scheme
	// reads as the host and everything after the colon as the path.
	if _, rest, ok := strings.Cut(remote, "://"); ok {
		host, path, ok := strings.Cut(rest, "/")
		if !ok || !isGitHub(host) {
			return ""
		}
		return trimmed(path)
	}
	// scp-style: [user@]host:owner/repo
	host, path, ok := strings.Cut(remote, ":")
	if !ok || strings.Contains(host, "/") || !isGitHub(host) {
		return ""
	}
	return trimmed(path)
}

// isGitHub says the host half of a remote is GitHub's, whatever userinfo is in
// front of it.
func isGitHub(host string) bool {
	if _, after, ok := strings.Cut(host, "@"); ok {
		host = after
	}
	return host == "github.com"
}

// trimmed is the owner/repo half of a remote, with the .git suffix gone and
// nothing more or less than two segments — a submodule path or a URL with a
// tail is not a repository this can name.
func trimmed(path string) string {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, repo, ok := strings.Cut(path, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return ""
	}
	return owner + "/" + repo
}

// Origins is what each Main root pushes to, in GitHub's own nameWithOwner.
//
// The harness has no equivalent of its own: a repo is a path labelled with
// filepath.Base, and matching that against GitHub's names is wrong the moment
// two organisations share a repository name. Measured across 103 clones, the
// ~/Projects/<org>/<repo> convention holds for 98 and fails silently on 5 —
// and silently is the problem, since the row simply never lights up, which is
// indistinguishable from having no Pull.
//
// Read once per root and kept for the process. A repository whose remote is
// re-pointed while the Dashboard is up stays stale until restart, which is the
// trade for never asking git twice. It reads .git/config, so nothing about the
// harness's network boundary changes here.
type Origins struct {
	// Read asks one root what it pushes to, as git would print it. Nil means
	// ask git.
	Read func(root string) string

	mu    sync.Mutex
	known map[string]string
}

// Of is the owner/repo the checkout at root pushes to, and the empty string
// for a directory with no GitHub origin — which is not an error: a repo with
// no remote, or one on another forge, simply has no Pull to be matched to.
func (o *Origins) Of(root string) string {
	if o == nil || root == "" {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if name, asked := o.known[root]; asked {
		return name
	}

	read := o.Read
	if read == nil {
		read = remoteOf
	}
	name := NameWithOwner(read(root))
	if o.known == nil {
		o.known = map[string]string{}
	}
	// Remembered even when it is nothing, so a directory that is not a
	// checkout is asked about once rather than on every redraw.
	o.known[root] = name
	return name
}

// remoteOf is git's own answer, and the empty string for every way of not
// having one — not a checkout, no origin, git not on PATH. None of those is an
// error worth returning: the row simply carries no mark.
func remoteOf(root string) string {
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Matched is the Pulls whose work is the work in a checkout: the same
// repository, and the same branch.
//
// Both halves are required. 5 of 15 measured Pulls shared a head branch with a
// Pull in a different repository — three repos on one dependabot branch — and a
// checkout was sitting on one of them at the time, so a branch-only rule would
// have matched one checkout to three Pulls in three repos.
//
// The rule is about the checkout rather than the process: two Sessions sharing
// one checkout are both on that branch, so both rows are marked, which is right
// — the mark is a claim about the working directory.
func (s Set) Matched(nameWithOwner, branch string) []Pull {
	if nameWithOwner == "" || branch == "" {
		return nil
	}
	var found []Pull
	for _, p := range s {
		if p.Repo == nameWithOwner && p.Head == branch {
			found = append(found, p)
		}
	}
	return found
}

// YourMove says the work in a checkout has a Pull waiting on you — which is
// the whole of what the Session row's mark claims.
//
// Not existence, which is measurably empty of information: of the 3 Pulls with
// a checkout on their head branch, all three were Sent. Your move earns its
// column because those states are the ones you act on in that working
// directory — Conflicted and Behind are rebases there, Rework is code you write
// there, Landable is the one press, and Yours is the review a main root exists
// to do.
//
// Two Pulls from one branch never make the row pick: the mark fires if either
// is Your move.
func (s Set) YourMove(nameWithOwner, branch string) bool {
	for _, p := range s.Matched(nameWithOwner, branch) {
		if p.YourMove() {
			return true
		}
	}
	return false
}
