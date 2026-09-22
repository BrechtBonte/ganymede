package dashboard

import (
	"strconv"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/pulls"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func itoa(n int) string { return strconv.Itoa(n) }

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

func TestTheCursorsRowIsInvertedAndOtherwisePlain(t *testing.T) {
	// The mock's cursor case. A state colour nested inside the inversion
	// fights with it, which is the reason selectedStyle already gives.
	m := Model{width: 40, focused: true}
	p := pull(pulls.Authored, "teamleadercrm/core", 48032)
	p.MergeState = "BEHIND"
	m.pulls.set = pulls.Set{p}

	plain, under := m.pullsRow(p, false), m.pullsRow(p, true)
	if under == plain {
		t.Error("the cursor's row is drawn exactly like every other row")
	}
	if !strings.Contains(ansi.Strip(under), "Behind") {
		t.Errorf("the cursor's row lost its state: %q", ansi.Strip(under))
	}
	// The state colour is gone under the inversion, and the inversion is the
	// only thing left saying anything about the row.
	if strings.Contains(under, yourMoveStyle.Render("Behind")) {
		t.Error("the state colour survived inside the inversion")
	}
}

func TestThePullTheWorkingPaneIsSittingOnIsMarkedToo(t *testing.T) {
	// The rail's own weaker mark, said of a Pull: here is where you were, as
	// against here is where the cursor is. It follows m.active rather than
	// m.cursor, because ↑↓ parks the tree cursor wherever you abandoned it
	// once Pulls is open.
	const origin, branch = "teamleadercrm/core", "PHX-4335-decode"
	m := Model{width: 40, focused: true}
	m.origins = map[string]string{"/repo": origin}
	m.branches = map[string]string{"/repo": branch}
	s := session.Session{PID: 4242, ID: "s", Name: "s", Dir: "/repo", State: session.Idle}
	m.rows = []row{{root: "/repo", session: &s, checkout: "/repo", holdsRoot: true}}

	here := pull(pulls.Authored, origin, 48032)
	here.Head = branch
	elsewhere := pull(pulls.Authored, origin, 48033)
	m.pulls.set = pulls.Set{here, elsewhere}

	// Nothing is on screen yet, so no row is the one you were in.
	quietHere, quietElsewhere := m.pullsRow(here, false), m.pullsRow(elsewhere, false)

	m.active = 4242
	if m.pullsRow(here, false) == quietHere {
		t.Error("the Pull the working pane is sitting on is drawn like every other row")
	}
	// And only that one: a Pull on another branch in the same repo is untouched.
	if m.pullsRow(elsewhere, false) != quietElsewhere {
		t.Error("a Pull on a branch nothing is standing in was marked")
	}
}
