package pulls

import (
	"strconv"
	"testing"
)

func itoa(n int) string { return strconv.Itoa(n) }

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pull(list List, repo string, number int) Pull {
	return Pull{
		List: list, Repo: repo, Number: number, Head: "head-" + repo,
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}
}

func TestEachListSortsByRepoThenNumber(t *testing.T) {
	// Sorting by repo keeps a stack contiguous, which is what makes a bare
	// stack mark readable: the parent is the row directly above three times in
	// four.
	set := Set{
		pull(Authored, "teamleadercrm/core", 48034),
		pull(Requested, "teamleadercrm/api-internal", 1564),
		pull(Authored, "teamleadercrm/core", 48032),
		pull(Authored, "teamleadercrm/prqa", 200),
		pull(Requested, "teamleadercrm/focus-frontend", 7430),
		pull(Authored, "teamleadercrm/core", 48033),
	}

	var authored []string
	for _, p := range set.Of(Authored) {
		authored = append(authored, p.Repo+"#"+itoa(p.Number))
	}
	want := []string{
		"teamleadercrm/core#48032", "teamleadercrm/core#48033",
		"teamleadercrm/core#48034", "teamleadercrm/prqa#200",
	}
	if !equal(authored, want) {
		t.Errorf("AUTHORED: got %v, want %v", authored, want)
	}

	var requested []string
	for _, p := range set.Of(Requested) {
		requested = append(requested, p.Repo)
	}
	if !equal(requested, []string{"teamleadercrm/api-internal", "teamleadercrm/focus-frontend"}) {
		t.Errorf("REQUESTED: got %v", requested)
	}
}

func TestOnlyTheRowsThatCameBackUnknownAreCollected(t *testing.T) {
	resolved := pull(Authored, "teamleadercrm/core", 48032)
	unknown := pull(Authored, "teamleadercrm/prqa", 200)
	unknown.Mergeable, unknown.MergeState = "UNKNOWN", "UNKNOWN"
	// REQUESTED is untouched by the second pass: isDraft and reviewDecision
	// never go UNKNOWN, so re-reading one would be a request bought for
	// nothing.
	requested := pull(Requested, "teamleadercrm/api-internal", 1564)
	requested.Mergeable, requested.MergeState = "UNKNOWN", "UNKNOWN"

	got := Set{resolved, unknown, requested}.Unresolved()
	if len(got) != 1 || got[0].Number != 200 {
		t.Errorf("got %d rows %v, want only the authored 200", len(got), got)
	}
}

func TestTheStackParentResolvesByLocalJoin(t *testing.T) {
	// Every other row's head branch against this row's base — no extra
	// request, which is why the mark costs nothing.
	parent := pull(Authored, "teamleadercrm/core", 48032)
	parent.Head = "PHX-4335-decode-search-term-for-subscription-title"
	child := pull(Authored, "teamleadercrm/core", 48033)
	child.Base = parent.Head
	set := Set{parent, child}

	found, ok := set.ParentOf(child)
	if !ok || found.Number != 48032 {
		t.Errorf("got %v %v, want 48032", found.Number, ok)
	}
	if _, ok := set.ParentOf(parent); ok {
		t.Error("a Pull on the default branch found itself a parent")
	}
}

func TestTheParentFallsBackToNothingWhenItIsNotInTheSet(t *testing.T) {
	// Measured on the day: one of the four stacked rows had a parent the
	// section did not hold, and its branch name was 30 columns. The mark stays
	// bare and says only that this row is not on the default branch.
	orphan := pull(Authored, "teamleadercrm/prqa", 200)
	orphan.Base = "fix/installation-id-from-redis"
	set := Set{orphan}

	if _, ok := set.ParentOf(orphan); ok {
		t.Error("found a parent that is not in the set")
	}
	if !orphan.Stacked() {
		t.Error("an orphaned stacked Pull stopped reporting itself stacked")
	}
}

func TestAParentInAnotherRepositoryIsNeverJoined(t *testing.T) {
	// 5 of 15 Pulls shared a head branch with a Pull in a different repo, so a
	// join on the branch alone would cross repositories.
	here := pull(Authored, "teamleadercrm/focus-service-bookkeeping", 979)
	here.Base = "security/bump-fast-uri-3.1.8"
	elsewhere := pull(Authored, "teamleadercrm/focus-frontend", 7430)
	elsewhere.Head = "security/bump-fast-uri-3.1.8"

	if _, ok := (Set{here, elsewhere}).ParentOf(here); ok {
		t.Error("joined a parent in another repository")
	}
}
