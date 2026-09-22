# Pulls Section Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put your open pull requests from every repo on GitHub — AUTHORED and REQUESTED — at the foot of the Dashboard, each row saying whose move it is, and mark the Session rows whose checkout has a Pull waiting on you.

**Architecture:** A new `internal/pulls` package owns the domain and the network: one `gh api graphql` subprocess per pass, a two-pass cycle on a thirty-minute clock, the nine state words, and the local joins that resolve a stack parent and match a Pull to a checkout. It draws nothing. `internal/dashboard/pulls.go` renders the section as a pure function of the fetched set, using the Dashboard's own `spread`/`tail`/`fitKeys` helpers, and takes the foot's box as a fifth case in `selected()`'s existing dispatch chain. The Dock stays two panes and `state.json` gains no section.

**Tech Stack:** Go 1.26.2 (stdlib plus the lipgloss/ansi already in `go.mod`), `gh` as a runtime dependency on the footing tmux and `claude` are on, `git remote get-url origin` for the repo identity.

**Spec:** `docs/superpowers/specs/2026-09-22-pulls-section-design.md`. Read it before Task 1 — it carries the reasoning and the measurements this plan only carries the shape of. Where the two disagree, the spec wins.

## Global Constraints

- **No new Go dependencies.** `go.mod` must not gain a line.
- **`internal/pulls` draws nothing.** No lipgloss, no glyphs, no column arithmetic. It answers *what* each Pull is; `internal/dashboard` decides how that reads.
- **`state.json` gains no section.** Nothing about Pulls is written to the sidecar, ever. `internal/config` is not touched.
- **The Dock stays exactly two panes.** `reattachClients` in `internal/topology/harness.go` must not change — it clears any pane past the second, hardened in `a8e6c4f`.
- **`strip.go`, `signatureColumns`, `signedWhenItFits` and `TestANarrowStatusLineKeepsTheCountAndGivesUpTheSignature` are untouched.** No Pulls number leaves the Dashboard.
- **`CONTEXT.md` is a glossary and nothing else.** No new term is due for any of this work, and no implementation detail lands there.
- **The nine words are exact:** `Draft`, `Rework`, `Conflicted`, `Behind`, `Landable`, `Sent` (AUTHORED); `Draft`, `Approved`, `Yours` (REQUESTED). A row with no state draws `—`, which is not a tenth word.
- **The section is 40 columns.** No line it draws may exceed `m.width`, at any width.
- **The tree keeps at least 21 rows at height 45 with Pulls open.** The section is capped at half the usable height.
- **Commit style:** free-form imperative subject reading as a sentence, no Conventional Commits prefix, 72 chars max, body explaining why when it is not obvious, and the `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` trailer as a final `-m`. Use the `atomic-commits` skill. Match the voice of `Give Pulls its way in, its keys, and its place in the legend`.
- **Comment voice:** this codebase explains *why* in full sentences above the code, not what the line does. Match `internal/dashboard/dashboard.go` and `internal/release/watch.go`. Every exported symbol gets a doc comment.
- **Run tests with** `go test ./...` from the repo root. Two tmux tests are flaky under parallel `go test ./...` — the popup reopen test and the ghostty open test. **The baseline fails them too.** Confirm against a clean baseline before assuming a change broke something.

## Two corrections to the spec, made here

1. **The query needs `defaultBranchRef { name }`.** The spec's query block lists `repository { nameWithOwner }`, but `Landable` is defined as `CLEAN`/`HAS_HOOKS` **and** the base being the default branch, and "`Landable` is withheld on a non-default base" is the whole reason stacked Pulls differ. The field is on an object already being fetched and does not change the measured `cost: 1`. Task 4 adds it.
2. **`selected()` is split rather than extended.** The spec says Pulls becomes a fifth case in `selected()`'s chain. `selected()` returns `[]string` and cannot see the line budget the cap needs, and the label has to change with the same dispatch. Task 11 renames the chain to `foot(space int) ([]string, string)` — the same five cases in the same order, returning its lines and its label together — and leaves the row detail in `rowDetail()`. The invariant the code states outright survives unchanged.

## File Structure

| File | Responsibility |
|---|---|
| `internal/pulls/pulls.go` (create) | `Pull`, `List`, `State`, the two state chains, the orthogonal marks, `Stacked`. |
| `internal/pulls/set.go` (create) | `Set` — the two lists in their drawing order, the unresolved rows, the stack-parent join. |
| `internal/pulls/fetch.go` (create) | `Fetcher` — the `gh api graphql` subprocess, both queries, and the three troubles told apart. |
| `internal/pulls/cycle.go` (create) | `Watcher` — the two-pass cycle, the two clocks, and refresh by hand. |
| `internal/pulls/match.go` (create) | `Origins` — `git remote get-url origin`, cached per process — and the checkout-to-Pull rule. |
| `internal/pulls/*_test.go` (create) | All of the above, against fake `gh` scripts and table-driven state cases. |
| `internal/dashboard/pulls.go` (create) | The section: entries, the measured columns, the row, the window, the chrome line, the four bodies. |
| `internal/dashboard/pulls_test.go` (create) | The section's rendering, against the mock's own case list. |
| `internal/dashboard/dashboard.go` (modify) | `pullsSection` on the Model, `foot()`, the keys, the `◆` mark, the `Pulls` hand. |
| `internal/dashboard/rows.go` (modify) | `row.yourMove`, and the answer that fills it. |
| `internal/tmuxconf/tmuxconf.go` (modify) | `p pulls` in `legendKeys`; `o open ticket` becomes `o open`. |
| `cmd/ganymede/main.go` (modify) | Wires the watcher and the refresh hand into the Dashboard. |
| `README.md` (modify) | The Prerequisites table gains a `gh` row. |
| every `*.go` (modify) | The `panel` comment sweep: 68 `panel`/`panels`, 13 `panelLines`. |

Task order is bottom-up. The state model, the fetch and the match are testable with no Dashboard at all; the rendering is a pure function of the fetched set; the Dashboard only changes once there is something for it to draw.

---

### Task 1: The Pull and the two state chains

**Files:**
- Create: `internal/pulls/pulls.go`
- Test: `internal/pulls/pulls_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pulls.Pull` (the struct below, all fields exported), `pulls.List` with `pulls.Authored`/`pulls.Requested`, `pulls.State` with the eight words and `pulls.Unresolved` (the empty string), `Pull.State() State`, `Pull.YourMove() bool`, `Pull.Resolved() bool`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the implementation**

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/ -run 'State|YourMove|Stateless' -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/pulls.go internal/pulls/pulls_test.go
git commit -m "Give a Pull the chain that says whose move it is" \
  -m "Two chains rather than one table: Draft outranks everything in both lists, Rework outranks Conflicted because a reviewer asking for changes names your next move before the rebase does, and Landable is withheld on a non-default base — CLEAN into an unmerged parent is not the press you want. A row whose test merge never landed falls out with no word at all rather than through to Sent, which would turn the three states meaning Your move into the one meaning the opposite." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The orthogonal marks and a stacked Pull

**Files:**
- Modify: `internal/pulls/pulls.go`
- Test: `internal/pulls/pulls_test.go`

**Interfaces:**
- Consumes: `Pull`, `State`, `Authored`, `Requested` from Task 1.
- Produces: `Pull.Failing() bool`, `Pull.Carries(APPROVAL) bool` — specifically `Pull.Approved() bool` and `Pull.Stacked() bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestOnlyAnExceptionalCheckIsMarked(t *testing.T) {
	// Drawn literally this is a mark on 13 of 17 rows, 6 of one day's 11 being
	// the pending clock alone. Both PENDING and SUCCESS are resting states, so
	// only a failure earns the column — which frees the tick to mean exactly
	// one thing.
	for checks, want := range map[string]bool{
		"FAILURE": true, "ERROR": true,
		"SUCCESS": false, "PENDING": false, "EXPECTED": false, "": false,
	} {
		p := Pull{List: Authored, Checks: checks}
		if got := p.Failing(); got != want {
			t.Errorf("checks %q: Failing() = %v, want %v", checks, got, want)
		}
	}
}

func TestTheTickIsAnApprovalAPersonGave(t *testing.T) {
	approved := Pull{List: Authored, Review: "APPROVED"}
	if !approved.Approved() {
		t.Error("an approved authored Pull carries no tick")
	}
	// In REQUESTED the approval is already the state word, so a tick beside it
	// would say the same thing twice.
	inRequested := Pull{List: Requested, Review: "APPROVED"}
	if inRequested.Approved() {
		t.Error("a requested Pull carries a tick as well as the word Approved")
	}
	if (Pull{List: Authored, Review: "REVIEW_REQUIRED"}).Approved() {
		t.Error("an unapproved Pull carries a tick")
	}
}

func TestTheMarksAreOrthogonalToTheState(t *testing.T) {
	// The one real collision in the measured data, and it is not a contest:
	// approved with the checks failing is Sent carrying both marks.
	p := Pull{
		List: Authored, Mergeable: "MERGEABLE", MergeState: "BLOCKED",
		Review: "APPROVED", Checks: "FAILURE", Base: "master", Default: "master",
	}
	if got := p.State(); got != Sent {
		t.Errorf("state: got %q, want %q", got, Sent)
	}
	if !p.Approved() || !p.Failing() {
		t.Errorf("marks: approved=%v failing=%v, want both", p.Approved(), p.Failing())
	}
	// And a red check never promotes the row: it is your PR's problem, not a
	// decision waiting on you.
	if p.YourMove() {
		t.Error("a failing check made the row Your move")
	}
}

func TestAPullOnSomethingOtherThanTheDefaultBranchIsStacked(t *testing.T) {
	onDefault := Pull{Base: "master", Default: "master"}
	if onDefault.Stacked() {
		t.Error("a Pull based on the default branch reports itself stacked")
	}
	onParent := Pull{Base: "PHX-4335-decode-search-term-for-subscription-title", Default: "master"}
	if !onParent.Stacked() {
		t.Error("a Pull based on another branch does not report itself stacked")
	}
	// A repository whose default branch the query could not read must not turn
	// every row in it into a stack.
	unknown := Pull{Base: "master", Default: ""}
	if unknown.Stacked() {
		t.Error("a Pull whose default branch is unknown reports itself stacked")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run 'Marked|Tick|Orthogonal|Stacked'`
Expected: FAIL — `p.Failing undefined`, `p.Approved undefined`, `p.Stacked undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/pulls/pulls.go`:

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/pulls.go internal/pulls/pulls_test.go
git commit -m "Draw a check mark only where the checks are exceptional" \
  -m "PENDING and SUCCESS are both resting states, so the three rollup marks land on 13 of 17 rows and the eye stops reading them. Only a failure earns a column, which frees the tick to mean the approval a person gave — and that is what settles a Pull approved with its checks failing: not a contest between two marks but Sent carrying both." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The Set — two lists in their order, and the stack parent

**Files:**
- Create: `internal/pulls/set.go`
- Test: `internal/pulls/set_test.go`

**Interfaces:**
- Consumes: `Pull`, `List`, `Authored`, `Requested`, `Pull.Resolved`, `Pull.Stacked` from Tasks 1–2.
- Produces: `pulls.Set []Pull`, `Set.Of(l List) []Pull` (sorted by repo then number), `Set.Unresolved() []Pull`, `Set.ParentOf(p Pull) (Pull, bool)`.

- [ ] **Step 1: Write the failing test**

```go
package pulls

import "testing"

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
```

Add the two helpers the tests use to `set_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run Set`
Expected: FAIL — `Set` undefined.

- [ ] **Step 3: Write the implementation**

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/set.go internal/pulls/set_test.go
git commit -m "Order the two lists, and join a stack to its parent locally" \
  -m "Sorting by repo then number keeps a stack contiguous, which is what lets the stack mark stay bare: the parent is the row directly above it three times in four, and naming it would cost seven columns off every row rather than off the four that are stacked. The join reads the other rows' head branches and is scoped to the repository, since five of fifteen Pulls shared a head branch across repos." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: The fetch — one `gh api graphql` subprocess, and the three troubles

**Files:**
- Create: `internal/pulls/fetch.go`
- Test: `internal/pulls/fetch_test.go`

**Interfaces:**
- Consumes: `Pull`, `Set`, `List`, `Authored`, `Requested` from Tasks 1–3.
- Produces: `pulls.Fetcher{GH string, Timeout time.Duration, First int}`, `Fetcher.Fetch(ctx context.Context) (Set, error)`, `pulls.Trouble` with `pulls.NotLoggedIn`/`pulls.Unauthorized`/`pulls.Unreachable`, `pulls.Error{Trouble Trouble, Said string, Err error}` implementing `error`, `pulls.TroubleOf(err error) (Trouble, bool)`.

**Why `gh` and not an HTTP client:** the deciding argument is the failure modes. Three outcomes have to be told apart and they are `gh`'s own diagnosis — never logged in is exit 4, an expired or revoked token is exit 1 with `HTTP 401` on stderr, and everything else is the network. A raw 401 from a client of our own cannot tell a revoked token from a changed scope. It is also why the harness holds no credential: it inherits the login you have already granted.

- [ ] **Step 1: Write the failing test**

```go
package pulls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH writes a script standing in for gh: it prints body on stdout, said on
// stderr, and exits with code. The arguments it was called with are written
// beside it, so a test can assert on the query that was actually sent.
func fakeGH(t *testing.T, body, said string, code int) (gh, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	gh = filepath.Join(dir, "gh")
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$*\" > " + argsFile + "\n" +
		"cat >> " + argsFile + "\n" +
		"printf '%s' " + shellQuote(body) + "\n" +
		"printf '%s' " + shellQuote(said) + " >&2\n" +
		"exit " + itoa(code) + "\n"
	if err := os.WriteFile(gh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return gh, argsFile
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const twoLists = `{"data":{
  "rateLimit":{"cost":1,"remaining":4993},
  "review":{"issueCount":1,"nodes":[
    {"number":1564,"title":"[PWR-237] Document the endpoint","url":"https://github.com/teamleadercrm/api-internal/pull/1564",
     "isDraft":false,"baseRefName":"master","headRefName":"PWR-237/document","updatedAt":"2026-09-22T06:11:03Z",
     "mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":"REVIEW_REQUIRED",
     "author":{"login":"a-colleague"},
     "repository":{"nameWithOwner":"teamleadercrm/api-internal","defaultBranchRef":{"name":"master"}},
     "statusCheckRollup":{"state":"PENDING"}}]},
  "mine":{"issueCount":1,"nodes":[
    {"number":1010,"title":"[FIRE-3137] Read the batch size","url":"https://github.com/teamleadercrm/focus-service-bookkeeping/pull/1010",
     "isDraft":true,"baseRefName":"master","headRefName":"FIRE-3137/batch-size","updatedAt":"2026-09-22T05:02:00Z",
     "mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":"REVIEW_REQUIRED",
     "author":{"login":"BrechtBonte"},
     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
     "statusCheckRollup":null}]}}}`

func TestAFetchReadsBothListsFromOneRequest(t *testing.T) {
	gh, argsFile := fakeGH(t, twoLists, "", 0)
	set, err := Fetcher{GH: gh}.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(set) != 2 {
		t.Fatalf("got %d Pulls, want 2", len(set))
	}

	mine := set.Of(Authored)
	if len(mine) != 1 || mine[0].Number != 1010 || !mine[0].IsDraft {
		t.Errorf("AUTHORED: got %+v", mine)
	}
	if mine[0].Repo != "teamleadercrm/focus-service-bookkeeping" || mine[0].Default != "master" {
		t.Errorf("AUTHORED identity: got %q base %q", mine[0].Repo, mine[0].Default)
	}
	// A PR with no checks at all comes back as a null rollup, which must read
	// as "no checks" rather than panicking on the way in.
	if mine[0].Checks != "" {
		t.Errorf("a null rollup read as %q", mine[0].Checks)
	}
	review := set.Of(Requested)
	if len(review) != 1 || review[0].Number != 1564 || review[0].Checks != "PENDING" {
		t.Errorf("REQUESTED: got %+v", review)
	}
	if review[0].URL == "" || review[0].Author != "a-colleague" {
		t.Errorf("REQUESTED fields: url=%q author=%q", review[0].URL, review[0].Author)
	}

	sent, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	query := string(sent)
	// The qualifier inverts the obvious guess and getting it wrong would be
	// invisible: every human review request on this account arrives as a team
	// request, which only the broad qualifier matches.
	for _, want := range []string{
		"is:pr is:open review-requested:@me",
		"is:pr is:open author:@me",
		"defaultBranchRef",
		"mergeStateStatus",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("the request did not carry %q\n%s", want, query)
		}
	}
	if strings.Contains(query, "user-review-requested") {
		t.Error("the request used the narrow qualifier, which matches only dependabot here")
	}
	if strings.Contains(query, "assignee:@me") {
		t.Error("the request asked for assignee:@me, which returns the same rows as author:@me")
	}
}

func TestTheThreeWaysAFetchFails(t *testing.T) {
	for _, c := range []struct {
		says string
		said string
		code int
		want Trouble
	}{
		{"never logged in is gh's own exit code", "", 4, NotLoggedIn},
		{"an expired or revoked token says so on stderr", "gh: HTTP 401: Bad credentials", 1, Unauthorized},
		{"anything else is the network", "dial tcp: lookup api.github.com: no such host", 1, Unreachable},
	} {
		gh, _ := fakeGH(t, "", c.said, c.code)
		_, err := Fetcher{GH: gh}.Fetch(context.Background())
		if err == nil {
			t.Fatalf("%s: fetch succeeded", c.says)
		}
		got, ok := TroubleOf(err)
		if !ok || got != c.want {
			t.Errorf("%s: got %v (%v), want %v", c.says, got, ok, c.want)
		}
	}
}

func TestAMissingGHIsTheLoginYouHaveNotMade(t *testing.T) {
	// A machine with the harness and no gh runs today. Once the section ships
	// gh is a hard runtime dependency, and the body that names `gh auth login`
	// is the right one to send a reader to.
	_, err := Fetcher{GH: "/nonexistent/gh"}.Fetch(context.Background())
	got, ok := TroubleOf(err)
	if !ok || got != NotLoggedIn {
		t.Errorf("got %v (%v), want NotLoggedIn", got, ok)
	}
}

func TestAnAnswerThatIsNotJSONIsUnreachableRatherThanAPanic(t *testing.T) {
	gh, _ := fakeGH(t, "<html>502 Bad Gateway</html>", "", 0)
	_, err := Fetcher{GH: gh}.Fetch(context.Background())
	got, ok := TroubleOf(err)
	if !ok || got != Unreachable {
		t.Errorf("got %v (%v), want Unreachable", got, ok)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run Fetch`
Expected: FAIL — `Fetcher` undefined.

- [ ] **Step 3: Write the implementation**

```go
package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout is how long one pass may take. Measured at 4.4–5.6 s for 17
// PRs carrying every field, against a gateway that gave up at about ten on a
// ten-repo query — so this is generous next to the real thing and short next
// to the thirty minutes until the next cycle.
const defaultTimeout = 30 * time.Second

// defaultFirst is the page size. Seventeen was the measured census and twenty
// is the next round number above it; a set that outgrows this loses its tail
// rather than its section.
const defaultFirst = 20

// reviewQuery and mineQuery are the two searches, as GitHub's own qualifiers.
//
// review-requested rather than user-review-requested, which inverts the obvious
// guess: measured, the broad qualifier returns 8 — three human requests and
// five dependabot — where the narrow one returns 5, all five dependabot. Every
// human review request on this account arrives as a team request, so the
// narrower-looking qualifier builds a section that hides every person asking
// you for something.
//
// There is no assignee:@me. It returns 7 PRs and they are the same 7 author:@me
// returns, every one authored by you: GitHub's PR assignee carries no
// independent meaning in this workflow.
const (
	reviewQuery = "is:pr is:open review-requested:@me"
	mineQuery   = "is:pr is:open author:@me"
)

// fields is what one PR is read for, shared by both passes so the two can
// never drift into disagreeing about what a row holds.
//
// mergeStateStatus is the expensive one and is kept: it is the only source for
// Behind and the only exact source for Landable, and at this section's scale
// the whole query with it included is under six seconds. defaultBranchRef is
// what withholds Landable from a stacked Pull. headRefName is free — measured
// cost: 1 unchanged — and pays for the stack parent and the Session match.
const fields = `
  number title url isDraft baseRefName headRefName updatedAt
  mergeable mergeStateStatus reviewDecision
  author { login }
  repository { nameWithOwner defaultBranchRef { name } }
  statusCheckRollup { state }`

// searchQuery carries both lists as aliased search fields, which is what makes
// a whole pass one HTTP request for one rate-limit point.
const searchQuery = `query($review: String!, $mine: String!, $first: Int!) {
  rateLimit { cost remaining }
  review: search(query: $review, type: ISSUE, first: $first) {
    issueCount
    nodes { ... on PullRequest {` + fields + ` } }
  }
  mine: search(query: $mine, type: ISSUE, first: $first) {
    issueCount
    nodes { ... on PullRequest {` + fields + ` } }
  }
}`

// Trouble is why a fetch did not land. The three are told apart because they
// want three different renderings and two different clocks, and gh already
// diagnoses all three — which is the argument for running it rather than
// holding a token of our own.
type Trouble int

const (
	// NotLoggedIn is gh exit 4, and a gh that is not installed at all: both
	// end with you running `gh auth login`, or installing gh first.
	NotLoggedIn Trouble = iota
	// Unauthorized is exit 1 carrying HTTP 401 — a token expired or revoked.
	Unauthorized
	// Unreachable is everything else, which is the network.
	Unreachable
)

// Error is a fetch that did not land, carrying which of the three it was and
// whatever gh had to say for itself.
type Error struct {
	Trouble Trouble
	// Said is gh's stderr, trimmed. It is kept for a diagnostic rather than
	// for the section, which says its own four things in its own words.
	Said string
	Err  error
}

func (e *Error) Error() string {
	if e.Said != "" {
		return fmt.Sprintf("read your pull requests: %v: %s", e.Err, e.Said)
	}
	return fmt.Sprintf("read your pull requests: %v", e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// TroubleOf is which of the three an error is, and whether it is one of them
// at all.
func TroubleOf(err error) (Trouble, bool) {
	var trouble *Error
	if errors.As(err, &trouble) {
		return trouble.Trouble, true
	}
	return 0, false
}

// Fetcher runs one pass of the census through gh.
//
// gh is a subprocess rather than an HTTP client of our own, and that is a
// decision about failure modes rather than about convenience: the three
// outcomes above are gh's own diagnosis, and a raw 401 from a client we wrote
// cannot tell a revoked token from a changed scope. It is also why the harness
// holds no credential at all — it inherits the login you have already granted.
type Fetcher struct {
	// GH is the gh binary to run. Empty means the gh on PATH.
	GH string
	// Timeout is how long one pass may take. Zero means the default.
	Timeout time.Duration
	// First is the page size for each list. Zero means the default.
	First int
}

// Fetch reads both lists in one request.
func (f Fetcher) Fetch(ctx context.Context) (Set, error) {
	var answer struct {
		Data struct {
			Review searchResult `json:"review"`
			Mine   searchResult `json:"mine"`
		} `json:"data"`
	}
	err := f.ask(ctx, &answer,
		"-f", "query="+searchQuery,
		"-f", "review="+reviewQuery,
		"-f", "mine="+mineQuery,
		"-F", "first="+strconv.Itoa(f.first()),
	)
	if err != nil {
		return nil, err
	}

	set := make(Set, 0, len(answer.Data.Mine.Nodes)+len(answer.Data.Review.Nodes))
	for _, node := range answer.Data.Mine.Nodes {
		set = append(set, node.pull(Authored))
	}
	for _, node := range answer.Data.Review.Nodes {
		set = append(set, node.pull(Requested))
	}
	return set, nil
}

// ask runs gh and decodes what it wrote, turning every way of not getting an
// answer into one of the three troubles.
func (f Fetcher) ask(ctx context.Context, into any, args ...string) error {
	ctx, giveUp := context.WithTimeout(ctx, f.timeout())
	defer giveUp()

	var out, said strings.Builder
	cmd := exec.CommandContext(ctx, f.gh(), append([]string{"api", "graphql"}, args...)...)
	cmd.Stdout = &out
	cmd.Stderr = &said
	err := cmd.Run()
	complaint := strings.TrimSpace(said.String())
	if err != nil {
		return &Error{Trouble: troubleOf(err, complaint), Said: complaint, Err: err}
	}
	if err := json.Unmarshal([]byte(out.String()), into); err != nil {
		// An answer that is not JSON is a gateway or a proxy talking, not
		// GitHub: the same thing a dropped connection is, and the same
		// rendering.
		return &Error{Trouble: Unreachable, Said: complaint, Err: err}
	}
	return nil
}

// troubleOf reads gh's own diagnosis. Exit 4 is the login never made; exit 1
// carrying a 401 is a token that has expired or been revoked; everything else
// is the network.
//
// A gh that could not be started at all lands on NotLoggedIn rather than on
// Unreachable, because the body that names `gh auth login` is the one that
// sends a reader somewhere useful — installing gh is the same errand.
func troubleOf(err error, said string) Trouble {
	var started *exec.Error
	if errors.As(err, &started) {
		return NotLoggedIn
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		if exited.ExitCode() == 4 {
			return NotLoggedIn
		}
		if strings.Contains(said, "HTTP 401") {
			return Unauthorized
		}
	}
	return Unreachable
}

func (f Fetcher) gh() string {
	if f.GH == "" {
		return "gh"
	}
	return f.GH
}

func (f Fetcher) timeout() time.Duration {
	if f.Timeout <= 0 {
		return defaultTimeout
	}
	return f.Timeout
}

func (f Fetcher) first() int {
	if f.First <= 0 {
		return defaultFirst
	}
	return f.First
}

// searchResult is one aliased search field.
type searchResult struct {
	IssueCount int    `json:"issueCount"`
	Nodes      []node `json:"nodes"`
}

// node is one PullRequest as the query asks for it. The nested shapes are
// pointers because GitHub sends null for every one of them in some real case —
// a PR with no checks, a repository whose default branch the token cannot see,
// a ghost author.
type node struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	IsDraft    bool      `json:"isDraft"`
	Base       string    `json:"baseRefName"`
	Head       string    `json:"headRefName"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Mergeable  string    `json:"mergeable"`
	MergeState string    `json:"mergeStateStatus"`
	Review     string    `json:"reviewDecision"`
	Author     *struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
		DefaultBranch *struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	} `json:"repository"`
	Rollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (n node) pull(list List) Pull {
	p := Pull{
		List: list, Number: n.Number, Title: n.Title, URL: n.URL,
		IsDraft: n.IsDraft, Base: n.Base, Head: n.Head, UpdatedAt: n.UpdatedAt,
		Mergeable: n.Mergeable, MergeState: n.MergeState, Review: n.Review,
	}
	if n.Author != nil {
		p.Author = n.Author.Login
	}
	if n.Repository != nil {
		p.Repo = n.Repository.NameWithOwner
		if n.Repository.DefaultBranch != nil {
			p.Default = n.Repository.DefaultBranch.Name
		}
	}
	if n.Rollup != nil {
		p.Checks = n.Rollup.State
	}
	return p
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/ -v`
Expected: PASS, every test.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/fetch.go internal/pulls/fetch_test.go
git commit -m "Read both lists through gh in one request for one point" \
  -m "gh as a subprocess rather than an HTTP client of our own, decided on the failure modes: never logged in is exit 4, an expired token is exit 1 carrying HTTP 401, and anything else is the network — three outcomes gh already diagnoses and a raw 401 from a client we wrote could not. It is also why the harness holds no credential; it inherits the login you have granted." \
  -m "The qualifier is review-requested:@me and never user-review-requested:@me, which inverts the obvious guess: the narrow one returned five rows on this account and all five were dependabot, because every human request here arrives as a team request. defaultBranchRef joins the field set, which the design's query block omitted — Landable is withheld on a non-default base and cannot be decided without it." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: The second pass — re-reading only the rows that came back UNKNOWN

**Files:**
- Modify: `internal/pulls/fetch.go`
- Test: `internal/pulls/fetch_test.go`

**Interfaces:**
- Consumes: `Fetcher`, `Fetcher.ask`, `node`, `Set`, `Set.Unresolved` from Tasks 3–4.
- Produces: `Fetcher.Reread(ctx context.Context, rows []Pull) ([]Pull, error)` — returns the same rows with their mergeability fields refreshed, in the order given.

- [ ] **Step 1: Write the failing test**

```go
func TestASecondPassAsksOnlyAboutTheRowsItWasGiven(t *testing.T) {
	const reread = `{"data":{
	  "rateLimit":{"cost":1,"remaining":4992},
	  "p0":{"pullRequest":{"number":979,"title":"Bump guzzle","url":"https://github.com/teamleadercrm/focus-service-bookkeeping/pull/979",
	     "isDraft":false,"baseRefName":"master","headRefName":"dependabot/composer/guzzle-8.2.0","updatedAt":"2026-09-22T06:00:00Z",
	     "mergeable":"MERGEABLE","mergeStateStatus":"BEHIND","reviewDecision":"REVIEW_REQUIRED",
	     "author":{"login":"dependabot"},
	     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
	     "statusCheckRollup":{"state":"FAILURE"}}}}}`

	gh, argsFile := fakeGH(t, reread, "", 0)
	unknown := Pull{
		List: Authored, Repo: "teamleadercrm/focus-service-bookkeeping", Number: 979,
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}

	rows, err := Fetcher{GH: gh}.Reread(context.Background(), []Pull{unknown})
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// The resolved fields land, and the row keeps the list it was already in —
	// the second pass reads a repository, which does not know which search
	// found the row.
	if rows[0].MergeState != "BEHIND" || rows[0].Mergeable != "MERGEABLE" {
		t.Errorf("mergeability did not resolve: %+v", rows[0])
	}
	if rows[0].List != Authored {
		t.Error("the re-read row lost its list")
	}
	if got := rows[0].State(); got != Behind {
		t.Errorf("state after the second pass: got %q, want %q", got, Behind)
	}

	sent, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	query := string(sent)
	for _, want := range []string{"focus-service-bookkeeping", "979", "pullRequest"} {
		if !strings.Contains(query, want) {
			t.Errorf("the second pass did not ask about %q\n%s", want, query)
		}
	}
	// It is a re-read of named rows, not the whole census again.
	if strings.Contains(query, "review-requested:@me") || strings.Contains(query, "author:@me") {
		t.Error("the second pass ran the search again instead of re-reading the rows")
	}
}

func TestASecondPassWithNothingToAskCostsNoRequest(t *testing.T) {
	// A gh that would fail if it ran at all, so a request proves itself.
	rows, err := Fetcher{GH: "/nonexistent/gh"}.Reread(context.Background(), nil)
	if err != nil {
		t.Fatalf("reread of nothing: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want none", len(rows))
	}
}

func TestARowThatNeverResolvesKeepsEverythingButItsState(t *testing.T) {
	const stillUnknown = `{"data":{"rateLimit":{"cost":1,"remaining":4991},
	  "p0":{"pullRequest":{"number":979,"title":"Bump guzzle","url":"https://example.invalid/979",
	     "isDraft":false,"baseRefName":"master","headRefName":"dependabot/composer/guzzle-8.2.0","updatedAt":"2026-09-22T06:00:00Z",
	     "mergeable":"UNKNOWN","mergeStateStatus":"UNKNOWN","reviewDecision":"REVIEW_REQUIRED",
	     "author":{"login":"dependabot"},
	     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
	     "statusCheckRollup":{"state":"FAILURE"}}}}}`

	gh, _ := fakeGH(t, stillUnknown, "", 0)
	unknown := Pull{
		List: Authored, Repo: "teamleadercrm/focus-service-bookkeeping", Number: 979,
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Base: "master", Default: "master",
	}
	rows, err := Fetcher{GH: gh}.Reread(context.Background(), []Pull{unknown})
	if err != nil {
		t.Fatal(err)
	}
	// It keeps its number, repo and marks — all from fields that never go
	// UNKNOWN — and draws no state word. There is no third pass: the next
	// cycle picks it up, and the row is honest meanwhile.
	if got := rows[0].State(); got != Unresolved {
		t.Errorf("got %q, want no state", got)
	}
	if !rows[0].Failing() || rows[0].Number != 979 {
		t.Errorf("a stateless row lost its marks or its identity: %+v", rows[0])
	}
	if rows[0].YourMove() {
		t.Error("a stateless row is Your move")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run SecondPass`
Expected: FAIL — `Reread` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/pulls/fetch.go`:

```go
// Reread asks about exactly the rows it is given, and nothing else.
//
// GitHub computes mergeability on a background test-merge commit and serves
// UNKNOWN until it lands. The commit is invalidated whenever the base branch
// moves, so a thirty-minute poll arrives cold every time — measured, one active
// repository went from 0/30 UNKNOWN to 14/30 in twenty minutes, and a re-read
// five seconds later resolved 30/30.
//
// It is one aliased request over the named rows rather than the search run
// again: the search would re-read every row to correct a handful, and would
// also let the set change shape between the two passes of one cycle.
//
// The rows come back in the order they were given, carrying the list they
// arrived in — the second pass reads repositories, which do not know which
// search found a row.
func (f Fetcher) Reread(ctx context.Context, rows []Pull) ([]Pull, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	var query strings.Builder
	query.WriteString("query {\n  rateLimit { cost remaining }\n")
	for i, p := range rows {
		owner, name, ok := strings.Cut(p.Repo, "/")
		if !ok {
			continue
		}
		fmt.Fprintf(&query, "  p%d: repository(owner: %q, name: %q) { pullRequest(number: %d) {%s } }\n",
			i, owner, name, p.Number, fields)
	}
	query.WriteString("}")

	var answer struct {
		Data map[string]struct {
			PullRequest *node `json:"pullRequest"`
		} `json:"data"`
	}
	if err := f.ask(ctx, &answer, "-f", "query="+query.String()); err != nil {
		return nil, err
	}

	read := make([]Pull, len(rows))
	for i, p := range rows {
		read[i] = p
		found, ok := answer.Data["p"+strconv.Itoa(i)]
		if !ok || found.PullRequest == nil {
			// A row the re-read could not see keeps what the first pass said
			// about it, which is an unresolved row — the honest answer, and
			// the one the next cycle corrects.
			continue
		}
		fresh := found.PullRequest.pull(p.List)
		read[i] = fresh
	}
	return read, nil
}
```

> The `rateLimit` field is asked for in both passes and nothing reads it. It is
> there because it is what proves the cost claim — `cost: 1` per pass, 4 points
> an hour against 5,000 — and because a query that stops asking is one nobody
> can check.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/fetch.go internal/pulls/fetch_test.go
git commit -m "Re-read only the rows whose test merge had not landed" \
  -m "GitHub invalidates the background test-merge commit whenever the base branch moves, so a thirty-minute poll arrives cold: one active repository went from 0/30 UNKNOWN to 14/30 in twenty minutes, and a re-read five seconds later resolved every one. The second pass names the rows rather than running the search again, which would re-read everything to correct a handful and let the set change shape between one cycle's two passes." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: The cycle and its two clocks

**Files:**
- Create: `internal/pulls/cycle.go`
- Test: `internal/pulls/cycle_test.go`

**Interfaces:**
- Consumes: `Fetcher.Fetch`, `Fetcher.Reread`, `Set`, `Set.Unresolved`, `Trouble`, `TroubleOf` from Tasks 3–5.
- Produces: `pulls.Report{Set Set, At time.Time, Err error}`, `pulls.Reader` (the interface `Watcher` fetches through), `pulls.Watcher{Read Reader, Every, Retry, Settle time.Duration, Now func() time.Time}`, `Watcher.Cycle(ctx) Report`, `Watcher.Watch(ctx context.Context, refresh <-chan struct{}) <-chan Report`, `pulls.Refresher` with `NewRefresher() *Refresher`, `(*Refresher).Refresh() bool`, `(*Refresher).Asked() <-chan struct{}`.

- [ ] **Step 1: Write the failing test**

```go
package pulls

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reader is a Reader whose two passes are scripted, and which records what it
// was asked.
type reader struct {
	sets     []Set
	fetchErr []error
	rereads  [][]Pull
	fetches  int
	rereadsN int
}

func (r *reader) Fetch(context.Context) (Set, error) {
	i := min(r.fetches, len(r.sets)-1)
	r.fetches++
	if i < len(r.fetchErr) && r.fetchErr[i] != nil {
		return nil, r.fetchErr[i]
	}
	return r.sets[i], nil
}

func (r *reader) Reread(_ context.Context, rows []Pull) ([]Pull, error) {
	r.rereads = append(r.rereads, rows)
	r.rereadsN++
	read := make([]Pull, len(rows))
	for i, p := range rows {
		p.Mergeable, p.MergeState = "MERGEABLE", "BEHIND"
		read[i] = p
	}
	return read, nil
}

func unresolved(number int) Pull {
	return Pull{
		List: Authored, Repo: "teamleadercrm/core", Number: number, Head: "head",
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}
}

func resolved(number int) Pull {
	p := unresolved(number)
	p.Mergeable, p.MergeState = "MERGEABLE", "BLOCKED"
	return p
}

func TestACycleIsTwoPassesAndOneAnswer(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1), unresolved(2)}}}
	w := Watcher{Read: read, Settle: time.Millisecond}

	report := w.Cycle(context.Background())
	if report.Err != nil {
		t.Fatalf("cycle: %v", report.Err)
	}
	if read.fetches != 1 || read.rereadsN != 1 {
		t.Errorf("passes: %d fetches, %d rereads, want 1 and 1", read.fetches, read.rereadsN)
	}
	// Only the unresolved row is re-read.
	if len(read.rereads[0]) != 1 || read.rereads[0][0].Number != 2 {
		t.Errorf("the second pass asked about %v, want only 2", read.rereads[0])
	}
	// One answer, both passes merged into it — never a provisional paint
	// followed by a settled one.
	if len(report.Set) != 2 {
		t.Fatalf("got %d rows, want 2", len(report.Set))
	}
	for _, p := range report.Set {
		if p.Number == 2 && p.State() != Behind {
			t.Errorf("the re-read row did not land: %q", p.State())
		}
		if p.Number == 1 && p.State() != Sent {
			t.Errorf("the resolved row changed under the second pass: %q", p.State())
		}
	}
	if report.At.IsZero() {
		t.Error("the report carries no time, so the chrome line has nothing to draw")
	}
}

func TestACycleWithNothingUnresolvedMakesOnePass(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1)}}}
	if report := (Watcher{Read: read, Settle: time.Millisecond}).Cycle(context.Background()); report.Err != nil {
		t.Fatal(report.Err)
	}
	if read.rereadsN != 0 {
		t.Errorf("made %d second passes over a fully resolved set, want none", read.rereadsN)
	}
}

func TestAFailedFirstPassIsReportedRatherThanRetriedInsideTheCycle(t *testing.T) {
	boom := &Error{Trouble: Unreachable, Err: errors.New("no route to host")}
	read := &reader{sets: []Set{nil}, fetchErr: []error{boom}}
	report := (Watcher{Read: read, Settle: time.Millisecond}).Cycle(context.Background())
	if report.Err == nil {
		t.Fatal("a failed pass reported no error")
	}
	if got, _ := TroubleOf(report.Err); got != Unreachable {
		t.Errorf("got trouble %v, want Unreachable", got)
	}
	if read.rereadsN != 0 {
		t.Error("a failed first pass still ran a second")
	}
}

func TestANetworkFailureRetriesSoonerThanTheWindow(t *testing.T) {
	// The instinct behind release's 30-minute Retry against a 10-hour Every,
	// scaled: five minutes rather than waiting out the whole window.
	w := Watcher{Every: 30 * time.Minute, Retry: 5 * time.Minute}
	for _, c := range []struct {
		says string
		err  error
		want time.Duration
	}{
		{"a landed cycle waits the window", nil, 30 * time.Minute},
		{"the network is retried sooner", &Error{Trouble: Unreachable}, 5 * time.Minute},
		// Stopping would strand the section until a Dashboard restart, where
		// thirty minutes costs nothing and lets a gh auth login in another
		// pane heal it without one.
		{"an expired login keeps polling at the window", &Error{Trouble: Unauthorized}, 30 * time.Minute},
		{"a login never made keeps polling too", &Error{Trouble: NotLoggedIn}, 30 * time.Minute},
	} {
		if got := w.until(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.says, got, c.want)
		}
	}
}

func TestTheWatchReportsEveryCycleAndAnswersARefresh(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1)}, {resolved(1), resolved(2)}}}
	// A window long enough that only the refresh can produce the second
	// report, so the test is about the key rather than about a clock.
	w := Watcher{Read: read, Every: time.Hour, Retry: time.Hour, Settle: time.Millisecond}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	asks := NewRefresher()
	reports := w.Watch(ctx, asks.Asked())

	first := <-reports
	if len(first.Set) != 1 {
		t.Fatalf("first cycle: got %d rows, want 1", len(first.Set))
	}
	if !asks.Refresh() {
		t.Fatal("a refresh between cycles was refused")
	}
	second := <-reports
	if len(second.Set) != 2 {
		t.Errorf("after r: got %d rows, want 2", len(second.Set))
	}

	stop()
	if _, open := <-reports; open {
		// Drain whatever was in flight, then the channel must close with the
		// watch.
		if _, open := <-reports; open {
			t.Error("the reports channel outlived the context")
		}
	}
}

func TestHoldingRefreshCannotStackCycles(t *testing.T) {
	asks := NewRefresher()
	if !asks.Refresh() {
		t.Fatal("the first ask was refused")
	}
	// A second ask with the first still unanswered is a no-op: holding r
	// cannot stack twelve-second cycles behind each other.
	if asks.Refresh() {
		t.Error("a second ask was accepted while the first was outstanding")
	}
	<-asks.Asked()
	if !asks.Refresh() {
		t.Error("an ask after the first was taken was refused")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run 'Cycle|Watch|Refresh|Network'`
Expected: FAIL — `Watcher` undefined.

- [ ] **Step 3: Write the implementation**

```go
package pulls

import (
	"context"
	"time"
)

// defaultEvery is how long between cycles. The rate cost is 4 points an hour
// against 5,000 — 0.08% — so this is not politeness to a budget; it is the
// pace at which a census of your own pull requests actually changes.
const defaultEvery = 30 * time.Minute

// defaultRetry is how long before a cycle the network ate is tried again. The
// instinct behind release's 30-minute Retry against its 10-hour Every, scaled:
// a laptop opened on a train has no business waiting out the whole window
// after it finds a connection.
const defaultRetry = 5 * time.Minute

// defaultSettle is how long the cycle waits between its two passes. Measured:
// a re-read five seconds after a cold read resolved 30 of 30, and held at
// +10, +15 and +20 s — so five seconds is the knee rather than a guess.
const defaultSettle = 5 * time.Second

// Reader is one pass of the census, and the re-read of the rows it could not
// resolve. Fetcher is the one that talks to GitHub; a test supplies its own.
type Reader interface {
	Fetch(ctx context.Context) (Set, error)
	Reread(ctx context.Context, rows []Pull) ([]Pull, error)
}

// Report is one cycle's answer: the set, when it landed, and why it did not.
//
// A failed cycle carries no set rather than an empty one. The two are
// different claims — "you have none" against "I could not ask" — and the
// section renders them differently, which is the whole of why silence was
// ruled out as a rendering.
type Report struct {
	Set Set
	At  time.Time
	Err error
}

// Watcher runs a cycle now and again, and whenever you ask for one.
type Watcher struct {
	// Read is where the census comes from. Nil is a Watcher that reports
	// nothing, which is a harness that was never wired up.
	Read Reader
	// Every is how long between cycles. Zero means the default.
	Every time.Duration
	// Retry is how long before a cycle the network ate is tried again. Zero
	// means the default.
	Retry time.Duration
	// Settle is how long the cycle waits between its two passes. Zero means
	// the default.
	Settle time.Duration
	// Now is the clock, for a test that needs one of its own. Nil means
	// time.Now.
	Now func() time.Time
}

// Cycle is one whole answer: both lists, the rows that came back unresolved
// re-read after Settle, and one report.
//
// Atomic rather than progressive. A paint at six seconds followed by a settled
// one at twelve buys six seconds exactly once, at Dashboard start, and charges
// for it permanently — a provisional rendering seen for five seconds every half
// hour, and a section carrying two ages at once where the chrome line has to
// state one. In the steady state the twelve seconds are invisible, because the
// section spends them showing the previous cycle.
func (w Watcher) Cycle(ctx context.Context) Report {
	if w.Read == nil {
		return Report{At: w.now()}
	}

	set, err := w.Read.Fetch(ctx)
	if err != nil {
		return Report{At: w.now(), Err: err}
	}

	unresolved := set.Unresolved()
	if len(unresolved) == 0 {
		return Report{Set: set, At: w.now()}
	}

	select {
	case <-ctx.Done():
		return Report{Set: set, At: w.now()}
	case <-time.After(w.settle()):
	}

	// A second pass that could not be made is not a failed cycle. The first
	// pass is in hand and every row in it is honest — the unresolved ones
	// simply draw no state word, which is what they would have drawn anyway
	// had the re-read come back still UNKNOWN.
	read, err := w.Read.Reread(ctx, unresolved)
	if err != nil {
		return Report{Set: set, At: w.now()}
	}

	fresh := make(map[[2]any]Pull, len(read))
	for _, p := range read {
		fresh[[2]any{p.Repo, p.Number}] = p
	}
	merged := make(Set, len(set))
	for i, p := range set {
		if settled, ok := fresh[[2]any{p.Repo, p.Number}]; ok {
			merged[i] = settled
			continue
		}
		merged[i] = p
	}
	return Report{Set: merged, At: w.now()}
}

// Watch runs a cycle now, then every Every — sooner after a network failure,
// and at once whenever refresh carries an ask. The channel is closed when the
// watch stops.
//
// The clock runs whether or not the section is drawn. That is not a preference
// but a consequence of the Your-move mark: the mark lives on a Session row, the
// tree is always drawn, and collapsing Pulls stops it drawing rows rather than
// fetching them.
func (w Watcher) Watch(ctx context.Context, refresh <-chan struct{}) <-chan Report {
	reports := make(chan Report)
	go w.watch(ctx, refresh, reports)
	return reports
}

func (w Watcher) watch(ctx context.Context, refresh <-chan struct{}, reports chan<- Report) {
	defer close(reports)

	for {
		report := w.Cycle(ctx)
		select {
		case reports <- report:
		case <-ctx.Done():
			return
		}

		// Anything asked for while the cycle was running is discarded here
		// rather than queued: r is a no-op while a cycle is in flight, so
		// holding it cannot stack twelve-second cycles behind each other.
		select {
		case <-refresh:
		default:
		}

		due := time.NewTimer(w.until(report.Err))
		select {
		case <-ctx.Done():
			due.Stop()
			return
		case <-due.C:
		case <-refresh:
			due.Stop()
		}
	}
}

// until is how long before the next cycle, given how this one ended.
//
// A network failure retries at Retry. An auth failure does not: it drops back
// to the plain window and keeps going, so a `gh auth login` in another pane
// heals the section without a Dashboard restart. Stopping would be correct
// about gh never refreshing itself and wrong about what that costs.
func (w Watcher) until(err error) time.Duration {
	if trouble, ok := TroubleOf(err); ok && trouble == Unreachable {
		return w.retry()
	}
	return w.every()
}

func (w Watcher) every() time.Duration {
	if w.Every <= 0 {
		return defaultEvery
	}
	return w.Every
}

func (w Watcher) retry() time.Duration {
	if w.Retry <= 0 {
		return defaultRetry
	}
	return w.Retry
}

func (w Watcher) settle() time.Duration {
	if w.Settle <= 0 {
		return defaultSettle
	}
	return w.Settle
}

func (w Watcher) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

// Refresher is the hand `r` pulls: it asks for a cycle now, and says whether
// the ask landed.
//
// One outstanding ask at a time. The channel holds a single token, so a second
// press with the first still untaken is refused rather than queued — which is
// what makes holding the key a no-op rather than a way to stack cycles.
type Refresher struct{ asked chan struct{} }

// NewRefresher returns a Refresher with nothing outstanding.
func NewRefresher() *Refresher {
	return &Refresher{asked: make(chan struct{}, 1)}
}

// Refresh asks for a cycle, and reports whether this ask is the one that will
// be answered.
func (r *Refresher) Refresh() bool {
	if r == nil {
		return false
	}
	select {
	case r.asked <- struct{}{}:
		return true
	default:
		return false
	}
}

// Asked is what the watch selects on.
func (r *Refresher) Asked() <-chan struct{} { return r.asked }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/ -race`
Expected: PASS, with no race reported.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/cycle.go internal/pulls/cycle_test.go
git commit -m "Run the census on a thirty-minute clock, two passes to a cycle" \
  -m "One answer per cycle rather than a provisional paint followed by a settled one: the early paint buys six seconds exactly once, at Dashboard start, and charges for it permanently in a rendering seen for five seconds every half hour and a section carrying two ages where the chrome line states one." \
  -m "Two clocks. The network is retried at five minutes rather than at the end of the window, which is release's own instinct scaled. An expired login is not: it keeps polling at thirty minutes, so a gh auth login in another pane heals the section without a Dashboard restart — stopping would be right about gh never refreshing itself and wrong about what that costs." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Matching a Session's checkout to its Pull

**Files:**
- Create: `internal/pulls/match.go`
- Test: `internal/pulls/match_test.go`

**Interfaces:**
- Consumes: `Pull`, `Set`, `Pull.YourMove` from Tasks 1–3.
- Produces: `pulls.NameWithOwner(remote string) string`, `pulls.Origins{Read func(root string) string}` with `(*Origins).Of(root string) string`, `Set.Matched(nameWithOwner, branch string) []Pull`, `Set.YourMove(nameWithOwner, branch string) bool`.

- [ ] **Step 1: Write the failing test**

```go
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
		"":                                     "",
		"not a url at all":                     "",
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
	p := Pull{
		List: list, Repo: repo, Head: head, Number: 1,
		Mergeable: "MERGEABLE", MergeState: state, Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}
	return p
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pulls/ -run 'Origin|Match|Mark|Branch'`
Expected: FAIL — `NameWithOwner` undefined.

- [ ] **Step 3: Write the implementation**

```go
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
	// scp-style: [user@]host:owner/repo
	if host, path, ok := strings.Cut(remote, ":"); ok && !strings.Contains(host, "/") {
		if !isGitHub(host) {
			return ""
		}
		return trimmed(path)
	}
	// URL-style: scheme://[user@]host/owner/repo
	_, rest, ok := strings.Cut(remote, "://")
	if !ok {
		return ""
	}
	host, path, ok := strings.Cut(rest, "/")
	if !ok || !isGitHub(host) {
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pulls/ -race -v`
Expected: PASS, every test, no race.

- [ ] **Step 5: Commit**

```bash
git add internal/pulls/match.go internal/pulls/match_test.go
git commit -m "Match a Pull to a checkout by its origin and its branch" \
  -m "Branch alone is dead: five of fifteen Pulls shared a head branch with a Pull in another repo — three repos on one dependabot branch — and a checkout was sitting on one of them, so a branch-only rule would have matched one checkout to three Pulls. The repo half comes from git remote get-url origin rather than the ~/Projects/<org>/<repo> convention, which holds for 98 of 103 clones and fails silently on the rest: the row simply never lights up, which reads exactly like having no Pull." \
  -m "Read once per root and kept for the process. It reads .git/config, so the network boundary is untouched — it is the fetch that redrew that, not this." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The row — the measured identity column, the state, the marks

**Files:**
- Create: `internal/dashboard/pulls.go`
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: `pulls.Set`, `pulls.Pull`, `pulls.Authored`, `pulls.Requested`, `Pull.State`, `Pull.YourMove`, `Pull.Failing`, `Pull.Approved`, `Pull.Stacked`, `Set.Of` from Tasks 1–7. Dashboard-private `spread`, `truncate`, `tail`, `joined`, `rendered`, `quietStyle`, `selectedStyle`, `blurredSelectedStyle`, `styleOf`.
- Produces: the glyph constants below; `pullsSection` on the Model (fields `open`, `cursor`, `offset`, `set`, `fetched`, `body`, `active`); `m.pullsColumns() (identity, numbers int)`; `m.pullsRow(p pulls.Pull, cursor bool) string`; `said(p pulls.Pull) string`; `stackOf(p pulls.Pull) string`; `marksOf(p pulls.Pull) string`; `yourMoveStyle`.

**The colour decision made here.** `yourMoveStyle` gets its own literal `#3fb950` rather than calling `session.Ready.Colour()`. That is `brandStyle`'s own precedent, stated in its comment: the two are the same triplet today, and one is not the other — `CONTEXT.md` keeps **Attention** for Sessions and **Your move** for Pulls, so the colour has to be free to move without dragging Ready with it. This also makes the spec's one open question — the colour of `◆` — a one-line change in Task 13.

- [ ] **Step 1: Write the failing test**

```go
package dashboard

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/pulls"
	"github.com/charmbracelet/x/ansi"
)

// pull builds a resting AUTHORED Pull in the shape the measured population
// actually has: BLOCKED under branch protection, checks PENDING.
func pull(list pulls.List, repo string, number int) pulls.Pull {
	return pulls.Pull{
		List: list, Repo: repo, Number: number,
		Head: "head-" + itoa(number), Base: "master", Default: "master",
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Review: "REVIEW_REQUIRED",
		Checks: "PENDING", URL: "https://github.com/" + repo + "/pull/" + itoa(number),
	}
}

func drawn(m Model, p pulls.Pull) string {
	return ansi.Strip(m.pullsRow(p, false))
}

func TestARowIsIdentityLeftAndStateRight(t *testing.T) {
	m := Model{width: 40}
	m.pulls.set = pulls.Set{pull(pulls.Requested, "teamleadercrm/api-internal", 1564)}

	line := drawn(m, m.pulls.set[0])
	if ansi.StringWidth(line) != 40 {
		t.Fatalf("got %d columns, want 40: %q", ansi.StringWidth(line), line)
	}
	if !strings.HasPrefix(line, " ") {
		t.Errorf("the row lost its one column of indent: %q", line)
	}
	if !strings.Contains(line, "api-internal#1564") {
		t.Errorf("the identity is not on the row: %q", line)
	}
	if !strings.HasSuffix(strings.TrimRight(line, " "), "Yours") {
		t.Errorf("the state is not right-aligned: %q", line)
	}
}

func TestTheRepoKeepsItsTailAndTheColumnIsMeasuredOverTheSection(t *testing.T) {
	// elide() is the obvious helper and the wrong one: it keeps the head,
	// which is right for a worktree carrying its ticket and wrong for repos
	// that differ only past their seventeenth character.
	m := Model{width: 40}
	m.pulls.set = pulls.Set{
		pull(pulls.Requested, "teamleadercrm/focus-service-ai-assistant", 1066),
		pull(pulls.Requested, "teamleadercrm/focus-service-ai-credit-usage", 279),
	}

	first, second := drawn(m, m.pulls.set[0]), drawn(m, m.pulls.set[1])
	if !strings.Contains(first, "assistant#1066") || !strings.Contains(second, "usage#279") {
		t.Errorf("the tails did not survive:\n%q\n%q", first, second)
	}
	if strings.Contains(first, "focus-service-ai-…") {
		t.Errorf("the head was kept, so two repos read the same: %q", first)
	}
}

func TestOneRepositoryNeverRendersTwoWays(t *testing.T) {
	// Elided against whatever each row's own tail left over, #979 and #1005 in
	// one repository came out as two different repositories.
	m := Model{width: 40}
	m.pulls.set = pulls.Set{
		pull(pulls.Requested, "teamleadercrm/focus-service-bookkeeping", 979),
		pull(pulls.Requested, "teamleadercrm/focus-service-bookkeeping", 1005),
	}
	m.pulls.set[1].Review = "APPROVED"

	short, long := drawn(m, m.pulls.set[0]), drawn(m, m.pulls.set[1])
	name := func(line string) string {
		name, _, _ := strings.Cut(strings.TrimLeft(line, " "), "#")
		return name
	}
	if name(short) != name(long) {
		t.Errorf("one repository rendered two ways:\n%q\n%q", name(short), name(long))
	}
}

func TestAStatelessRowDrawsADash(t *testing.T) {
	// An empty column reads as a rendering that failed; the dash reads as an
	// answer the harness does not have. It is not a tenth word.
	m := Model{width: 40}
	p := pull(pulls.Authored, "teamleadercrm/focus-service-bookkeeping", 1010)
	p.Mergeable, p.MergeState = "UNKNOWN", "UNKNOWN"
	m.pulls.set = pulls.Set{p}

	line := drawn(m, p)
	if !strings.HasSuffix(strings.TrimRight(line, " "), noState) {
		t.Errorf("a stateless row did not draw %q: %q", noState, line)
	}
}

func TestTheMarksRideAfterTheState(t *testing.T) {
	m := Model{width: 40}
	p := pull(pulls.Authored, "teamleadercrm/focus-service-bookkeeping", 1005)
	p.Review, p.Checks = "APPROVED", "FAILURE"
	m.pulls.set = pulls.Set{p}

	line := strings.TrimRight(drawn(m, p), " ")
	if !strings.HasSuffix(line, "Sent "+approvalMark+" "+checksFailed) {
		t.Errorf("approved with failing checks did not draw Sent ✓ ✗: %q", line)
	}
}

func TestOnlyAFailureDrawsACheckMark(t *testing.T) {
	m := Model{width: 40}
	for checks, want := range map[string]bool{
		"FAILURE": true, "ERROR": true, "SUCCESS": false, "PENDING": false, "": false,
	} {
		p := pull(pulls.Requested, "teamleadercrm/core", 48032)
		p.Checks = checks
		m.pulls.set = pulls.Set{p}
		if got := strings.Contains(drawn(m, p), checksFailed); got != want {
			t.Errorf("checks %q: drew a mark = %v, want %v", checks, got, want)
		}
	}
}

func TestAStackedRowCarriesABareMark(t *testing.T) {
	// It does not name the parent: ↳#48032 is seven columns, and because the
	// identity column is measured over the section those seven come off every
	// row rather than off the four that are stacked.
	m := Model{width: 40}
	p := pull(pulls.Authored, "teamleadercrm/core", 48033)
	p.Base = "PHX-4335-decode-search-term-for-subscription-title"
	m.pulls.set = pulls.Set{p}

	line := drawn(m, p)
	if !strings.Contains(line, stackMark) {
		t.Errorf("a stacked row carries no mark: %q", line)
	}
	if strings.Contains(line, stackMark+"#") {
		t.Errorf("the stack mark named its parent: %q", line)
	}
	// And Landable is withheld there, which is the reason it is marked at all.
	p.MergeState = "CLEAN"
	if got := p.State(); got != pulls.Sent {
		t.Errorf("a clean stacked row read %q, want Sent", got)
	}
}

func TestNoRowExceedsTheSidepanel(t *testing.T) {
	// The mock's own -check, carried into the tests: the widest tail the
	// section can hold, against the longest repository name on the account.
	m := Model{width: 40}
	p := pull(pulls.Authored, "teamleadercrm/focus-service-developer-portal-frontend", 999999)
	p.Base, p.Review, p.Checks = "some-parent-branch", "CHANGES_REQUESTED", "FAILURE"
	m.pulls.set = pulls.Set{p}

	for _, cursor := range []bool{false, true} {
		line := m.pullsRow(p, cursor)
		if w := ansi.StringWidth(ansi.Strip(line)); w != 40 {
			t.Errorf("cursor=%v: got %d columns, want 40: %q", cursor, w, ansi.Strip(line))
		}
	}
}
```

Add `func itoa(n int) string { return strconv.Itoa(n) }` to `pulls_test.go` if the package does not already have one.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run Pulls`
Expected: FAIL — `m.pulls undefined`, `pullsRow undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/dashboard/pulls.go`:

```go
package dashboard

import (
	"strconv"
	"strings"
	"time"

	"github.com/BrechtBonte/ganymede/internal/pulls"
	"github.com/charmbracelet/lipgloss"
)

// The section's own marks. None of these collide with the glyphs already
// spoken on the panel: █ ● ⠿ ○ ❯ ⚠ ❄ ⇡ ⏵ ▣ ⚑ ▢.
const (
	// checksFailed is the only rollup state worth a column. See
	// pulls.Pull.Failing for why the other two are not drawn.
	checksFailed = "✗"
	// approvalMark means exactly one thing: the approval a person gave.
	approvalMark = "✓"
	// stackMark says the Pull sits on something other than the default
	// branch, and deliberately does not say what.
	stackMark = "↳"
	// noState is drawn where the word goes on a row whose mergeability never
	// resolved. An empty column reads as a rendering that failed; this reads
	// as an answer the harness does not have. It is drawn, not said, and
	// appears in no vocabulary.
	noState = "—"
	// aboveMark and belowMark carry the scroll counts on the chrome line,
	// which is the one line in the section that costs no row.
	aboveMark = "▴"
	belowMark = "▾"
)

// yourMoveStyle is how a Pull waiting on you reads.
//
// Its own style with its own literal hex, and deliberately not
// session.Ready.Colour() — brandStyle's own reasoning applied a second time.
// The two are the same triplet today, and Your move is not Attention:
// CONTEXT.md keeps Attention for Sessions and Your move for Pulls, so this has
// to be free to move without dragging Ready with it.
var yourMoveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))

// pullsSection is the Pulls section's whole state: the last good answer, when
// it landed, what the section is showing instead of rows, and where the cursor
// is inside it.
//
// None of it survives a restart, and none of it reaches state.json. A Pull
// decays inside the poll interval, and no view state the Dashboard holds has
// ever survived a restart — not the cursor, not an open picker, not a
// half-typed input.
type pullsSection struct {
	// open says the section has the foot and the cursor.
	open bool
	// cursor is which entry — heading or Pull — the section's own selection
	// is on, and is meaningless while open is false.
	cursor int
	// offset is the first entry the window shows.
	offset int
	// set is the last cycle that landed. A network failure keeps it: the rows
	// are the last good answer and the chrome line's time says how old.
	set pulls.Set
	// fetched is when set landed, drawn at the far end of the PULLS label.
	fetched time.Time
	// body is what the section shows instead of rows, and empty for the rows
	// themselves.
	body pullsBody
}

// said is the state as the row draws it: the word, or the mark that stands for
// its absence.
func said(p pulls.Pull) string {
	if state := p.State(); state != pulls.Unresolved {
		return string(state)
	}
	return noState
}

// stackOf is the one column a stacked Pull spends, and nothing for every other
// row.
func stackOf(p pulls.Pull) string {
	if p.Stacked() {
		return stackMark
	}
	return ""
}

// marksOf is what the world has done to a Pull, as against what state it is
// in: a failing rollup, and an approval a person gave. Both, one, or neither.
func marksOf(p pulls.Pull) string {
	var said []string
	if p.Approved() {
		said = append(said, approvalMark)
	}
	if p.Failing() {
		said = append(said, checksFailed)
	}
	return strings.Join(said, " ")
}

// tailOf is a row's whole right-hand end, which is what the identity column is
// measured against.
func tailOf(p pulls.Pull) string {
	return joined(stackOf(p), said(p), marksOf(p))
}

// pullsColumns is where a row's parts start, measured once over the whole
// section rather than per row.
//
// Per row is the bug it exists to avoid: elided against whatever each row's
// own tail left over, #979 and #1005 in one repository came out as
// focus-service-bookkeep… and focus-service-bookkeeping — the same repository
// reading as two different ones.
func (m Model) pullsColumns() (identity, numbers int) {
	var widest int
	for _, p := range m.pulls.set {
		if w := lipgloss.Width(tailOf(p)); w > widest {
			widest = w
		}
		if w := lipgloss.Width("#" + strconv.Itoa(p.Number)); w > numbers {
			numbers = w
		}
	}
	// One column of indent, one of gap before the tail.
	return max(0, m.width-1-widest-1-numbers), numbers
}

// pullsRow draws one Pull: identity on the left, state right-aligned at the
// far end — the row every other row on the Dashboard already is.
//
// The repo keeps its tail rather than its head. elide() is the obvious helper
// and the wrong one here: it is right for a worktree carrying its ticket and
// wrong for an organisation whose repositories differ only past their
// seventeenth character — focus-service-ai-assistant and
// focus-service-ai-credit-usage both come out as focus-service-ai-… and both
// are on the section today. The cost is a leading … on most rows, which is the
// price of rows that name different things differently.
func (m Model) pullsRow(p pulls.Pull, cursor bool) string {
	room, _ := m.pullsColumns()
	name := tail(p.Repo, room)
	number := "#" + strconv.Itoa(p.Number)
	if cursor {
		// The cursor's row is inverted and otherwise plain, for the reason
		// selectedStyle already gives: a state colour nested inside the
		// inversion fights with it.
		return m.selectedRowStyle().Width(m.width).
			Render(spread(" "+name+number, tailOf(p), m.width))
	}
	styled := joined(
		rendered(quietStyle, stackOf(p)),
		rendered(pullStyle(p), said(p)),
		rendered(markStyle(p), marksOf(p)))
	return spread(" "+quietStyle.Render(name)+number, styled, m.width)
}

// pullStyle is how a Pull's state word reads: the colour a row wanting
// something keeps, and the panel's quiet for every state asking nothing of
// you — the same reading styleOf gives a Session.
func pullStyle(p pulls.Pull) lipgloss.Style {
	if p.YourMove() {
		return yourMoveStyle
	}
	return quietStyle
}

// markStyle draws a failing rollup in Blocked's own red and everything else
// quiet. A failure is the one thing here that has stopped.
func markStyle(p pulls.Pull) lipgloss.Style {
	if p.Failing() {
		return styleOf(session.Blocked)
	}
	return quietStyle
}
```

> `markStyle` needs `"github.com/BrechtBonte/ganymede/internal/session"` in the
> import block.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/ -run Pulls -v`
Expected: PASS, all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/pulls.go internal/dashboard/pulls_test.go
git commit -m "Draw a Pull as identity left and state right" \
  -m "The row every other row on the Dashboard already is, with a different tail. The repo keeps its tail rather than its head, because this org's repositories differ past their seventeenth character and elide() would render focus-service-ai-assistant and focus-service-ai-credit-usage identically. The identity column is measured once over the whole section: elided against whatever each row's own tail left over, two numbers in one repository came out as two different repositories." \
  -m "Your move gets its own style with its own literal hex rather than session.Ready.Colour(), on brandStyle's own reasoning — the two are the same triplet today, and the glossary keeps Attention for Sessions and Your move for Pulls, so this has to be free to move without dragging Ready with it." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: The window — headings that stick, scrolling, the chrome line

**Files:**
- Modify: `internal/dashboard/pulls.go`
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: everything from Task 8.
- Produces: `pullsEntry{heading string, count int, pull pulls.Pull, isPull bool}`; `m.pullsEntries() []pullsEntry`; `m.pullsRows(space int) (lines []string, above, below int)`; `m.pullsHeading(name string, count int) string`; `pullsLegend(width int) string`; `m.pullsLabel(above, below int) string`.

- [ ] **Step 1: Write the failing test**

```go
func manyPulls(n int) pulls.Set {
	set := make(pulls.Set, 0, n)
	for i := range n {
		set = append(set, pull(pulls.Requested, "teamleadercrm/repo-"+itoa(i), 100+i))
	}
	return set
}

func TestBothHeadingsCarryTheirOwnCount(t *testing.T) {
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.set = pulls.Set{
		pull(pulls.Authored, "teamleadercrm/core", 48032),
		pull(pulls.Requested, "teamleadercrm/api-internal", 1564),
		pull(pulls.Requested, "teamleadercrm/focus-frontend", 7430),
	}

	lines, _, _ := m.pullsRows(20)
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "AUTHORED 1") || !strings.Contains(body, "REQUESTED 2") {
		t.Errorf("the headings do not carry their counts:\n%s", body)
	}
	if strings.Index(body, "AUTHORED") > strings.Index(body, "REQUESTED") {
		t.Error("REQUESTED came before AUTHORED")
	}
}

func TestTheSectionEndsOnItsOwnKeyLine(t *testing.T) {
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.set = manyPulls(3)

	lines, _, _ := m.pullsRows(20)
	last := ansi.Strip(lines[len(lines)-1])
	for _, key := range []string{"⏎ jump", "o open", "r refresh", "esc close"} {
		if !strings.Contains(last, key) {
			t.Errorf("the key line is missing %q: %q", key, last)
		}
	}
	if w := ansi.StringWidth(last); w > 40 {
		t.Errorf("the key line is %d columns", w)
	}
}

func TestTheEighteenthPullScrolls(t *testing.T) {
	// 2 headings + 17 rows = 19, and the key line takes the 20th. On the
	// measuring day the section is exactly full.
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.set = manyPulls(17)

	lines, above, below := m.pullsRows(20)
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want exactly the 20 it was given", len(lines))
	}
	if above != 0 || below != 0 {
		t.Errorf("a section that fits reported %d above and %d below", above, below)
	}

	m.pulls.set = manyPulls(18)
	_, above, below = m.pullsRows(20)
	if above != 0 || below != 1 {
		t.Errorf("the 18th Pull: got %d above and %d below, want 0 and 1", above, below)
	}
}

func TestTheHeadingTheWindowOpenedInsideSticks(t *testing.T) {
	// A scrolled section never shows rows whose list is off the top.
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.set = manyPulls(17)
	m.pulls.offset = 8

	lines, above, _ := m.pullsRows(20)
	if first := ansi.Strip(lines[0]); !strings.HasPrefix(first, "REQUESTED") {
		t.Errorf("the window's own heading did not stick: %q", first)
	}
	if above == 0 {
		t.Error("rows above the window were not counted")
	}
}

func TestTheScrollCountsRideOnTheChromeLine(t *testing.T) {
	// The 20th row is the key line at 39 of 40 columns, so there is nowhere in
	// the section to put a marker. It rides the chrome line beside the fetch
	// time, where it costs no row and is never itself scrolled away.
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.fetched = time.Date(2026, 9, 22, 14, 31, 0, 0, time.Local)

	label := ansi.Strip(m.pullsLabel(2, 9))
	if !strings.HasPrefix(label, "PULLS") {
		t.Errorf("the label does not read PULLS: %q", label)
	}
	for _, want := range []string{aboveMark + "2", belowMark + "9", "14:31"} {
		if !strings.Contains(label, want) {
			t.Errorf("the chrome line is missing %q: %q", want, label)
		}
	}
	if w := ansi.StringWidth(label); w != 40 {
		t.Errorf("the chrome line is %d columns: %q", w, label)
	}

	// A section that fits spends no columns saying so.
	quiet := ansi.Strip(m.pullsLabel(0, 0))
	if strings.Contains(quiet, aboveMark) || strings.Contains(quiet, belowMark) {
		t.Errorf("a section that fits drew scroll marks: %q", quiet)
	}
}

func TestAFetchThatHasNotLandedHasNoTimeToDraw(t *testing.T) {
	m := Model{width: 40}
	m.pulls.open = true
	label := ansi.Strip(m.pullsLabel(0, 0))
	if strings.Contains(label, ":") {
		t.Errorf("a section with nothing fetched drew a time: %q", label)
	}
}

func TestTheSectionTakesOnlyWhatItNeeds(t *testing.T) {
	// Three Pulls do not spend twenty lines. The cap is a ceiling, not a size.
	m := Model{width: 40}
	m.pulls.open = true
	m.pulls.set = manyPulls(3)

	lines, _, _ := m.pullsRows(20)
	// 2 headings + 3 rows + the key line.
	if len(lines) != 6 {
		t.Errorf("got %d lines for three Pulls, want 6", len(lines))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'Heading|Scroll|Chrome|Eighteenth|KeyLine|TakesOnly'`
Expected: FAIL — `pullsRows undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/dashboard/pulls.go`:

```go
// pullsEntry is one drawable thing in the section: a list's heading, or a Pull
// under it. The two share a list so the window can be measured in entries and
// the cursor can step through both — a heading is never selectable, but it is
// scrolled past like anything else.
type pullsEntry struct {
	heading string
	count   int
	pull    pulls.Pull
	isPull  bool
}

// pullsEntries is the section as a flat list: AUTHORED and its rows, then
// REQUESTED and its rows.
//
// Sections by role rather than by repo. Measured, repo group headers cost 7
// headings against 2 — 24 lines where the section has 20 — and the meaning
// agrees: one list is a decision you owe someone, the other a decision owed to
// you, and those are different urgencies that do not belong in one ordering.
func (m Model) pullsEntries() []pullsEntry {
	authored, requested := m.pulls.set.Of(pulls.Authored), m.pulls.set.Of(pulls.Requested)
	list := make([]pullsEntry, 0, len(authored)+len(requested)+2)
	for _, group := range []struct {
		name string
		rows []pulls.Pull
	}{{"AUTHORED", authored}, {"REQUESTED", requested}} {
		list = append(list, pullsEntry{heading: group.name, count: len(group.rows)})
		for _, p := range group.rows {
			list = append(list, pullsEntry{pull: p, isPull: true})
		}
	}
	return list
}

// pullsRows draws the section's lists inside space lines, and says how many
// Pulls are out of sight above and below the window.
//
// The last line is always the key line, whatever else fits. The window is
// measured in entries rather than rows because a heading takes a line too, and
// the heading of the list the window opened inside is redrawn at the top — so
// a scrolled section never shows rows whose list is off the screen.
func (m Model) pullsRows(space int) (lines []string, above, below int) {
	list := m.pullsEntries()
	room := space - 1 // the key line has the last

	first := min(max(m.pulls.offset, 0), max(0, len(list)-1))
	if first > 0 {
		name, count, hidden := pullsOpening(list, first)
		if name != "" {
			lines = append(lines, m.pullsHeading(name, count))
			above, room = hidden, room-1
		}
	}

	last := first
	for ; last < len(list) && len(lines) < room+boolToInt(above > 0); last++ {
		drawn := m.pullsLine(list[last], last == m.pulls.cursor)
		if len(lines) >= space-1 {
			break
		}
		lines = append(lines, drawn)
	}
	for _, e := range list[last:] {
		if e.isPull {
			below++
		}
	}
	return append(lines, pullsLegend(m.width)), above, below
}

// pullsLine is one entry: a heading, or a row.
func (m Model) pullsLine(e pullsEntry, cursor bool) string {
	if !e.isPull {
		return m.pullsHeading(e.heading, e.count)
	}
	return m.pullsRow(e.pull, cursor)
}

// pullsOpening is the list the window's first entry belongs to, and how many
// of that list's rows are above the window.
func pullsOpening(list []pullsEntry, first int) (name string, count, hidden int) {
	for i := first - 1; i >= 0; i-- {
		if !list[i].isPull {
			return list[i].heading, list[i].count, hidden
		}
		hidden++
	}
	return "", 0, 0
}

// pullsHeading names a list and carries its count, in the panel's quiet — the
// same weight the SELECTED label is drawn in, for the same reason.
//
// The count is what says how much is out of sight: the window shows what it
// shows, and REQUESTED 11 above four visible rows is the section saying so
// without spending a line on it.
func (m Model) pullsHeading(name string, count int) string {
	return quietStyle.Render(truncate(name+" "+strconv.Itoa(count), m.width))
}

// pullsLegend is the section's own keys, on its last line — the way
// claimingView ends with "⏎ claim · esc cancel".
//
// They stay off the Dock's legend because r fires only inside Pulls, and the
// legend's own rule is that offering a key which would silently do nothing is
// worse than not offering it. Pulls is on screen exactly when r is live.
func pullsLegend(width int) string {
	return fitKeys([]string{"⏎ jump", "o open", "r refresh", "esc close"}, width)
}

// pullsLabel is the section's chrome line: its name, and at the far end what
// is out of sight and when the set was fetched.
//
// The shape header() already uses for the clock, and it costs no row — chrome
// covers the label, so the age and the scroll counts are free. Two hours old
// reads as two hours old against the Dashboard's own clock a few lines above,
// with no word for it, no threshold to cross and no line spent. "Stale" is not
// available: CONTEXT.md lists it under Behind's Avoid, and one word with two
// meanings inside one section is worse than no word.
func (m Model) pullsLabel(above, below int) string {
	var scroll string
	if above > 0 {
		scroll = aboveMark + strconv.Itoa(above)
	}
	if below > 0 {
		scroll = joined(scroll, belowMark+strconv.Itoa(below))
	}
	var fetched string
	if !m.pulls.fetched.IsZero() {
		fetched = m.pulls.fetched.Format("15:04")
	}
	return spread(quietStyle.Render("PULLS"), rendered(quietStyle, joined(scroll, fetched)), m.width)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

> If `boolToInt` reads as noise to the implementer, inline the `room`
> bookkeeping instead — the behaviour the tests pin is what matters, not the
> helper.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/ -run 'Heading|Scroll|Chrome|Eighteenth|KeyLine|TakesOnly|Pulls' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/pulls.go internal/dashboard/pulls_test.go
git commit -m "Give Pulls two headings, a window, and a line it never scrolls" \
  -m "Sections by role rather than by repo: repo group headers cost seven headings against two, which is 24 lines in a section that has 20 — and one list is a decision you owe someone while the other is a decision owed to you, which are different urgencies that do not belong in one ordering." \
  -m "The 20th row is the key line at 39 of 40 columns, so there is nowhere in the section for a scroll marker. It rides the chrome line beside the fetch time, which costs no row and is never itself scrolled away, and the heading of the list the window opened inside is redrawn at the top so a scrolled section never shows rows whose list is off the screen." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: The four bodies, and the one that keeps its rows

**Files:**
- Modify: `internal/dashboard/pulls.go`
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: everything from Tasks 8–9, plus `pulls.Trouble`, `pulls.NotLoggedIn`, `pulls.Unauthorized`, `pulls.Unreachable`, `pulls.TroubleOf`, `pulls.Report`.
- Produces: `pullsBody` with `pullsRowsBody`/`pullsFetching`/`pullsEmpty`/`pullsAuth`/`pullsNetwork`; `bodyOf(report pulls.Report) pullsBody`; `m.pullsPanel(space int) (lines []string, above, below int)`.

- [ ] **Step 1: Write the failing test**

```go
func TestTheFourBodiesAreDistinguishable(t *testing.T) {
	// The update check's silence reads as "you are up to date", true on nearly
	// every day. An empty Pulls reads as "you have none", which on the
	// measuring day is false seventeen times over — so silence is ruled out
	// and each of these says something different.
	seen := map[string]string{}
	for _, c := range []struct {
		says string
		body pullsBody
		want []string
	}{
		{"nothing fetched yet", pullsFetching, []string{"Fetching"}},
		{"genuinely no Pulls", pullsEmpty, []string{"Nothing of yours is open"}},
		{"cannot fetch: the login", pullsAuth, []string{"gh auth login"}},
	} {
		m := Model{width: 40}
		m.pulls.open, m.pulls.body = true, c.body
		lines, _, _ := m.pullsPanel(20)
		drawn := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range c.want {
			if !strings.Contains(drawn, want) {
				t.Errorf("%s: missing %q\n%s", c.says, want, drawn)
			}
		}
		if before, ok := seen[drawn]; ok {
			t.Errorf("%s reads identically to %s", c.says, before)
		}
		seen[drawn] = c.says
		if len(lines) != 20 {
			t.Errorf("%s: got %d lines, want the 20 it was given", c.says, len(lines))
		}
		// Every body keeps the section's own keys, because r is the way out of
		// an expired login.
		if !strings.Contains(drawn, "r refresh") {
			t.Errorf("%s: lost the key line", c.says)
		}
	}
}

func TestANetworkFailureKeepsTheLastGoodRows(t *testing.T) {
	// The first three bodies replace the lists. This one does not: the rows
	// are the last good answer and their own timestamp says how old.
	m := Model{width: 40}
	m.pulls.open, m.pulls.body = true, pullsNetwork
	m.pulls.set = pulls.Set{pull(pulls.Authored, "teamleadercrm/focus-service-bookkeeping", 1010)}
	m.pulls.fetched = time.Date(2026, 9, 22, 14, 31, 0, 0, time.Local)

	lines, _, _ := m.pullsPanel(20)
	drawn := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(drawn, "Unreachable") || !strings.Contains(drawn, "5m") {
		t.Errorf("the network body does not say why or when it will try again:\n%s", drawn)
	}
	if !strings.Contains(drawn, "bookkeeping#1010") {
		t.Errorf("the last good rows were dropped:\n%s", drawn)
	}
	if !strings.Contains(ansi.Strip(m.pullsLabel(0, 0)), "14:31") {
		t.Error("the rows lost the timestamp that says how old they are")
	}
}

func TestWhichBodyAReportProduces(t *testing.T) {
	landed := pulls.Report{Set: pulls.Set{pull(pulls.Authored, "teamleadercrm/core", 1)}, At: time.Now()}
	for _, c := range []struct {
		says   string
		report pulls.Report
		want   pullsBody
	}{
		{"rows", landed, pullsRowsBody},
		{"a fetch that worked and found nothing", pulls.Report{At: time.Now()}, pullsEmpty},
		{"never logged in", pulls.Report{Err: &pulls.Error{Trouble: pulls.NotLoggedIn}}, pullsAuth},
		{"a token expired or revoked", pulls.Report{Err: &pulls.Error{Trouble: pulls.Unauthorized}}, pullsAuth},
		{"the network", pulls.Report{Err: &pulls.Error{Trouble: pulls.Unreachable}}, pullsNetwork},
	} {
		if got := bodyOf(c.report); got != c.want {
			t.Errorf("%s: got %q, want %q", c.says, got, c.want)
		}
	}
}

func TestBeforeTheFirstCycleTheSectionSaysItIsFetching(t *testing.T) {
	// Nothing is remembered across restarts, so every Dashboard start has an
	// unfilled state for roughly twelve seconds — and it is never blank.
	m := Model{width: 40}
	m.pulls.open = true
	lines, _, _ := m.pullsPanel(20)
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Fetching") {
		t.Error("a Dashboard that has not fetched yet drew something other than Fetching")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'Bodies|Network|Report|Fetching'`
Expected: FAIL — `pullsPanel undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/dashboard/pulls.go`:

```go
// pullsBody is what the section shows instead of its rows.
//
// Four, and they have to be visibly different from one another. The update
// check gets to say nothing when it cannot check, because silence there reads
// as "you are up to date" and that is true on nearly every day. An empty Pulls
// reads as "you have none", which is a claim — and on the measuring day a false
// one seventeen times over.
type pullsBody string

const (
	// pullsRowsBody is the lists themselves.
	pullsRowsBody pullsBody = ""
	// pullsFetching is the first cycle of a session, roughly twelve seconds.
	pullsFetching pullsBody = "fetching"
	// pullsEmpty is a fetch that worked and found nothing — with the time on
	// the chrome line proving it fresh.
	pullsEmpty pullsBody = "empty"
	// pullsAuth is a login never made or no longer good.
	pullsAuth pullsBody = "auth"
	// pullsNetwork is GitHub unreachable, which is the one body that keeps its
	// rows.
	pullsNetwork pullsBody = "network"
)

// bodyOf is which body a cycle's answer produces.
//
// The two auth troubles land on one body on purpose: `gh auth login` is the
// errand either way, and a section that distinguished "never logged in" from
// "logged in and expired" would be spending a line on a difference you cannot
// act on differently.
func bodyOf(report pulls.Report) pullsBody {
	if report.Err != nil {
		if trouble, ok := pulls.TroubleOf(report.Err); ok && trouble == pulls.Unreachable {
			return pullsNetwork
		}
		return pullsAuth
	}
	if len(report.Set) == 0 {
		return pullsEmpty
	}
	return pullsRowsBody
}

// pullsPanel is the whole section inside space lines: its body, and how many
// Pulls are out of sight above and below.
func (m Model) pullsPanel(space int) (lines []string, above, below int) {
	switch m.pulls.body {
	case pullsNetwork:
		// The rows stay. They are the last good answer and the chrome line's
		// own timestamp says how old it is; what the section adds is why it is
		// not newer, and when it will try again.
		lines, above, below = m.pullsRows(space - 1)
		reason := cautionStyle.Render(truncate(caution+" Unreachable — retrying 5m", m.width))
		return append([]string{reason}, lines...), above, below
	case pullsFetching, pullsEmpty, pullsAuth:
		return m.pullsSays(space), 0, 0
	default:
		return m.pullsRows(space)
	}
}

// pullsSays is the three bodies that replace the lists: a sentence or two in
// the panel's quiet, and the section's own keys still on the last line —
// because r is the way back from an expired login, and a body that dropped it
// would be the one screen where the recovery key is hidden.
func (m Model) pullsSays(space int) []string {
	var said []string
	switch m.pulls.body {
	case pullsFetching:
		said = []string{quietStyle.Render(truncate("Fetching…", m.width))}
	case pullsEmpty:
		said = []string{
			quietStyle.Render(truncate("Nothing of yours is open, and", m.width)),
			quietStyle.Render(truncate("nobody has asked for a review.", m.width)),
		}
	case pullsAuth:
		said = []string{
			cautionStyle.Render(truncate(caution+" Not logged in to GitHub.", m.width)),
			quietStyle.Render(truncate("Run gh auth login; r retries.", m.width)),
		}
	}
	return append(fill(clip(said, max(0, space-1)), max(0, space-1)), pullsLegend(m.width))
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/`
Expected: PASS, and no existing Dashboard test broken.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/pulls.go internal/dashboard/pulls_test.go
git commit -m "Say which of four things the section is doing, never nothing" \
  -m "Silence is the one rendering ruled out. The update check can say nothing when it cannot check, because silence there reads as 'you are up to date' and that is true on nearly every day; an empty Pulls reads as 'you have none', which is a claim and was false seventeen times over on the measuring day." \
  -m "A network failure is the one body that keeps its rows: they are the last good answer and the chrome line's own timestamp says how old, so what the section adds is why it is not newer. Every body keeps the key line, because r is the way back from an expired login and hiding it there would hide it on the one screen it is for." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: The foot's fifth case, the label, and the space contract

**Files:**
- Modify: `internal/dashboard/dashboard.go` (the `Model` struct near `:260`, `View()` at `:1414`, `detail()` at `:1895`, `selected()` at `:1910`)
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: `m.pullsPanel`, `m.pullsLabel`, `pullsSection` from Tasks 8–10.
- Produces: `Model.pulls pullsSection`; `m.foot(space int) (lines []string, label string)` replacing `selected()`; `m.rowDetail() []string` (the old `selected()`'s last case); `m.settingView() []string` (the old `setting` case); `const footLabel = "SELECTED"`; `m.pullsWanted() int`.

**What changes and what must not.** `selected()` is a priority chain whose default state is the row detail, and four keys already displace it. Pulls becomes the fifth case, below the four input flows and above the row detail — so pressing `c`, `w` or `t` while Pulls is open shows that input in the foot exactly as today, and Pulls returns when the input closes. The label becomes dispatch-dependent for this one case only: **the other four keep hardcoding `SELECTED` and keep lying**, which predates this work and is explicitly not fixed here.

- [ ] **Step 1: Write the failing test**

```go
func TestPullsTakesTheFootBelowTheInputsAndAboveTheRowDetail(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.open = true
	m.pulls.set = manyPulls(4)
	m.pulls.body = pullsRowsBody

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "PULLS") {
		t.Fatalf("the foot does not carry PULLS:\n%s", view)
	}
	if strings.Contains(view, "SELECTED") {
		t.Error("the label kept saying SELECTED while Pulls held the foot")
	}

	// An input opened over it wins, and the label goes back to lying the way
	// it already does for all four.
	m.setting = &setting{dir: "/tmp/x", root: "/tmp/x", name: "x"}
	withInput := ansi.Strip(m.View())
	if !strings.Contains(withInput, "ticket ›") {
		t.Error("the ticket input did not take the foot over Pulls")
	}
	if !strings.Contains(withInput, "SELECTED") {
		t.Error("the label did not go back to SELECTED for an input flow")
	}

	// And Pulls returns when the input closes.
	m.setting = nil
	if !strings.Contains(ansi.Strip(m.View()), "PULLS") {
		t.Error("Pulls did not return when the input closed")
	}
}

func TestTheTreeKeepsTwentyOneRowsWithPullsOpen(t *testing.T) {
	// At height 45 chrome is 4, leaving 41 usable; the cap is half of that,
	// which is 20 — so the tree is never given fewer than 21.
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.open = true
	m.pulls.set = manyPulls(40) // far more than the cap can hold
	m.pulls.body = pullsRowsBody
	m.rows = treeOfSessions(t, 30)

	lines := strings.Split(m.View(), "\n")
	if len(lines) > 45 {
		t.Fatalf("the panel drew %d lines at height 45", len(lines))
	}
	// The foot begins at the PULLS label; everything above it but the header,
	// its rule and the label's own rule is the tree.
	label := -1
	for i, line := range lines {
		if strings.HasPrefix(ansi.Strip(line), "PULLS") {
			label = i
			break
		}
	}
	if label < 0 {
		t.Fatal("no PULLS label in the view")
	}
	// header, rule, ...tree..., rule, label
	if tree := label - 3; tree < 21 {
		t.Errorf("the tree got %d lines, want at least 21", tree)
	}
}

func TestTheUpdateNoticeComesOutOfTheTreeNotTheFoot(t *testing.T) {
	// The box is the one thing on the panel that is always in the same place.
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.open = true
	m.pulls.set = manyPulls(17)
	m.pulls.body = pullsRowsBody
	m.rows = treeOfSessions(t, 30)

	before := footHeight(t, m)
	m.release = release.Update{Installed: "2.1.0", Latest: "2.2.0", Channel: "latest"}
	if after := footHeight(t, m); after != before {
		t.Errorf("the update notice cost the foot %d lines", before-after)
	}
}

func TestASectionSmallerThanTheCapLeavesTheRestToTheTree(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.open = true
	m.pulls.set = manyPulls(3)
	m.pulls.body = pullsRowsBody
	m.rows = treeOfSessions(t, 30)

	// 2 headings + 3 rows + the key line.
	if got := footHeight(t, m); got != 6 {
		t.Errorf("three Pulls took %d lines of the foot, want 6", got)
	}
}
```

Add the two helpers to `pulls_test.go`:

```go
// footHeight is how many lines the foot holds, counted from the label down.
func footHeight(t *testing.T, m Model) int {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(ansi.Strip(line), "PULLS") {
			return len(lines) - i - 1
		}
	}
	t.Fatal("no PULLS label in the view")
	return 0
}

// treeOfSessions is n Session rows under one repo header, which is enough tree
// to be squeezed by anything the foot does.
func treeOfSessions(t *testing.T, n int) []row {
	t.Helper()
	rows := []row{{root: "/repo", state: repo.Free}}
	for i := range n {
		s := session.Session{PID: 1000 + i, ID: "s" + itoa(i), Name: "s" + itoa(i), Dir: "/repo", State: session.Idle}
		rows = append(rows, row{root: "/repo", session: &s, checkout: "/repo", holdsRoot: true})
	}
	return rows
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'Foot|TwentyOne|UpdateNotice|SmallerThanTheCap'`
Expected: FAIL — `m.pulls undefined`.

- [ ] **Step 3: Write the implementation**

Add to the `Model` struct in `dashboard.go`, after `takingOver`:

```go
	// pulls is the Pulls section at the foot: the last cycle that landed,
	// whether the section has the foot and the cursor, and where inside it.
	// Nothing here survives a restart, deliberately — see internal/pulls.
	pulls pullsSection
```

Replace `selected()` with `foot()` and split its last two cases out. In `dashboard.go`:

```go
// footLabel is what the box at the sidepanel's foot is called for every case
// but Pulls.
//
// Four of the five cases are lying when they draw it — a Claim dialog is not
// the selected row either — and that is left exactly as it was. The lie
// predates this work, fixing all four was offered and declined, and making the
// label dispatch-dependent for one case of five is the odd shape that was
// chosen over the alternative of touching four flows this section has no
// business in.
const footLabel = "SELECTED"

// foot is the box at the sidepanel's foot: the lines it holds and the label
// above them.
//
// The chain is a priority order, and the row detail is only its last case: the
// foot is not a detail display that happens to host inputs, it is a modal
// surface whose default state is the detail. Pulls is the fifth case, below the
// four input flows and above the row detail — so c, w and t take the foot while
// Pulls is open exactly as they do today, and Pulls returns when they close.
//
// space is the most lines Pulls may take, and no other case reads it. The four
// inputs and the row detail are as long as they are, and the tree absorbs the
// difference the way it always has.
func (m Model) foot(space int) (lines []string, label string) {
	quiet := quietStyle.Render(truncate(footLabel, m.width))
	switch {
	case m.spawning != nil:
		return m.spawningView(), quiet
	case m.claiming != nil:
		return m.claimingView(), quiet
	case m.takingOver != nil:
		return m.takingOverView(), quiet
	case m.setting != nil:
		return m.settingView(), quiet
	case m.pulls.open:
		// The cost, accepted: while Pulls is open you cannot see the detail of
		// the row you are standing on. Pulls displaces precisely what the
		// cursor is for.
		rows, above, below := m.pullsPanel(min(m.pullsWanted(), space))
		return rows, m.pullsLabel(above, below)
	default:
		return m.rowDetail(), quiet
	}
}

// pullsWanted is how many lines the section would take if nothing capped it:
// both headings, every row, and the key line.
//
// Taking what it needs rather than always taking the cap is what keeps a quiet
// day's three Pulls from costing the tree seventeen rows.
func (m Model) pullsWanted() int {
	switch m.pulls.body {
	case pullsFetching, pullsEmpty, pullsAuth:
		// Two lines of prose at most, and the key line.
		return 3
	case pullsNetwork:
		return len(m.pullsEntries()) + 2
	default:
		return len(m.pullsEntries()) + 1
	}
}
```

`settingView()` is the old `setting` case lifted out verbatim, and `rowDetail()` is everything after it — the `m.cursor >= len(m.rows)` guard, the repo-header box and the Session box. Neither changes a line of what it draws.

`detail()` becomes:

```go
func (m Model) detail(space int) ([]string, string) {
	lines, label := m.foot(space)
	if m.notice != "" {
		for _, line := range strings.Split(ansi.Wrap(m.notice, m.width, ""), "\n") {
			lines = append(lines, styleOf(session.Blocked).Render(line))
		}
	}
	return lines, label
}
```

And `View()`'s frame arithmetic:

```go
	chrome := 4
	if update != "" {
		chrome++
	}
	usable := max(0, m.height-chrome)
	// Pulls takes what it needs, capped at half the usable height, and scrolls
	// inside its budget when the cap bites. Count-agnostic, so it survives
	// however many Pulls there turn out to be — and the scrolling is not new
	// machinery, since shown() already solves "keep the cursor visible inside a
	// line budget" for any budget down to a single line.
	detail, label := m.detail(usable / 2)
	space := usable - len(detail)
	if space < 0 {
		detail = detail[:max(0, len(detail)+space)]
		space = 0
	}

	lines := []string{m.header(), rule}
	if update != "" {
		lines = append(lines, update)
	}
	lines = append(lines, m.tree(space)...)
	lines = append(lines, rule, label)
	lines = append(lines, detail...)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/`
Expected: PASS — including every pre-existing Dashboard test, which all go through `View()` and none of which call `selected()` or `detail()` directly.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/pulls_test.go
git commit -m "Let Pulls take the foot, below the inputs and above the detail" \
  -m "The foot is a modal surface whose default state is the row detail, and four keys already displace it; Pulls is the fifth case in the same chain, so c, w and t take the foot over it exactly as they do today and Pulls returns when they close. The label becomes dispatch-dependent for this one case and the other four keep hardcoding SELECTED — the lie predates this work and fixing all four was offered and declined." \
  -m "The section takes what it needs, capped at half the usable height. At height 45 that is 20 lines, so the tree is never given fewer than 21; a quiet day's three Pulls take six and the rest stays with the tree. The cap is count-agnostic, so it survives however many Pulls there turn out to be." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: The keys — `p`, `esc`, `↑↓`, `o`, `⏎`, `r`

**Files:**
- Modify: `internal/dashboard/dashboard.go` (`pressed()` at `:1018`), `internal/dashboard/pulls.go`, `internal/ticket/tickets.go`
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: `pullsSection`, `m.pullsEntries`, `m.jumpTo`, `m.harness.Tickets` from earlier tasks.
- Produces: `dashboard.Pulls` interface (`Refresh() bool`); `Harness.Pulls Pulls`; `dashboard.PullsReport` message type; `m.togglePulls()`, `m.closePulls()`, `m.pullsUp()`, `m.pullsDown()`, `m.openPull()`, `m.jumpToPull()`, `m.refreshPulls()`, `m.selectedPull() (pulls.Pull, bool)`, `m.pullsReported(PullsReport) Model`; `Tickets.OpenURL(url string) error` and `ticket.Tickets.OpenURL`.

- [ ] **Step 1: Write the failing test**

```go
type refuser struct{ asked, allow int }

func (r *refuser) Refresh() bool {
	r.asked++
	return r.asked <= r.allow
}

func key(r rune) tea.KeyMsg           { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func special(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func pressing(m Model, msgs ...tea.KeyMsg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestPOpensPullsAndEscAndPClose(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.rows = treeOfSessions(t, 5)
	m.pulls.set, m.pulls.body = manyPulls(4), pullsRowsBody

	opened := pressing(m, key('p'))
	if !opened.pulls.open {
		t.Fatal("p did not open Pulls")
	}
	if closed := pressing(opened, special(tea.KeyEsc)); closed.pulls.open {
		t.Error("esc did not close Pulls")
	}
	if toggled := pressing(opened, key('p')); toggled.pulls.open {
		t.Error("p did not toggle Pulls closed")
	}
	// Closed means gone: no spine, no count, nothing on the panel at all.
	if strings.Contains(ansi.Strip(pressing(opened, key('p')).View()), "PULLS") {
		t.Error("a closed section left something behind")
	}
}

func TestThereIsOneCursorAtATime(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.rows = treeOfSessions(t, 5)
	m.cursor = 3
	m.pulls.set, m.pulls.body = manyPulls(4), pullsRowsBody

	open := pressing(m, key('p'), special(tea.KeyDown), special(tea.KeyDown))
	// The tree's highlight freezes rather than moving.
	if open.cursor != 3 {
		t.Errorf("↑↓ moved the tree cursor to %d while Pulls was open", open.cursor)
	}
	if open.pulls.cursor == opened(m).pulls.cursor {
		t.Error("↑↓ did not drive the section's own rows")
	}
	// And esc puts the cursor back exactly where it was.
	if back := pressing(open, special(tea.KeyEsc)); back.cursor != 3 {
		t.Errorf("esc left the tree cursor at %d, want 3", back.cursor)
	}
}

func opened(m Model) Model { return pressing(m, key('p')) }

func TestTheSectionCursorNeverLandsOnAHeading(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.set = pulls.Set{
		pull(pulls.Authored, "teamleadercrm/core", 48032),
		pull(pulls.Requested, "teamleadercrm/api-internal", 1564),
	}
	m.pulls.body = pullsRowsBody

	open := pressing(m, key('p'))
	for range len(open.pullsEntries()) + 2 {
		if e := open.pullsEntries()[open.pulls.cursor]; !e.isPull {
			t.Fatalf("the cursor landed on the heading %q", e.heading)
		}
		open = pressing(open, special(tea.KeyDown))
	}
}

func TestOOpensThePullAndEnterJumpsToItsSession(t *testing.T) {
	opened, jumped := "", 0
	m := Model{width: 40, height: 45, focused: true}
	m.harness = Harness{
		Tickets: &fakeTickets{openURL: func(url string) error { opened = url; return nil }},
		Jumper:  jumperFunc(func(pid int) error { jumped = pid; return nil }),
	}
	s := session.Session{PID: 4242, ID: "s", Name: "s", Dir: "/repo", State: session.Idle}
	m.rows = []row{{root: "/repo", session: &s, checkout: "/repo", holdsRoot: true}}
	m.origins = map[string]string{"/repo": "teamleadercrm/core"}
	m.branches = map[string]string{"/repo": "PHX-4335-decode"}

	p := pull(pulls.Authored, "teamleadercrm/core", 48032)
	p.Head = "PHX-4335-decode"
	m.pulls.set, m.pulls.body = pulls.Set{p}, pullsRowsBody

	open := pressing(m, key('p'))
	pressing(open, key('o'))
	if opened != p.URL {
		t.Errorf("o opened %q, want %q", opened, p.URL)
	}
	pressing(open, special(tea.KeyEnter))
	if jumped != 4242 {
		t.Errorf("⏎ jumped to %d, want 4242", jumped)
	}
}

func TestEnterOnAPullWithNoSessionNamesO(t *testing.T) {
	// Measured at 3 of 15. It mirrors open()'s own "no ticket — press t to set
	// one": the key that would have worked is named rather than nothing
	// happening.
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.set, m.pulls.body = manyPulls(2), pullsRowsBody

	after := pressing(m, key('p'), special(tea.KeyEnter))
	if after.notice == "" {
		t.Fatal("⏎ on an unmatched Pull said nothing at all")
	}
	if !strings.Contains(after.notice, "o") {
		t.Errorf("the notice does not name o: %q", after.notice)
	}
}

func TestRIsANoOpWhileACycleIsInFlight(t *testing.T) {
	asks := &refuser{allow: 1}
	m := Model{width: 40, height: 45, focused: true}
	m.harness = Harness{Pulls: asks}
	m.pulls.set, m.pulls.body = manyPulls(2), pullsRowsBody

	open := pressing(m, key('p'), key('r'), key('r'), key('r'))
	if asks.asked != 3 {
		t.Errorf("r asked %d times, want 3", asks.asked)
	}
	// Holding it cannot stack cycles: the Refresher refuses, and the Dashboard
	// says nothing about a key that is deliberately quiet.
	if open.notice != "" {
		t.Errorf("a refused refresh set a notice: %q", open.notice)
	}
}

func TestRFiresOnlyInsidePulls(t *testing.T) {
	asks := &refuser{allow: 10}
	m := Model{width: 40, height: 45, focused: true}
	m.harness = Harness{Pulls: asks}
	m.rows = treeOfSessions(t, 3)

	pressing(m, key('r'))
	if asks.asked != 0 {
		t.Error("r fired with Pulls closed, where the legend never offers it")
	}
}

func TestCWAndTStayLiveWhilePullsIsOpen(t *testing.T) {
	// Each opens a flow that names its subject before anything happens, so you
	// never act blind — you act on a row whose highlight never moved.
	m := Model{width: 40, height: 45, focused: true}
	m.harness = Harness{Tickets: &fakeTickets{}}
	s := session.Session{PID: 7, ID: "s", Name: "s", Dir: "/repo", State: session.Idle}
	m.rows = []row{{root: "/repo", session: &s, checkout: "/repo", holdsRoot: true}}
	m.pulls.set, m.pulls.body = manyPulls(2), pullsRowsBody

	after := pressing(m, key('p'), key('t'))
	if after.setting == nil {
		t.Fatal("t did not open the ticket input while Pulls was open")
	}
	if !after.pulls.open {
		t.Error("opening an input closed Pulls, which should only be displaced")
	}
}

func TestACycleReportRepaintsTheSectionOnce(t *testing.T) {
	m := Model{width: 40, height: 45, focused: true}
	m.pulls.open = true

	landed := pulls.Report{Set: manyPulls(3), At: time.Date(2026, 9, 22, 14, 31, 0, 0, time.Local)}
	next, _ := m.Update(PullsReport(landed))
	after := next.(Model)
	if after.pulls.body != pullsRowsBody || len(after.pulls.set) != 3 {
		t.Errorf("the report did not land: body=%q rows=%d", after.pulls.body, len(after.pulls.set))
	}
	if !after.pulls.fetched.Equal(landed.At) {
		t.Error("the chrome line's time did not move")
	}

	// A network failure keeps the rows and their timestamp.
	broken := pulls.Report{Err: &pulls.Error{Trouble: pulls.Unreachable}, At: time.Now()}
	next, _ = after.Update(PullsReport(broken))
	kept := next.(Model)
	if kept.pulls.body != pullsNetwork || len(kept.pulls.set) != 3 {
		t.Errorf("a network failure dropped the last good rows: %d", len(kept.pulls.set))
	}
	if !kept.pulls.fetched.Equal(landed.At) {
		t.Error("a failed cycle moved the timestamp on rows it did not refresh")
	}
}
```

> `fakeTickets` and `jumperFunc` are the fakes `dashboard_test.go` already has,
> or one-line additions in its style if the shapes differ. `fakeTickets` needs
> the new `OpenURL` field.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'POpens|OneCursor|Heading|OOpens|EnterOn|RIs|RFires|CWAnd|CycleReport'`
Expected: FAIL — `Harness.Pulls undefined`, `PullsReport undefined`.

- [ ] **Step 3: Write the implementation**

In `dashboard.go`, the hand and the message beside the others:

```go
// Pulls is the fetch of your open pull requests, which r asks to run now.
//
// It is the only thing the harness knows that nothing can push to it: the
// registry watch is a file watcher, the hooks are sub-second edges, and the
// reconciler cross-checks something local — each learns of a change because
// the change announces itself. GitHub does not.
type Pulls interface {
	// Refresh asks for a whole cycle, and says whether this ask is the one
	// that will be answered. A cycle already in flight answers false, which is
	// what makes holding r a no-op rather than a way to stack cycles.
	Refresh() bool
}

// PullsReport is one cycle's answer, arriving the way Release does.
type PullsReport pulls.Report
```

In `Update`, beside `case Release:`:

```go
	case PullsReport:
		return m.pullsReported(msg), nil
```

In `pressed()`, after the input-flow guards and before the `switch msg.Type`:

```go
	// Pulls has the cursor while it is open, so ↑↓, ⏎, o, esc, p and r are its
	// own. Everything else falls through: c, w and t stay live on the frozen
	// tree row, because each opens a flow that names its subject before
	// anything happens.
	if m.pulls.open {
		switch {
		case msg.Type == tea.KeyEsc:
			return m.closePulls().noting(), nil
		case msg.Type == tea.KeyUp:
			return m.pullsUp().noting(), nil
		case msg.Type == tea.KeyDown:
			return m.pullsDown().noting(), nil
		case msg.Type == tea.KeyEnter:
			return m.jumpToPull().noting(), nil
		case msg.Type == tea.KeyRunes && string(msg.Runes) == "o":
			return m.openPull().noting(), nil
		case msg.Type == tea.KeyRunes && string(msg.Runes) == "r":
			return m.refreshPulls().noting(), nil
		case msg.Type == tea.KeyRunes && string(msg.Runes) == "p":
			return m.closePulls().noting(), nil
		}
	}
```

and `p` in the runes switch:

```go
		case "p":
			m = m.togglePulls()
```

In `pulls.go`, the handlers:

```go
// togglePulls opens the section, or closes it.
//
// p is free — the only runes bound on the Dashboard are o, t, g, w and c — and
// every Dashboard key is already the first letter of what it does. There is no
// fourth global chord: the three at tmux's root table are taken from every pane
// of every Session permanently, and a census you glance at, kept right by a
// thirty-minute clock, buys nothing by arriving one keypress sooner.
func (m Model) togglePulls() Model {
	if m.pulls.open {
		return m.closePulls()
	}
	m.pulls.open = true
	m.pulls.cursor, m.pulls.offset = m.firstPull(), 0
	return m
}

// closePulls puts the cursor back exactly where it was.
//
// Closed is fully hidden. A one-line spine carrying a count was rejected twice
// over: it costs one of the 21 rows the tree is guaranteed, paid in the common
// case for the rare one — and the tree already answers "is there anything in
// Pulls", because the Session marks are drawn whether the section is open or
// not.
func (m Model) closePulls() Model {
	m.pulls.open = false
	return m
}

// firstPull is the first selectable entry: a heading is scrolled past but never
// landed on, since there is nothing on it to open or jump to.
func (m Model) firstPull() int {
	for i, e := range m.pullsEntries() {
		if e.isPull {
			return i
		}
	}
	return 0
}

// pullsUp and pullsDown step the section's cursor over its Pulls, skipping the
// headings, and carry the window with them.
func (m Model) pullsUp() Model   { return m.pullsStep(-1) }
func (m Model) pullsDown() Model { return m.pullsStep(1) }

func (m Model) pullsStep(by int) Model {
	list := m.pullsEntries()
	for at := m.pulls.cursor + by; at >= 0 && at < len(list); at += by {
		if !list[at].isPull {
			continue
		}
		m.pulls.cursor = at
		return m.scrolledToCursor()
	}
	return m
}

// scrolledToCursor keeps the section's cursor inside its window, which is the
// same job shown() does for the tree — in one dimension, since every entry
// here is exactly one line.
func (m Model) scrolledToCursor() Model {
	room := max(1, min(m.pullsWanted(), max(0, m.height-4)/2)-1)
	if m.pulls.cursor < m.pulls.offset {
		m.pulls.offset = m.pulls.cursor
	}
	if m.pulls.cursor >= m.pulls.offset+room {
		m.pulls.offset = m.pulls.cursor - room + 1
	}
	return m
}

// selectedPull is the Pull the section's cursor is on.
func (m Model) selectedPull() (pulls.Pull, bool) {
	list := m.pullsEntries()
	if m.pulls.cursor < 0 || m.pulls.cursor >= len(list) || !list[m.pulls.cursor].isPull {
		return pulls.Pull{}, false
	}
	return list[m.pulls.cursor].pull, true
}

// openPull shows the pull request in the browser — o's one meaning over a
// subject it did not have.
//
// internal/browser needs nothing: Browser.Open takes any URL, ticket.Open
// merely builds the JIRA address before calling it, and a Pull carries its own.
func (m Model) openPull() Model {
	p, ok := m.selectedPull()
	if !ok || m.harness.Tickets == nil {
		return m
	}
	if err := m.harness.Tickets.OpenURL(p.URL); err != nil {
		m.notice = err.Error()
	}
	return m
}

// jumpToPull puts the Session the Pull belongs to in front of you — ⏎'s one
// meaning over a subject it did not have, reusing the Session match in the
// other direction for free.
//
// It is the one genuinely useful gesture here: a Pull in Rework is a row you
// want to be standing in, not reading about. When no Session matches —
// measured at 3 of 15 — it names the key that would have worked, the way
// open()'s "no ticket — press t to set one" already does.
func (m Model) jumpToPull() Model {
	p, ok := m.selectedPull()
	if !ok {
		return m
	}
	for i := range m.rows {
		r := m.rows[i]
		if r.session == nil {
			continue
		}
		if m.originOf(r.root) == p.Repo && m.branchOf(r.checkout) == p.Head {
			return m.jumpTo(*r.session)
		}
	}
	m.notice = "no Session on this branch — press o to open it"
	return m
}

// refreshPulls runs a whole cycle by hand — both passes — and resets the
// window, so a refresh at 14:29 does not get a second at 14:31.
//
// A refused ask says nothing. r is deliberately quiet while a cycle is in
// flight, and a notice there would be the harness complaining about a key
// doing exactly what it promised.
func (m Model) refreshPulls() Model {
	if m.harness.Pulls != nil {
		m.harness.Pulls.Refresh()
	}
	return m
}

// pullsReported takes in one cycle's answer.
//
// A cycle that could not reach GitHub keeps the last good rows and the
// timestamp they were fetched under: the rows are still the best answer there
// is, and moving the clock on rows nothing refreshed would be the section
// claiming a freshness it does not have.
func (m Model) pullsReported(report PullsReport) Model {
	m.pulls.body = bodyOf(pulls.Report(report))
	if m.pulls.body == pullsNetwork {
		return m
	}
	m.pulls.set, m.pulls.fetched = report.Set, report.At
	if m.pulls.cursor >= len(m.pullsEntries()) {
		m.pulls.cursor, m.pulls.offset = m.firstPull(), 0
	}
	return m
}
```

`Tickets` gains one method, since `o` now has two subjects:

```go
type Tickets interface {
	Of(dir, root string) ticket.Key
	Set(dir, root string, key ticket.Key) error
	Open(key ticket.Key) error
	// OpenURL shows any address in the browser. A Pull carries its own, where
	// a ticket's is built from its key — the two reach the same browser.
	OpenURL(url string) error
}
```

and `ticket.Tickets` grows the implementation beside `Open`:

```go
// OpenURL shows url in the browser. Open builds a ticket's address and calls
// this; a Pull already has one.
func (t *Tickets) OpenURL(url string) error {
	show := t.Browser
	if show == nil {
		show = browser.Browser{}
	}
	return show.Open(url)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/ ./internal/ticket/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/ internal/ticket/
git commit -m "Give Pulls its keys, and give o and enter a second subject" \
  -m "p opens it, esc and p close it, and there is no fourth global chord: the three at tmux's root table are taken from every pane of every Session permanently, and a census kept right by a thirty-minute clock buys nothing by arriving one keypress sooner. p moves the cursor into the section — one cursor at a time, so there is no focus indicator to find room for inside 40 columns and the arrows never do two things." \
  -m "o and enter keep the one meaning each already had. o opens the pull request where it opened the ticket; enter puts the Session the Pull belongs to in front of you, reusing the branch match in the other direction for free, and names o when no checkout has it. r runs a cycle by hand and is a no-op while one is in flight, which is what stops holding it stacking twelve-second cycles." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: The `◆` mark on a Session row

**Files:**
- Modify: `internal/dashboard/dashboard.go` (`Model`, `asking()` at `:688`, `line()` at `:1642`), `internal/dashboard/rows.go` (`row`, `answers`, `rowsOf`), `internal/dashboard/pulls.go`
- Test: `internal/dashboard/pulls_test.go`

**Interfaces:**
- Consumes: `pulls.Origins`, `Set.YourMove`, `repo.Branch` from Task 7.
- Produces: `Model.origins map[string]string`, `Model.branches map[string]string`, `m.originOf(root string) string`, `m.branchOf(checkout string) string`, `Harness.Origins *pulls.Origins`, `row.yourMove bool`, `answers.yourMove func(root, checkout string) bool`, `const moveMark = "◆"`, `moveStyle`.

> **Ordering note.** The two caches and their accessors are what Task 12's
> `jumpToPull` reads. If you are executing strictly in order, add the `origins`
> and `branches` fields and the `originOf`/`branchOf` accessors — the first
> block of Step 3 below — during Task 12. They are four lines each and belong
> to the same seam.

- [ ] **Step 1: Write the failing test**

```go
func TestASessionRowIsMarkedOnlyWhenItsPullIsYourMove(t *testing.T) {
	const origin, branch = "teamleadercrm/core", "PHX-4335-decode"
	for _, c := range []struct {
		says  string
		state string
		list  pulls.List
		want  bool
	}{
		{"a rebase waiting in that working directory", "BEHIND", pulls.Authored, true},
		{"a review you owe in a main root on a colleague's branch", "BLOCKED", pulls.Requested, true},
		{"a Sent Pull asks nothing of you there", "BLOCKED", pulls.Authored, false},
	} {
		p := pull(c.list, origin, 48032)
		p.MergeState, p.Head = c.state, branch

		m := Model{width: 40, height: 45, focused: true}
		m.pulls.set = pulls.Set{p}
		m.origins = map[string]string{"/repo": origin}
		m.branches = map[string]string{"/repo": branch}
		s := session.Session{PID: 7, ID: "s", Name: "s", Dir: "/repo", State: session.Idle}
		m.rows = []row{{root: "/repo", session: &s, checkout: "/repo", holdsRoot: true,
			yourMove: m.pulls.set.YourMove(origin, branch)}}
		m.cursor = -1

		line := ansi.Strip(m.line(0))
		if got := strings.Contains(line, moveMark); got != c.want {
			t.Errorf("%s: mark = %v, want %v: %q", c.says, got, c.want, line)
		}
	}
}

func TestTheMarkSitsAtTheFarRightOnEverySessionRow(t *testing.T) {
	// spread() right-aligns the tail, so the mark lands in the same column on
	// every Session row however wide the ticket and the age are — the
	// harness's own stated reason for where a repo header's root mark sits.
	m := Model{width: 40, height: 45, focused: true}
	m.cursor = -1
	short := session.Session{PID: 1, ID: "a", Name: "a", Dir: "/repo", State: session.Idle}
	long := session.Session{PID: 2, ID: "b", Name: "b", Dir: "/repo", State: session.Blocked,
		Since: time.Now().Add(-73 * time.Hour)}
	m.rows = []row{
		{root: "/repo", session: &short, checkout: "/repo", holdsRoot: true, yourMove: true},
		{root: "/repo", session: &long, checkout: "/repo/wt", ticket: "FIRE-28419", yourMove: true},
	}

	for i := range m.rows {
		line := ansi.Strip(m.line(i))
		if !strings.HasSuffix(line, moveMark) {
			t.Errorf("row %d does not end on the mark: %q", i, line)
		}
		if w := ansi.StringWidth(line); w != 40 {
			t.Errorf("row %d is %d columns: %q", i, w, line)
		}
	}
}

func TestAHeaderRowNeverCarriesTheMark(t *testing.T) {
	// A repo can sit on the rail with no live Session, and its header is the
	// Main root: the mark's claim is that the work in this checkout has
	// something waiting on you. The far-right column there is also spoken for
	// — it carries the Main root's own state.
	m := Model{width: 40, height: 45, focused: true}
	m.cursor = -1
	m.rows = []row{{root: "/repo", state: repo.Free, yourMove: true}}
	if strings.Contains(ansi.Strip(m.line(0)), moveMark) {
		t.Error("a repo header carried the Your-move mark")
	}
}

func TestAYourMovePullNeverReordersTheTree(t *testing.T) {
	// moreUrgent and louder rank by Session state, and Attention is Sessions
	// only. Promoting a row would put a repo at the top of the rail for a
	// reason the tree's ordering rule cannot express.
	idle := session.Session{PID: 1, ID: "a", Name: "a", Dir: "/quiet", State: session.Idle}
	blocked := session.Session{PID: 2, ID: "b", Name: "b", Dir: "/loud", State: session.Blocked}
	ask := answers{
		root:     func(dir string) string { return dir },
		checkout: func(dir string) string { return dir },
		ticket:   func(string, string) ticket.Key { return "" },
		caution:  func(string) (repo.Caution, bool) { return repo.Caution{}, false },
		popup:    func(string) popup.Status { return popup.Status{} },
		frozen:   func(string) bool { return false },
		claimed:  func(string) (string, bool) { return "", false },
		// Only the quiet repo has a Pull waiting on you.
		yourMove: func(root, checkout string) bool { return root == "/quiet" },
	}

	rows := rowsOf([]session.Session{idle, blocked}, []string{"/quiet", "/loud"}, ask)
	if rows[0].root != "/loud" {
		t.Errorf("a Your-move Pull promoted %q above the Blocked Session", rows[0].root)
	}
	// And the mark is still on the row it belongs to.
	for _, r := range rows {
		if r.session != nil && r.root == "/quiet" && !r.yourMove {
			t.Error("the quiet repo's Session row lost its mark")
		}
	}
}

func TestBothSessionsInOneCheckoutAreMarked(t *testing.T) {
	// The rule is about the checkout, not the process: two Sessions sharing
	// one checkout are both on that branch, and the mark is a claim about the
	// working directory.
	const origin, branch = "teamleadercrm/core", "PHX-4335-decode"
	p := pull(pulls.Authored, origin, 48032)
	p.MergeState, p.Head = "BEHIND", branch

	m := Model{width: 40, height: 45, focused: true}
	m.cursor = -1
	m.pulls.set = pulls.Set{p}
	first := session.Session{PID: 1, ID: "a", Name: "a", Dir: "/repo", State: session.Idle}
	second := session.Session{PID: 2, ID: "b", Name: "b", Dir: "/repo", State: session.Idle}
	mark := m.pulls.set.YourMove(origin, branch)
	m.rows = []row{
		{root: "/repo", session: &first, checkout: "/repo", holdsRoot: true, yourMove: mark},
		{root: "/repo", session: &second, checkout: "/repo", holdsRoot: true, yourMove: mark},
	}
	for i := range m.rows {
		if !strings.Contains(ansi.Strip(m.line(i)), moveMark) {
			t.Errorf("row %d is not marked", i)
		}
	}
}

func TestTheOriginAndTheBranchAreAskedOncePerRoot(t *testing.T) {
	asked := map[string]int{}
	m := Model{width: 40, height: 45}
	m.harness = Harness{Origins: &pulls.Origins{Read: func(root string) string {
		asked[root]++
		return "git@github.com:teamleadercrm/core.git"
	}}}

	for range 3 {
		m.originOf("/repo")
	}
	if asked["/repo"] != 1 {
		t.Errorf("the origin was read %d times, want 1", asked["/repo"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'Mark|YourMove|Header|BothSessions|OncePerRoot'`
Expected: FAIL — `row.yourMove undefined`, `moveMark undefined`.

- [ ] **Step 3: Write the implementation**

The two caches on `Model`, beside `tickets`:

```go
	// origins is what each Main root pushes to, in GitHub's own
	// nameWithOwner. Read once per root and kept for the process, the way
	// pulls.Origins documents: a repository whose remote is re-pointed while
	// the Dashboard is up stays stale until restart.
	origins map[string]string
	// branches is the branch each checkout is on, which with the origin is
	// the whole of the rule matching a Pull to a Session. It is let go of on
	// the tick alongside the tickets, for the same reason: a branch switched
	// in a Main root waits for that tick and no longer.
	branches map[string]string
```

`asking()` gains one line beside the tickets it already clears:

```go
func (m Model) asking() Model {
	clear(m.tickets)
	clear(m.branches)
	clear(m.checkouts)
	return m.showing(m.set)
}
```

The accessors, in `pulls.go`:

```go
// originOf is what the Main root at root pushes to. The harness has no
// equivalent of its own — a repo is a path labelled with filepath.Base — and
// this is the string GitHub returns, which is what makes the match exact.
func (m Model) originOf(root string) string {
	if name, asked := m.origins[root]; asked {
		return name
	}
	if m.harness.Origins == nil {
		return ""
	}
	name := m.harness.Origins.Of(root)
	m.origins[root] = name
	return name
}

// branchOf is the branch a checkout is on. It is already read for every
// Session to derive its ticket, so the join costs no request of its own.
func (m Model) branchOf(checkout string) string {
	if branch, asked := m.branches[checkout]; asked {
		return branch
	}
	branch := repo.Branch(checkout)
	m.branches[checkout] = branch
	return branch
}
```

> Both maps are made in `rebuilt()` alongside `m.roots` and `m.checkouts`, and
> `originOf`/`branchOf` write into the map the Model already holds — the same
> shape `ticketOf` uses, and the reason they are value receivers that still
> cache.

`row` and `answers` in `rows.go`:

```go
	// yourMove says the checkout this Session has its hands on has a Pull
	// waiting on you — Rework, Conflicted, Behind or Landable in AUTHORED, or
	// Yours in REQUESTED. It is never set on a repo's header row: a repo with
	// nothing running in it has no work in flight, and the header's far-right
	// column already carries the Main root's state.
	//
	// It is what the world has done to the row rather than what you have done
	// to it, which is why it is not one of marks() — whose docstring promises
	// the opposite.
	yourMove bool
```

```go
	// yourMove is whether the checkout a Session has its hands on has a Pull
	// waiting on you.
	yourMove func(root, checkout string) bool
```

and in `rowsOf`'s Session-row branch:

```go
			rows = append(rows, row{
				root: root, session: running, ticket: ask.ticket(running.Dir, root), popup: ask.popup(running.Dir),
				checkout: checkout, holdsRoot: checkout == root, frozen: ask.frozen(running.ID),
				yourMove: ask.yourMove(root, checkout),
			})
```

The mark, in `pulls.go`:

```go
// moveMark says the work in this checkout has a Pull waiting on you.
//
// One column, at the far right end of the tail after the age — spread()
// right-aligns the tail, so it lands in the same column on every Session row
// however wide the ticket and the age are. That is the harness's own stated
// reason for where a repo header's root mark sits.
//
// Your move rather than existence, because existence is measurably empty of
// information: of the 3 Pulls with a checkout on their head branch, all three
// were Sent. Two costs are accepted with it — the mark is dark on every row in
// a healthy working set, so it will not be seen working until a Pull goes
// Behind; and "no Pull" and "Sent Pull" read identically, which is correct for
// a tree whose ordering rule is what is asking something of you, and a real
// loss.
const moveMark = "◆"

// moveStyle is how the mark reads.
//
// Its own style with its own literal hex, for yourMoveStyle's reason and one
// more: this is the one thing the design left open. It is Ready's green today
// — the rail's existing "there is something here for you", which is what the
// mark means — against the objection that CONTEXT.md keeps Attention for
// Sessions and Your move for Pulls, and a shared colour blurs two categories
// the glossary separates. Amber was the alternative and collides with the
// caution line directly above it on header rows. Changing it is this line.
var moveStyle = yourMoveStyle
```

and `line()`'s Session branch gains it on the tail:

```go
	mark := marks(r)
	key := abbreviated(r.ticket)
	tail := joined(key, age, moveOf(r))
```

with the plain/styled split following the rest of the row:

```go
	default:
		return spread(indent+styleOf(r.session.State).Render(glyph)+" "+mark+label,
			joined(rendered(ticketColour, key), rendered(quietStyle, age), rendered(moveStyle, moveOf(r))), m.width)
```

```go
// moveOf is the mark, or nothing at all — a row carrying none costs the layout
// nothing, the way joined() already promises.
func moveOf(r row) string {
	if r.session != nil && r.yourMove {
		return moveMark
	}
	return ""
}
```

`elide`'s room calculation in `line()` already subtracts `lipgloss.Width(tail)`, so the label gives up the two columns the mark takes — 25 to 23 on a worktree name.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS, including every pre-existing row and viewport test.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "Mark a Session row whose checkout has a Pull waiting on you" \
  -m "Your move rather than existence, which is measurably empty of information: of the three Pulls with a checkout on their head branch, all three were Sent, so an existence mark would have put a glyph on three rows and named no action. It sits at the far right of the tail, where spread() lands it in the same column on every row — the harness's own reason for where a repo header's root mark sits — and never on a header row, which has no work in flight and whose far-right column carries the root's state." \
  -m "It is not one of marks(), whose docstring promises what you have done to a row where this is what the world has done. And it never reorders: moreUrgent and louder rank by Session state, and promoting a row would put a repo at the top of the rail for a reason the tree's ordering rule cannot express." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: `p pulls` on the Dock's legend

**Files:**
- Modify: `internal/tmuxconf/tmuxconf.go` (`legendKeys`)
- Test: `internal/tmuxconf/tmuxconf_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `legendKeys` gains `"p pulls"` between the popup chord and `"w spawn"`, and `"o open ticket"` becomes `"o open"`.

**The measurement this is built on.** At an 80-column Dock this is a strict gain — six keys visible instead of five. `p` lands at column 56 and displaces nothing; `c`, `t`, `o` and `g` were already past the edge at 80 and still are. Net cost three columns, because `o open ticket` shortening to `o open` hands seven back.

- [ ] **Step 1: Write the failing test**

```go
func TestTheLegendOffersPullsAmongTheChords(t *testing.T) {
	// The placement follows legendKeys's own rule — movement first, then the
	// chords nothing else advertises, because no row is ever standing on them.
	// No row is ever standing on p either.
	at := func(key string) int {
		for i, k := range legendKeys {
			if strings.HasPrefix(k, key) {
				return i
			}
		}
		return -1
	}
	popup, pulls, spawn := at("⌃` popup"), at("p pulls"), at("w spawn")
	if pulls < 0 {
		t.Fatalf("p pulls is not on the legend: %v", legendKeys)
	}
	if !(popup < pulls && pulls < spawn) {
		t.Errorf("p pulls is at %d, want between the popup chord (%d) and w spawn (%d)", pulls, popup, spawn)
	}
}

func TestOpenIsNoLongerOnlyATicket(t *testing.T) {
	// o now has two subjects, and a legend saying "open ticket" over a Pull
	// row would be the box's own words used to mean something they do not.
	for _, key := range legendKeys {
		if key == "o open ticket" {
			t.Error("the legend still says o open ticket")
		}
	}
	if !slices.Contains(legendKeys, "o open") {
		t.Errorf("the legend lost o entirely: %v", legendKeys)
	}
}

func TestSixKeysFitAnEightyColumnDock(t *testing.T) {
	// The strict gain the placement was chosen for. legend() is measured on
	// the plain phrases, so this counts them the same way.
	plain := 0
	visible := 0
	for _, key := range legendKeys {
		next := plain + lipgloss.Width(key)
		if visible > 0 {
			next += len(" · ")
		}
		if next > 80 {
			break
		}
		plain, visible = next, visible+1
	}
	if visible < 6 {
		t.Errorf("%d keys fit 80 columns, want at least 6", visible)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tmuxconf/ -run 'Legend|Open|SixKeys'`
Expected: FAIL — `p pulls` is not on the legend.

- [ ] **Step 3: Write the implementation**

In `legendKeys`, between the popup chord and `"w spawn"`:

```go
	// Pulls, which is a section of the Dashboard rather than a chord — but it
	// belongs here for the same reason the two above it do: no row is ever
	// standing on it, so the SELECTED box will never offer it. At an
	// 80-column Dock it lands at column 56 and displaces nothing, which makes
	// this a strict gain of one visible key.
	//
	// The section's own keys — ⏎ jump · o open · r refresh · esc close — stay
	// off, and ride on its last line instead. r fires only inside Pulls, and
	// offering a key that would silently do nothing is worse than not
	// offering it.
	"p pulls",
	"w spawn",
```

and the shortened label:

```go
	// o open rather than o open ticket: the key now has two subjects — a
	// ticket on a tree row, a pull request on a Pull — and the legend says
	// every label or the plainest one rather than the first of them. The seven
	// columns it hands back are most of what p costs.
	"o open",
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tmuxconf/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmuxconf/
git commit -m "Advertise p on the Dock, and stop calling o a ticket key" \
  -m "p joins the chords group because no row is ever standing on it, which is the same reason the focus key and the popup chord are there: the SELECTED box will never offer it, so the legend is the only place it can be learned. At an 80-column Dock it lands at column 56 and displaces nothing, which makes this six visible keys instead of five." \
  -m "o open ticket becomes o open, because the key now has two subjects and a legend promising a ticket over a Pull row would be the box's own words used to mean something they do not. The seven columns it hands back pay for most of p." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 15: Wiring the watch into the Dashboard

**Files:**
- Modify: `cmd/ganymede/main.go` (`runDashboard`)
- Test: none — this is wiring, and every piece it joins is tested. Verified by hand in Step 4.

**Interfaces:**
- Consumes: `pulls.Watcher`, `pulls.Fetcher`, `pulls.NewRefresher`, `pulls.Origins`, `dashboard.PullsReport`, `Harness.Pulls`, `Harness.Origins`.
- Produces: nothing new.

- [ ] **Step 1: Write the wiring**

In `runDashboard`, beside the other hands:

```go
	// Your open pull requests, on their own thirty-minute clock. The refresher
	// is the hand r pulls; the watch is what answers it, and both end with the
	// Dashboard the way every other watch here does.
	//
	// gh is a hard runtime dependency of the Dashboard from here on, on the
	// footing tmux and claude are on. Nothing is said about it at startup: a
	// gh that will not run reaches you as the section's own body naming
	// `gh auth login`, which is where a stderr line would have sent you anyway.
	refresh := pulls.NewRefresher()
	hands.Pulls = refresh
	hands.Origins = &pulls.Origins{}
```

and beside the `release` goroutine:

```go
	// One cycle's answer at a time, arriving the way the update check's does:
	// New takes the one stream the Dashboard is built around, and this is two
	// lines rather than a parameter threaded through every caller.
	go func() {
		for report := range (pulls.Watcher{Read: pulls.Fetcher{}}).Watch(ctx, refresh.Asked()) {
			program.Send(dashboard.PullsReport(report))
		}
	}()
```

with `"github.com/BrechtBonte/ganymede/internal/pulls"` in the import block.

- [ ] **Step 2: Build it**

Run: `go build ./... && go vet ./...`
Expected: no output.

- [ ] **Step 3: Run the whole suite**

Run: `go test ./...`
Expected: PASS, except the two known-flaky tmux tests. Confirm those against a clean baseline before treating either as a regression:

```bash
git stash && go test ./internal/topology/ 2>&1 | tail -20; git stash pop
```

- [ ] **Step 4: Verify it live**

```bash
make build && ./scripts/refresh.sh
```

Then, in the Dock:
1. Press `p`. The section replaces the SELECTED box and says `Fetching…` for roughly twelve seconds, then draws your two lists.
2. Confirm the working client is untouched — the Dock is still exactly two panes.
3. `↑↓` drives the section; the tree's highlight does not move. `esc` puts the cursor back where it was.
4. `o` on a row opens that pull request in Firefox. `⏎` on a row whose branch you have checked out jumps there; on one you have not, the foot says so and names `o`.
5. `r` refetches and the time at the right of the `PULLS` label moves.
6. `t` while Pulls is open opens the ticket input over it; `esc` brings Pulls back.

- [ ] **Step 5: Commit**

```bash
git add cmd/ganymede/main.go
git commit -m "Wire the Pulls watch and its refresh into the Dashboard" \
  -m "The watch ends with the Dashboard, as every other one here does, and the report reaches it the way the update check's does — New takes the one stream the Dashboard is built around, and this is two lines rather than a parameter threaded through every caller." \
  -m "Nothing is said at startup about a gh that will not run. Unlike terminal-notifier or the cross-check, this one has a rendering of its own: the section names gh auth login in the place you are looking when you notice it missing, which is where a stderr line would have sent you anyway." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 16: The `gh` prerequisite, and the `panel` comment sweep

**Files:**
- Modify: `README.md` (the Prerequisites table)
- Modify: every `*.go` carrying `panel`, `panels` or `panelLines`
- Test: the existing suite, unchanged

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. Both halves are execution rather than decision — but a reviewer should not have to discover either, so they land as two commits of their own.

- [ ] **Step 1: Add the `gh` row**

In `README.md`'s Prerequisites table, after the tmux row:

```markdown
| [GitHub CLI](https://cli.github.com) (`gh`) | Reads the Pulls section at the foot of the dashboard — your open pull requests and the reviews you owe. Ganymede holds no GitHub credential of its own; it uses the login you have already granted `gh` | `brew install gh` then `gh auth login` |
```

It was deliberately not written with the rest of the documentation: it is user-facing documentation of a built product, and before this branch a reader would have installed `gh` for nothing.

- [ ] **Step 2: Commit the README**

```bash
git add README.md
git commit -m "Tell a reader they need gh, now that the Dashboard does" \
  -m "Held back until the section shipped, because before it a reader would have installed gh for nothing. The row says why the harness holds no credential of its own while it is there." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Find every use of the retired word**

```bash
grep -rnE '\bpanels?\b' --include='*.go' .
grep -rn 'panelLines' --include='*.go' .
```

Expected: 68 and 13, which is what the tree held when the plan was written. A number that has moved is fine; the sweep is over what is there.

- [ ] **Step 4: Sweep them**

`panel` is on the `_Avoid_` line in `CONTEXT.md` for both the Dashboard and Pulls, and the Go comments use it throughout to mean the Dashboard — "the SELECTED box is the one thing on the panel that is always in the same place". Reclaiming it for this section would turn every one of those into a trap.

Rules for the sweep:
- A comment meaning the whole left pane becomes **the Dashboard**, or **the sidepanel** where the sentence is about the 40 columns rather than about what is drawn in them. Both are glossary terms.
- `panelLines` becomes `sidepanelLines`, or whatever names what it actually measures — read it before renaming it.
- **Do not touch the Pulls code this branch just wrote**, which never used the word.
- Comments are prose, not identifiers: re-read each sentence after the substitution and fix the ones that now read badly. A mechanical `sed` over 68 sites will produce several that do.

- [ ] **Step 5: Verify and commit**

```bash
grep -rnE '\bpanels?\b' --include='*.go' .   # expect no output
go test ./...
```

```bash
git add -A
git commit -m "Stop calling the Dashboard the panel" \
  -m "The word is on the _Avoid_ line in CONTEXT.md for both the Dashboard and Pulls, and the Go comments used it throughout to mean the Dashboard — so reclaiming it for the section would have turned every one of those into a trap. Sixty-eight comments and thirteen references to panelLines now say Dashboard, or sidepanel where the sentence is about the forty columns rather than about what is drawn in them." \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## The one open question the implementation inherits

**The colour of `◆`.** Task 13 ships it in Ready's green, which is what the mock draws and what the mark means — the rail's existing "there is something here for you". The objection is real: `CONTEXT.md` keeps **Attention** for Sessions and **Your move** for Pulls, and a shared colour blurs two categories the glossary deliberately separates. Amber was the alternative and collides with the caution line sitting directly above it on header rows.

Measurement could not settle it, and it wants a look in the live Dock before it is frozen. After Task 15, with a real set on screen:

```bash
make build && ./scripts/refresh.sh
```

Put both in front of the user rather than picking one — `moveStyle` is a single line in `internal/dashboard/pulls.go`, so rendering the alternative is one edit and one `refresh.sh`. The mark is dark on every row in a healthy working set, so this needs a Pull that has actually gone `Behind`, `Conflicted` or `Landable`, or a main root sitting on a colleague's branch with a review you owe.

## Self-review against the spec

Checked section by section after the plan was written.

| Spec section | Task |
|---|---|
| Vocabulary — the nine words, `panel` retired | 1, 16 |
| Where it lives, the foot's dispatch, the space contract, the label | 11 |
| What it shows — the sets, the qualifier, sections by role, all repos, bots and drafts | 3, 4, 9 |
| The query | 4 |
| The state model — both chains, priority, `Landable` withheld | 1 |
| Marks — orthogonal, failures only | 2, 8 |
| Stacked Pulls are a mark | 2, 3, 8 |
| The row — no title, the kept tail, the measured column, `—` | 8 |
| Scrolling and the two highlights | 9 |
| Fetching — `gh`, the two passes, nothing remembered, the four bodies, the two clocks | 4, 5, 6, 10 |
| Keys — `p`, `esc`, one cursor, `o`/`⏎`, `r`, the legend | 12, 14 |
| Matching a Session to its Pull — the rule, the six cases, the mark, where it sits | 7, 13 |
| Where it stops | **no task, by design** — `strip.go` is untouched, which is the deliverable |
| Docs — `README.md`, the sweep | 16 |
| Testing — every listed case | each task's Step 1 |

**Two things the spec asks for that no task implements, both deliberately:**

1. **The second highlight on a Pull row** — `blurredSelectedStyle` for the Pull the working pane's Session is sitting on. Task 8 draws the cursor's row and Task 9 the window; the `m.active` highlight is the same three lines the mock's `rowLines` carries. **Fold it into Task 8's Step 3** using `m.active` and the `originOf`/`branchOf` pair from Task 13, and add the mock's `cursor` case to Task 8's tests. It is listed here rather than silently dropped.
2. **`assignee:@me`, repo filtering, dimming stacked rows, a third pass, a spine when collapsed, any Tile or strip number.** All rejected in the spec with measurements. No task builds them, and none should.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-pulls-section.md`. Two execution options:

**1. Subagent-Driven (recommended)** — a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints.
