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
