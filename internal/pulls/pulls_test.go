package pulls

import "testing"

func TestTheStateAPullIsIn(t *testing.T) {
	// A healthy PR under this org's branch protection rests at BLOCKED with
	// its checks PENDING, so every case below starts from that and moves one
	// field: a case built on CLEAN would be testing a population that is 2 of
	// 15 rather than 13.
	resting := Pull{
		List: Authored, Repo: "teamleadercrm/core", Number: 48032,
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Review: "REVIEW_REQUIRED",
		Checks: "PENDING", Base: "master", Default: "master",
	}
	with := func(change func(*Pull)) Pull {
		p := resting
		change(&p)
		return p
	}

	for _, c := range []struct {
		says string
		pull Pull
		want State
	}{
		{"a draft is in nobody's queue", with(func(p *Pull) { p.IsDraft = true }), Draft},
		{"a draft outranks a conflict it has not been asked to resolve",
			with(func(p *Pull) { p.IsDraft, p.Mergeable = true, "CONFLICTING" }), Draft},
		{"changes requested is your move", with(func(p *Pull) { p.Review = "CHANGES_REQUESTED" }), Rework},
		{"rework outranks conflicted: you are rewriting the code anyway",
			with(func(p *Pull) { p.Review, p.Mergeable = "CHANGES_REQUESTED", "CONFLICTING" }), Rework},
		{"conflicted reads mergeable", with(func(p *Pull) { p.Mergeable = "CONFLICTING" }), Conflicted},
		{"conflicted reads DIRTY too", with(func(p *Pull) { p.MergeState = "DIRTY" }), Conflicted},
		{"the base has moved under it", with(func(p *Pull) { p.MergeState = "BEHIND" }), Behind},
		{"conflicted outranks behind", with(func(p *Pull) { p.Mergeable, p.MergeState = "CONFLICTING", "BEHIND" }), Conflicted},
		{"clean on the default branch is one press", with(func(p *Pull) { p.MergeState = "CLEAN" }), Landable},
		{"hooks are clean too", with(func(p *Pull) { p.MergeState = "HAS_HOOKS" }), Landable},
		{"landable is withheld on a non-default base",
			with(func(p *Pull) { p.MergeState, p.Base = "CLEAN", "PHX-4335-decode-search-term" }), Sent},
		{"nobody's move but the reviewer's", resting, Sent},
		{"an approved authored row is still Sent", with(func(p *Pull) { p.Review = "APPROVED" }), Sent},
		{"mergeability that never resolved has no word",
			with(func(p *Pull) { p.Mergeable, p.MergeState = "UNKNOWN", "UNKNOWN" }), Unresolved},
		{"an unresolved draft is still a draft",
			with(func(p *Pull) { p.IsDraft, p.Mergeable = true, "UNKNOWN" }), Draft},
	} {
		if got := c.pull.State(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.says, got, c.want)
		}
	}
}

func TestTheStateARequestedPullIsIn(t *testing.T) {
	resting := Pull{
		List: Requested, Repo: "teamleadercrm/core", Number: 48032,
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Review: "REVIEW_REQUIRED",
		Checks: "PENDING", Base: "master", Default: "master",
	}
	with := func(change func(*Pull)) Pull {
		p := resting
		change(&p)
		return p
	}

	for _, c := range []struct {
		says string
		pull Pull
		want State
	}{
		{"converted to draft while its request stands", with(func(p *Pull) { p.IsDraft = true }), Draft},
		{"somebody else has approved it", with(func(p *Pull) { p.Review = "APPROVED" }), Approved},
		{"you owe this review", resting, Yours},
		{"a null review decision cannot mean missing data here",
			with(func(p *Pull) { p.Review = "" }), Yours},
		{"mergeability never touches the requested chain",
			with(func(p *Pull) { p.Mergeable, p.MergeState = "UNKNOWN", "UNKNOWN" }), Yours},
		{"nor does a conflict", with(func(p *Pull) { p.Mergeable = "CONFLICTING" }), Yours},
	} {
		if got := c.pull.State(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.says, got, c.want)
		}
	}
}

func TestWhichStatesAreYourMove(t *testing.T) {
	for state, want := range map[State]bool{
		Rework: true, Conflicted: true, Behind: true, Landable: true, Yours: true,
		Draft: false, Sent: false, Approved: false, Unresolved: false,
	} {
		if got := state.YourMove(); got != want {
			t.Errorf("%q: got %v, want %v", state, got, want)
		}
	}
}

func TestAStatelessRowIsNotYourMove(t *testing.T) {
	// The undercount the design chose deliberately: a row we could not read
	// marks nothing, rather than claiming a move that may not be yours.
	stateless := Pull{List: Authored, Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Base: "master", Default: "master"}
	if stateless.Resolved() {
		t.Error("an UNKNOWN row reports itself resolved")
	}
	if stateless.YourMove() {
		t.Error("a row with no state is Your move")
	}
}
