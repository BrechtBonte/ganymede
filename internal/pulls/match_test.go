package pulls

import (
	"sync"
	"testing"
)

func TestOwnerAndRepoAreReadOutOfEveryRemoteSpelling(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:teamleadercrm/core.git":            "teamleadercrm/core",
		"git@github.com:teamleadercrm/core":                "teamleadercrm/core",
		"https://github.com/teamleadercrm/core.git":        "teamleadercrm/core",
		"https://github.com/teamleadercrm/core":            "teamleadercrm/core",
		"ssh://git@github.com/teamleadercrm/core.git":      "teamleadercrm/core",
		"https://user@github.com/BrechtBonte/ganymede.git": "BrechtBonte/ganymede",
		// Trailing whitespace is what git's own output carries.
		"git@github.com:teamleadercrm/core.git\n": "teamleadercrm/core",
		// Not GitHub, and not this harness's business: non-GitHub forges are
		// out of scope, and a wrong owner/repo would match a row it should not.
		"git@gitlab.com:teamleadercrm/core.git": "",
		"":                                      "",
		"not a url at all":                      "",
	} {
		if got := NameWithOwner(remote); got != want {
			t.Errorf("%q: got %q, want %q", remote, got, want)
		}
	}
}

func TestAnOriginIsReadOncePerRootAndKeptForTheProcess(t *testing.T) {
	// ~15 ms per root, once per process. A repository whose remote is
	// re-pointed while the Dashboard is up stays stale until restart, which is
	// the trade for never asking twice.
	var mu sync.Mutex
	asked := map[string]int{}
	origins := &Origins{Read: func(root string) string {
		mu.Lock()
		defer mu.Unlock()
		asked[root]++
		return "git@github.com:teamleadercrm/core.git"
	}}

	for range 3 {
		if got := origins.Of("/Users/you/Projects/core"); got != "teamleadercrm/core" {
			t.Fatalf("got %q", got)
		}
	}
	if asked["/Users/you/Projects/core"] != 1 {
		t.Errorf("git was asked %d times, want 1", asked["/Users/you/Projects/core"])
	}

	// A root with no origin is remembered as having none, so a directory that
	// is not a checkout is not asked about on every redraw either.
	quiet := &Origins{Read: func(string) string { return "" }}
	quiet.Of("/tmp/nowhere")
	quiet.Of("/tmp/nowhere")
	if got := quiet.Of("/tmp/nowhere"); got != "" {
		t.Errorf("got %q, want none", got)
	}
}

func TestOriginsIsSafeToAskFromSeveralGoroutines(t *testing.T) {
	origins := &Origins{Read: func(string) string { return "git@github.com:teamleadercrm/core.git" }}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			origins.Of("/Users/you/Projects/core")
			origins.Of("/Users/you/Projects/other" + itoa(i))
		}()
	}
	wg.Wait()
}

func matchable(list List, repo, head string, state string) Pull {
	return Pull{
		List: list, Repo: repo, Head: head, Number: 1,
		Mergeable: "MERGEABLE", MergeState: state, Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}
}

func TestAPullAndACheckoutAreTheSameWorkOnRepoAndBranch(t *testing.T) {
	// Branch alone is dead: 5 of 15 Pulls shared a head branch with a Pull in
	// a different repo, and a checkout was sitting on one of them.
	here := matchable(Authored, "teamleadercrm/focus-service-bookkeeping", "security/bump-fast-uri-3.1.8", "BEHIND")
	elsewhere := matchable(Authored, "teamleadercrm/focus-frontend", "security/bump-fast-uri-3.1.8", "BEHIND")
	set := Set{here, elsewhere}

	got := set.Matched("teamleadercrm/focus-service-bookkeeping", "security/bump-fast-uri-3.1.8")
	if len(got) != 1 || got[0].Repo != "teamleadercrm/focus-service-bookkeeping" {
		t.Errorf("got %d rows %v, want only the bookkeeping one", len(got), got)
	}
	if len(set.Matched("teamleadercrm/core", "security/bump-fast-uri-3.1.8")) != 0 {
		t.Error("matched a branch in a repository with no Pull on it")
	}
	// A worktree branch never pushed has no Pull with that head.
	if len(set.Matched("teamleadercrm/focus-service-bookkeeping", "never-pushed")) != 0 {
		t.Error("matched a branch no Pull has as its head")
	}
	// Neither half alone is ever enough to match.
	if len(set.Matched("", "security/bump-fast-uri-3.1.8")) != 0 || len(set.Matched("teamleadercrm/core", "")) != 0 {
		t.Error("matched on half the rule")
	}
}

func TestTheMarkFiresOnYourMoveAndNothingElse(t *testing.T) {
	const repo, head = "teamleadercrm/core", "PHX-4335-decode"
	for _, c := range []struct {
		says string
		set  Set
		want bool
	}{
		{"a rebase waiting in that working directory",
			Set{matchable(Authored, repo, head, "BEHIND")}, true},
		{"a review you owe in a main root on a colleague's branch",
			Set{matchable(Requested, repo, head, "BLOCKED")}, true},
		{"a Sent Pull asks nothing of you there",
			Set{matchable(Authored, repo, head, "BLOCKED")}, false},
		{"no Pull at all reads the same as a Sent one, which is the accepted loss",
			Set{}, false},
	} {
		if got := c.set.YourMove(repo, head); got != c.want {
			t.Errorf("%s: got %v, want %v", c.says, got, c.want)
		}
	}
}

func TestADraftMatchedToACheckoutMarksNothing(t *testing.T) {
	draft := matchable(Authored, "teamleadercrm/core", "PHX-4335-decode", "BLOCKED")
	draft.IsDraft = true
	if (Set{draft}).YourMove("teamleadercrm/core", "PHX-4335-decode") {
		t.Error("a Draft marked its Session row")
	}
}

func TestTwoPullsFromOneBranchMarkTheRowIfEitherIsYourMove(t *testing.T) {
	// The row never picks between them; Pulls highlights both.
	const repo, head = "teamleadercrm/core", "PHX-4335-decode"
	set := Set{
		matchable(Authored, repo, head, "BLOCKED"),
		matchable(Requested, repo, head, "BLOCKED"),
	}
	if len(set.Matched(repo, head)) != 2 {
		t.Error("only one of two Pulls on one branch matched")
	}
	if !set.YourMove(repo, head) {
		t.Error("the mark did not fire when one of two matched Pulls is Your move")
	}
}

func TestAStatelessPullMarksNoSessionRow(t *testing.T) {
	stateless := matchable(Authored, "teamleadercrm/core", "PHX-4335-decode", "UNKNOWN")
	stateless.Mergeable = "UNKNOWN"
	if (Set{stateless}).YourMove("teamleadercrm/core", "PHX-4335-decode") {
		t.Error("a row we could not read marked its Session")
	}
}
