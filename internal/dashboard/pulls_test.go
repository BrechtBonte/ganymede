package dashboard

import (
	"strconv"
	"strings"
	"testing"
	"time"

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
