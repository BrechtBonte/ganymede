// Package dashboard renders the Dashboard: the always-visible sidepanel TUI
// listing the working set of repos and their Sessions.
//
// It draws the state model's account and nothing it has invented — the states
// a Session can be found in, sorted so that everything asking something of you
// is at the top. A Session whose row disappears is Gone.
package dashboard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Sessions is a fresh account of the working set, as the state model reports
// it.
type Sessions []session.Session

// watchEnded says the state model has stopped reporting. The Dashboard keeps
// showing what it last drew rather than blanking the tree.
type watchEnded struct{}

// Jumper puts a Session in front of you by steering the working client to the
// pane it is running in.
type Jumper interface {
	Jump(pid int) error
}

// Seen is how the Dashboard says a Session has been put in front of you, which
// is what clears Ready. It reports as it jumps rather than waiting for tmux to
// notice the focus, because the Dashboard is the one that moved you.
type Seen func(id string)

// Strip is the ambient attention strip: the counts the Dashboard puts where
// you are already looking, in the status line of the Session you are working
// in rather than over here in the sidepanel.
type Strip interface {
	Show(waiting session.Attention) error
}

// Model is the Dashboard's bubbletea model.
type Model struct {
	width, height int
	sessions      <-chan []session.Session
	jumper        Jumper
	strip         Strip
	seen          Seen
	rows          []row
	cursor        int
	// roots remembers which Main root a Session's directory belongs to.
	roots map[string]string
	// waiting is what the working set on show is asking of you: the header
	// counts it, and the strip carries it.
	waiting session.Attention
	// written is what the strip was last told, so that a working set rebuilt
	// with the same Attention in it is not written out again — and shown says
	// whether it has been told anything at all, since a Dashboard opening on a
	// quiet working set still has to clear whatever the last one left there.
	written session.Attention
	shown   bool
	// notice is the last thing the Dashboard was asked to do and could not.
	notice string
}

// New returns a Dashboard drawing the working sets that arrive on sessions,
// jumping through jumper, counting Attention out to strip and reporting what
// you have seen through seen. It is sized for the sidepanel until the terminal
// says otherwise.
func New(sessions <-chan []session.Session, jumper Jumper, strip Strip, seen Seen) Model {
	return Model{
		width:    topology.SidepanelWidth,
		height:   45,
		sessions: sessions,
		jumper:   jumper,
		strip:    strip,
		seen:     seen,
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(waitFor(m.sessions), ticking()) }

// tick is the Dashboard asking to be drawn again with nothing new to show.
type tick struct{}

// ticking keeps the wait ages honest. Nothing arrives on the watch while every
// Session sits still, and a row that has been Blocked for an hour would go on
// saying four minutes for as long as nothing else moved.
func ticking() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return tick{} })
}

// waitFor takes the next working set off the watch.
func waitFor(sessions <-chan []session.Session) tea.Cmd {
	if sessions == nil {
		return nil
	}
	return func() tea.Msg {
		set, ok := <-sessions
		if !ok {
			return watchEnded{}
		}
		return Sessions(set)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case Sessions:
		return m.showing(msg).counted(), waitFor(m.sessions)
	case watchEnded:
		return m, nil
	case tick:
		return m, ticking()
	case tea.KeyMsg:
		return m.pressed(msg)
	}
	return m, nil
}

// showing redraws the tree around a new working set, leaving the selection on
// the row it was on however far that row has moved.
func (m Model) showing(sessions []session.Session) Model {
	var selected string
	if m.cursor < len(m.rows) {
		selected = m.rows[m.cursor].key()
	}

	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.rows = rowsOf(sessions, m.rootOf)
	m.waiting = session.AttentionIn(sessions)
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == selected {
			m.cursor = i
			break
		}
	}
	return m
}

// counted carries the working set's Attention out to the strip.
//
// Counts that have not moved are not written again: writing the strip redraws
// every client on the Sessions server, the working set is rebuilt whenever
// anything at all moves, and flickering the Session you are typing in to tell
// you what it already said is worse than no strip.
//
// It is written here rather than handed to the runtime, which would run it in
// a goroutine of its own: two counts written out of order would leave the
// status line saying something that had stopped being true, and a Dashboard
// which believed it had already said the true thing would never correct it.
// One tmux call, on a count that has actually changed, is the cheaper end of
// that trade — and it is what the jump does too.
func (m Model) counted() Model {
	if m.strip == nil || (m.shown && m.waiting == m.written) {
		return m
	}
	// The strip is deliberate redundancy: everything it says is on the rail
	// already, so one that could not be written is not worth a word about. It
	// is worth trying again, though, which is why only a write that landed
	// counts as having been said.
	if err := m.strip.Show(m.waiting); err != nil {
		return m
	}
	m.written, m.shown = m.waiting, true
	return m
}

// rootOf is repo.Root, remembering what it answered. Working a root out costs
// a git subprocess, the tree is rebuilt every time any Session changes state,
// and a directory does not move house while the harness is up.
func (m Model) rootOf(dir string) string {
	if root, known := m.roots[dir]; known {
		return root
	}
	root := repo.Root(dir)
	m.roots[dir] = root
	return root
}

func (m Model) pressed(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Whatever the Dashboard last could not do has been read by now.
	m.notice = ""

	switch msg.Type {
	case tea.KeyCtrlC:
		// The Dashboard is meant to stay up for as long as the harness does,
		// so it answers to no quit key. Ctrl+C is left alone for the times you
		// are running it by hand — and on the way out it takes the strip with
		// it, since a count nobody is left to keep up to date is one that will
		// be wrong by morning. It goes out the same way every other count
		// does, so nothing can be left in flight behind it.
		if m.strip != nil && m.shown {
			_ = m.strip.Show(session.Attention{})
		}
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	case tea.KeyEnter:
		m = m.jump()
	}
	return m, nil
}

// jump puts the selected Session in front of you, which is also the moment it
// counts as seen. A repo's header row is not a Session and has nowhere to go.
func (m Model) jump() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	jumping := m.rows[m.cursor].session
	if jumping == nil || m.jumper == nil {
		return m
	}
	if err := m.jumper.Jump(jumping.PID); err != nil {
		// A jump that could not be made left you where you were, so the
		// Session has not been seen and its badge stays.
		m.notice = err.Error()
		return m
	}
	if m.seen != nil {
		m.seen(jumping.ID)
	}
	return m
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	ruleStyle  = lipgloss.NewStyle().Faint(true)
	quietStyle = lipgloss.NewStyle().Faint(true)
	repoStyle  = lipgloss.NewStyle().Bold(true)
	// The selected row is inverted and otherwise drawn plainly: a state colour
	// nested inside the inversion fights with it.
	selectedStyle = lipgloss.NewStyle().Reverse(true)
)

// styleOf is how a state is drawn: its own colour where it has one, and the
// quiet the sidepanel keeps for the states asking nothing of you.
func styleOf(state session.State) lipgloss.Style {
	if colour := state.Colour(); colour != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colour))
	}
	return quietStyle
}

func (m Model) View() string {
	rule := ruleStyle.Render(strings.Repeat("─", m.width))
	detail := m.detail()

	// The frame the tree lives in: the title and its rule above, the detail
	// box's rule and heading below.
	space := m.height - 4 - len(detail)
	if space < 0 {
		// A sidepanel with no room for both gives up detail before it gives up
		// the tree.
		detail = detail[:max(0, len(detail)+space)]
		space = 0
	}

	lines := []string{m.header(), rule}
	lines = append(lines, m.tree(space)...)
	lines = append(lines, rule, titleStyle.Render(truncate("SELECTED", m.width)))
	lines = append(lines, detail...)
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// header names the Dashboard, and counts what is waiting on you beside it. The
// tree scrolls and the detail box shows one row; how much is asking something
// of you is a number that should never have to be scrolled to.
func (m Model) header() string {
	return spread(titleStyle.Render("GANYMEDE"), m.counts(), m.width)
}

// counts draws Attention as a mark and a number per tier, in the tier's own
// colour — the same reading as the strip in the working client's status line.
// A working set asking nothing of you is drawn as nothing: a count that is
// always there is one you stop seeing.
func (m Model) counts() string {
	var tiers []string
	for _, tier := range []struct {
		state session.State
		count int
	}{
		{session.Blocked, m.waiting.Blocked},
		{session.Ready, m.waiting.Ready},
	} {
		if tier.count > 0 {
			tiers = append(tiers, styleOf(tier.state).Render(tier.state.Glyph()+" "+strconv.Itoa(tier.count)))
		}
	}
	return strings.Join(tiers, " ")
}

// tree draws the repo tree, showing as much of it as space allows and keeping
// the selection in view.
func (m Model) tree(space int) []string {
	if len(m.rows) == 0 {
		return clip(m.nothingRunning(), space)
	}

	first := 0
	if len(m.rows) > space {
		// Centre what is on show on the selection.
		first = min(max(0, m.cursor-space/2), len(m.rows)-space)
	}
	lines := make([]string, 0, space)
	for i := first; i < len(m.rows) && len(lines) < space; i++ {
		lines = append(lines, m.line(i))
	}
	return lines
}

// nothingRunning says so, rather than leaving an empty frame that reads as a
// Dashboard which has broken.
func (m Model) nothingRunning() []string {
	return []string{
		quietStyle.Render(truncate("No sessions.", m.width)),
		"",
		quietStyle.Render(truncate("Repos with a live Session, a", m.width)),
		quietStyle.Render(truncate("Claimed root, or recent activity", m.width)),
		quietStyle.Render(truncate("appear here.", m.width)),
	}
}

// line draws one row of the tree.
func (m Model) line(i int) string {
	r := m.rows[i]
	if r.session == nil {
		name := truncate(r.label(), m.width)
		if i == m.cursor {
			return selectedStyle.Width(m.width).Render(name)
		}
		return repoStyle.Render(name)
	}

	// Two columns of indent put a Session under its repo; then the state
	// glyph, which is what the eye runs down; and the age at the far end,
	// which is what the ordering within a tier is made of — the row above has
	// been waiting on you longer, and the rail should be able to show that.
	const indent = "  "
	glyph := r.session.State.Glyph()
	age := ageOf(*r.session)
	name := truncate(r.session.Name, m.width-lipgloss.Width(indent+glyph+" ")-lipgloss.Width(age)-1)
	if i == m.cursor {
		return selectedStyle.Width(m.width).Render(spread(indent+glyph+" "+name, age, m.width))
	}
	return spread(indent+styleOf(r.session.State).Render(glyph)+" "+name, quietStyle.Render(age), m.width)
}

// detail is the SELECTED box: what the highlighted row has no room to say.
func (m Model) detail() []string {
	lines := m.selected()
	if m.notice != "" {
		lines = append(lines, styleOf(session.Blocked).Render(truncate(m.notice, m.width)))
	}
	return lines
}

func (m Model) selected() []string {
	if m.cursor >= len(m.rows) {
		return []string{quietStyle.Render("—")}
	}

	r := m.rows[m.cursor]
	if r.session == nil {
		return []string{
			repoStyle.Render(truncate(r.label(), m.width)),
			quietStyle.Render(shorten(r.root, m.width)),
		}
	}

	// What the Session is doing and how long it has been doing it, then its
	// name with the whole width to itself: the rail has to give the indent,
	// the mark and the age their columns first, and a worktree name — which is
	// what carries the ticket — is the row most likely to have run out of them.
	state := styleOf(r.session.State)
	standing := string(r.session.State)
	if age := ageOf(*r.session); age != "" {
		standing += " · " + age
	}
	lines := []string{
		state.Render(r.session.State.Glyph()) + " " + truncate(standing, m.width-2),
		elide(r.session.Name, m.width),
	}
	if r.session.Reason != "" {
		// Blocked is always displayed with its reason.
		lines = append(lines, state.Render(truncate(r.session.Reason, m.width)))
	}
	if r.session.Snippet != "" {
		// An unread badge you cannot read anything of is only half a badge.
		lines = append(lines, quietStyle.Render(truncate(r.session.Snippet, m.width)))
	}
	return append(lines,
		quietStyle.Render(shorten(r.session.Dir, m.width)),
		quietStyle.Render(truncate("⏎ jump", m.width)),
	)
}

// spread puts left at one end of a line width columns wide and right at the
// other. Left is the end that gives way: a name reads truncated, while a count
// or an age is the whole of what it says.
func spread(left, right string, width int) string {
	if room := width - lipgloss.Width(right) - 1; lipgloss.Width(left) > room {
		left = truncate(left, max(0, room))
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return truncate(right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// ageOf is how long a Session has been in the state it is in. A registry
// record with no clock on it has no age to show, and says nothing rather than
// counting from the epoch.
func ageOf(s session.Session) string {
	if s.Since.IsZero() {
		return ""
	}
	return age(time.Since(s.Since))
}

// age is a duration in the coarsest unit that still says something, which in a
// 40-column rail is all there is room for. Anything under a minute is now,
// including a clock that has run backwards on us.
func age(waited time.Duration) string {
	switch {
	case waited < time.Minute:
		return "now"
	case waited < time.Hour:
		return strconv.Itoa(int(waited.Minutes())) + "m"
	case waited < 24*time.Hour:
		return strconv.Itoa(int(waited.Hours())) + "h"
	default:
		return strconv.Itoa(int(waited.Hours()/24)) + "d"
	}
}

// clip keeps at most space lines.
func clip(lines []string, space int) []string {
	if len(lines) <= space {
		return lines
	}
	return lines[:max(0, space)]
}

// truncate keeps a line inside the sidepanel rather than letting it wrap.
// Width is counted in terminal columns, not characters: a Session named in a
// script whose characters are two columns wide would otherwise wrap and push
// every row below it out of step with the selection.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "")
}

// elide fits a name into width by keeping its head — where a worktree carries
// its ticket — and saying that the rest was cut, rather than leaving you to
// wonder whether the name really ends there.
func elide(name string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(name, width, "…")
}

// shorten fits a path into width by keeping its tail, which is the end that
// says which checkout you are looking at.
func shorten(path string, width int) string {
	path = underHome(path)
	if width <= 0 {
		return ""
	}
	if over := ansi.StringWidth(path) - width; over >= 0 {
		// One column of the budget goes to saying something was cut off.
		return ansi.TruncateLeft(path, over+1, "…")
	}
	return path
}

// underHome writes a path under the home directory the way you would say it.
// The separator has to be there: /Users/you-archive is not inside /Users/you.
func underHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
