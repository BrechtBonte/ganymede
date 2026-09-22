// Package pulls is your open pull requests on GitHub: the ones you wrote and
// the ones asking for your review, each carrying whose move it is.
//
// It answers what a Pull is, never how it reads. The nine words are the
// glossary's, the chains that pick between them are the whole of the state
// model, and the joins — a stack's parent, a checkout's Pull — are local and
// cost no request. Nothing here draws a column or knows a glyph.
package pulls

import "time"

// List is which of the two lists a Pull rides in. The two are structurally
// disjoint: GitHub will not request a review from a PR's own author, so there
// is no dedupe rule to write.
type List int

const (
	// Authored is `author:@me` — waiting on you the moment a review bounces
	// back or the branch conflicts.
	Authored List = iota
	// Requested is `review-requested:@me` — somebody waiting on your decision.
	Requested
)

// State is whose move a Pull is. The two lists carry different vocabularies,
// and every word here is in CONTEXT.md.
type State string

const (
	// Draft: nothing is asked of anyone; you marked it unfinished. Said of a
	// Pull in either list, whatever else is true of it.
	Draft State = "Draft"
	// Rework: a reviewer has asked for changes.
	Rework State = "Rework"
	// Conflicted: it cannot merge mechanically until you resolve it.
	Conflicted State = "Conflicted"
	// Behind: the base has moved under it.
	Behind State = "Behind"
	// Landable: you can merge it now — never said of a Stacked Pull, whose
	// parent is unmerged.
	Landable State = "Landable"
	// Sent: you have done your part and it is with the world, whether nobody
	// has looked yet, a reviewer is looking, or it is approved and the checks
	// are still running. It claims nothing about who holds it.
	Sent State = "Sent"
	// Approved: someone has already approved it, so your review is a second
	// opinion.
	Approved State = "Approved"
	// Yours: you owe this review.
	Yours State = "Yours"
	// Unresolved is the absence of a word, not a tenth one. GitHub computes
	// mergeability on a background test-merge commit and serves UNKNOWN until
	// it lands; a row still UNKNOWN after the cycle's second pass answers a
	// different question — "we could not tell" — and the section draws that
	// rather than saying it.
	Unresolved State = ""
)

// YourMove says the state is one yours to act on. It is the union CONTEXT.md
// defines, and Unresolved is deliberately outside it: a row that undercounts is
// the direction a wrong answer should err in.
func (s State) YourMove() bool {
	switch s {
	case Rework, Conflicted, Behind, Landable, Yours:
		return true
	}
	return false
}

// Pull is one open pull request, in the fields the query asks for.
//
// The GitHub enums are kept as the strings GitHub sends rather than parsed
// into types of our own. They are read in exactly one place — the chains
// below — and a private enum per field would be three more vocabularies to
// keep in step with an API that owns all three.
type Pull struct {
	// List is which query returned it.
	List List
	// Repo is GitHub's own nameWithOwner. The harness's label for a repo is a
	// directory basename, which it cannot be here: a Pull may be in a repo
	// this machine holds no path for.
	Repo string
	// Number is the PR number, which with Repo identifies it.
	Number int
	// Title is the PR's title. The row has no room for it — see the spec's
	// "There is no title" — and it is carried for the one thing that does:
	// telling a person what they are looking at in a diagnostic.
	Title string
	// URL is where `o` opens. A Pull carries its own, so internal/browser
	// needs nothing.
	URL string
	// IsDraft is GitHub's isDraft, which outranks everything in both chains.
	IsDraft bool
	// Base and Head are baseRefName and headRefName. Head is what matches a
	// Pull to a checkout and what a stacked Pull's parent is found by; Base
	// is what withholds Landable.
	Base, Head string
	// Default is the repository's default branch. Landable means you can press
	// merge now, and CLEAN into an unmerged parent is not that.
	Default string
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN.
	Mergeable string
	// MergeState is mergeStateStatus: BLOCKED, CLEAN, HAS_HOOKS, BEHIND,
	// DIRTY, UNSTABLE or UNKNOWN. It is the only source for Behind and the
	// only exact one for Landable, which is why it is kept despite its cost.
	MergeState string
	// Review is reviewDecision: APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	// or empty. Empty must be read as "nobody has asked for a review" rather
	// than as missing data — which cannot occur in REQUESTED, where membership
	// already asserts that a review was asked of you.
	Review string
	// Checks is statusCheckRollup.state: SUCCESS, FAILURE, ERROR, PENDING,
	// EXPECTED or empty for a PR with no checks at all.
	Checks string
	// Author is the login that opened it, which is what tells a dependabot bump
	// from a colleague's work in a diagnostic. Rows are not filtered on it:
	// Pulls reports your census, it does not curate it.
	Author string
	// UpdatedAt is GitHub's updatedAt. Nothing draws it — the section's age is
	// the fetch's, not the row's — and it is the tiebreaker of last resort.
	UpdatedAt time.Time
}

// State is whose move this Pull is: the first match down its list's own chain.
func (p Pull) State() State {
	// Draft outranks everything in both lists. A draft is in nobody's queue,
	// so nothing it carries is urgent — you will resolve the conflict before
	// marking it ready, and the alternative nags you about work you have
	// explicitly declared unfinished.
	if p.IsDraft {
		return Draft
	}
	if p.List == Requested {
		// Membership in review-requested:@me already asserts that GitHub
		// thinks you owe a review, so the fallback needs no signal of its own.
		if p.Review == "APPROVED" {
			return Approved
		}
		return Yours
	}
	// Rework outranks Conflicted. When a reviewer has asked for work you are
	// rewriting the code anyway and the rebase comes afterwards, so the social
	// block names your next move. The two genuinely co-occur, and the chain is
	// what settles it rather than an assumption that they are exclusive.
	if p.Review == "CHANGES_REQUESTED" {
		return Rework
	}
	// Everything below reads the test-merge commit, which is the half that
	// goes UNKNOWN. Falling through to Sent while it is unresolved would turn
	// the three states meaning Your move into the one meaning the opposite.
	if !p.Resolved() {
		return Unresolved
	}
	if p.Mergeable == "CONFLICTING" || p.MergeState == "DIRTY" {
		return Conflicted
	}
	if p.MergeState == "BEHIND" {
		return Behind
	}
	// Withheld on a non-default base: CLEAN there means "mergeable into an
	// unmerged parent", which is not the action you want. The row falls
	// through to Sent, which is the reason a Stacked Pull is genuinely
	// different rather than merely quieter.
	if (p.MergeState == "CLEAN" || p.MergeState == "HAS_HOOKS") && p.Base == p.Default {
		return Landable
	}
	return Sent
}

// Resolved says GitHub's background test merge has landed, so the fields the
// AUTHORED chain reads mean something. The two go UNKNOWN together and are
// both checked, because either one alone would be a chain reading half an
// answer.
func (p Pull) Resolved() bool {
	return p.Mergeable != "UNKNOWN" && p.Mergeable != "" && p.MergeState != "UNKNOWN"
}

// YourMove says this Pull's state is one yours to act on.
func (p Pull) YourMove() bool { return p.State().YourMove() }

// Failing says the checks are exceptional — the one thing on a Pull worth a
// mark of its own.
//
// PENDING and SUCCESS are both resting states: drawn literally the three
// rollup marks land on 13 of 17 rows, and a mark two rows in three carry is a
// mark the eye stops reading. Drawing only the failure leaves four, and frees
// the tick to mean exactly one thing.
func (p Pull) Failing() bool {
	return p.Checks == "FAILURE" || p.Checks == "ERROR"
}

// Approved says a person has approved this Pull, in AUTHORED only. In
// REQUESTED the approval is already the state word, and a mark beside it would
// be the row saying the same thing twice.
//
// It is orthogonal to the state rather than a rival to it: a Pull approved
// with its checks failing is Sent carrying both marks, which is what resolves
// the one real collision in the measured data without a priority fight.
func (p Pull) Approved() bool {
	return p.List == Authored && p.Review == "APPROVED"
}

// Stacked says the Pull is based on something other than its repository's
// default branch — the reason Landable is withheld from it, and the whole of
// what its row can honestly say. It never dims a row: measured, the only
// stacked Pulls were the only three rows in the section with a person waiting
// behind them.
//
// A repository whose default branch could not be read is not stacked. Every
// row in it would otherwise carry the mark, which is a worse answer than none.
func (p Pull) Stacked() bool {
	return p.Default != "" && p.Base != "" && p.Base != p.Default
}
