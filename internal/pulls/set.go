package pulls

import (
	"cmp"
	"slices"
)

// Set is one cycle's answer: both lists in one slice, since the two are
// structurally disjoint and nothing ever has to dedupe them.
//
// It is the whole of what the section draws. Nothing about it is remembered
// across restarts: a Pull decays inside the poll interval, and a set replayed
// from disk at start would be GitHub's own two-hour lag with a longer fuse.
type Set []Pull

// Of is one list, in the order the section draws it: by repo, then by number
// inside each repo.
//
// Sorting by repo is what keeps a stack contiguous, which is what lets the
// stack mark stay bare — the parent is the row directly above it three times
// in four, and naming it would cost seven columns off every row in the
// section rather than off the four that are stacked.
func (s Set) Of(list List) []Pull {
	rows := make([]Pull, 0, len(s))
	for _, p := range s {
		if p.List == list {
			rows = append(rows, p)
		}
	}
	slices.SortFunc(rows, func(a, b Pull) int {
		if d := cmp.Compare(a.Repo, b.Repo); d != 0 {
			return d
		}
		return cmp.Compare(a.Number, b.Number)
	})
	return rows
}

// Unresolved is the AUTHORED rows whose test-merge commit had not landed when
// the first pass read them — the rows the cycle's second pass re-reads, and
// nothing else.
//
// REQUESTED is left out on purpose. isDraft and reviewDecision never go
// UNKNOWN, so its chain is complete on the first pass and a re-read there would
// be a request bought for an answer already in hand.
func (s Set) Unresolved() []Pull {
	var rows []Pull
	for _, p := range s {
		if p.List == Authored && !p.Resolved() {
			rows = append(rows, p)
		}
	}
	return rows
}

// ParentOf is the Pull a stacked one sits on, found by matching its base
// against the other rows' head branches inside the same repository.
//
// The join is local and costs no request, which is the whole reason the stack
// mark is affordable. It is scoped to the repository because 5 of 15 measured
// Pulls shared a head branch with a Pull in a different repo — the same
// collision that kills a branch-only match to a checkout.
//
// A parent outside the set is not an error. The lists hold your Pulls, and a
// colleague's parent branch is simply not among them; the row keeps its bare
// mark, which is all it could honestly say either way.
func (s Set) ParentOf(p Pull) (Pull, bool) {
	if !p.Stacked() {
		return Pull{}, false
	}
	for _, other := range s {
		if other.Repo == p.Repo && other.Head == p.Base {
			return other, true
		}
	}
	return Pull{}, false
}
