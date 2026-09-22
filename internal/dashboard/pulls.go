package dashboard

import (
	"strconv"
	"strings"
	"time"

	"github.com/BrechtBonte/ganymede/internal/pulls"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/charmbracelet/lipgloss"
)

// The section's own marks. None of these collide with the glyphs already
// spoken on the sidepanel: █ ● ⠿ ○ ❯ ⚠ ❄ ⇡ ⏵ ▣ ⚑ ▢.
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
	// body is what the section shows instead of its rows, and the zero value
	// is the first cycle of a session — which is the state every Dashboard
	// starts in, since nothing about Pulls survives a restart.
	body pullsBody
}

// originOf is what the Main root at root pushes to. The harness has no
// equivalent of its own — a repo is a path labelled with filepath.Base — and
// this is the string GitHub returns, which is what makes the match exact.
//
// The Model's map is a second reading of pulls.Origins' own, which caches for
// the process behind a mutex; a Model built without one still answers, it
// simply asks again.
func (m Model) originOf(root string) string {
	if name, asked := m.origins[root]; asked {
		return name
	}
	if m.harness.Origins == nil {
		return ""
	}
	name := m.harness.Origins.Of(root)
	if m.origins != nil {
		m.origins[root] = name
	}
	return name
}

// branchOf is the branch a checkout is on. It is already read for every
// Session to derive its ticket, so the join costs no request of its own.
func (m Model) branchOf(checkout string) string {
	if branch, asked := m.branches[checkout]; asked {
		return branch
	}
	branch := repo.Branch(checkout)
	if m.branches != nil {
		m.branches[checkout] = branch
	}
	return branch
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
//
// The two highlights are the rail's own pair, and the section draws them the
// way line() does: the cursor's row inverted, and the Pull the working pane is
// sitting on inverted and faint.
func (m Model) pullsRow(p pulls.Pull, cursor bool) string {
	room, _ := m.pullsColumns()
	name, number := tail(p.Repo, room), "#"+strconv.Itoa(p.Number)
	switch {
	case cursor:
		// The cursor's row is inverted and otherwise plain, for the reason
		// selectedStyle already gives: a state colour nested inside the
		// inversion fights with it.
		return m.selectedRowStyle().Width(m.width).Render(spread(" "+name+number, tailOf(p), m.width))
	case m.showingPull(p):
		return blurredSelectedStyle.Width(m.width).Render(spread(" "+name+number, tailOf(p), m.width))
	}
	styled := joined(
		rendered(quietStyle, stackOf(p)),
		rendered(pullStyle(p), said(p)),
		rendered(markStyle(p), marksOf(p)))
	return spread(" "+quietStyle.Render(name)+number, styled, m.width)
}

// showingPull says the working client's pane is sitting on the checkout this
// Pull belongs to — the weaker of the rail's two marks, said of a Pull.
//
// It follows m.active rather than m.cursor, which is the Session you are
// working with and the only one that stays put: ↑↓ parks the tree cursor
// wherever you abandoned it once Pulls is open. It can light more than one
// row, and a working pane whose branch has no Pull lights none, which is the
// correct silence.
func (m Model) showingPull(p pulls.Pull) bool {
	if m.active == 0 {
		return false
	}
	for i := range m.rows {
		r := m.rows[i]
		if r.session == nil || r.session.PID != m.active {
			continue
		}
		return m.originOf(r.root) == p.Repo && m.branchOf(r.checkout) == p.Head
	}
	return false
}

// pullStyle is how a Pull's state word reads: the colour a row wanting
// something keeps, and the sidepanel's quiet for every state asking nothing of
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
	room := max(0, space-1) // the key line has the last

	first := min(max(m.pulls.offset, 0), max(0, len(list)-1))
	if first > 0 {
		if name, count, hidden := pullsOpening(list, first); name != "" {
			lines = append(lines, m.pullsHeading(name, count))
			above = hidden
		}
	}

	last := first
	for ; last < len(list) && len(lines) < room; last++ {
		lines = append(lines, m.pullsLine(list[last], last == m.pulls.cursor))
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

// pullsHeading names a list and carries its count, in the sidepanel's quiet —
// the same weight the SELECTED label is drawn in, for the same reason.
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

// pullsBody is what the section shows instead of its rows.
//
// Four, and they have to be visibly different from one another. The update
// check gets to say nothing when it cannot check, because silence there reads
// as "you are up to date" and that is true on nearly every day. An empty Pulls
// reads as "you have none", which is a claim — and on the measuring day a false
// one seventeen times over.
type pullsBody string

const (
	// pullsFetching is the first cycle of a session, roughly twelve seconds.
	// It is the zero value because it is the state a Dashboard starts in:
	// nothing is remembered across restarts, so there is always a cycle in
	// flight before there are rows.
	pullsFetching pullsBody = ""
	// pullsRowsBody is the lists themselves.
	pullsRowsBody pullsBody = "rows"
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
	case pullsRowsBody:
		return m.pullsRows(space)
	default:
		return m.pullsSays(space), 0, 0
	}
}

// pullsSays is the three bodies that replace the lists: a sentence or two in
// the sidepanel's quiet, and the section's own keys still on the last line —
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
	room := max(0, space-1)
	return append(fill(clip(said, room), room), pullsLegend(m.width))
}
