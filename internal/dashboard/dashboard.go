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
	"github.com/BrechtBonte/ganymede/internal/workingset"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Sessions is a fresh account of the Sessions, as the state model reports
// them.
type Sessions []session.Session

// watchEnded says the state model has stopped reporting. The Dashboard keeps
// showing what it last drew rather than blanking the tree.
type watchEnded struct{}

// Jumper puts a Session in front of you by steering the working client to the
// pane it is running in.
type Jumper interface {
	Jump(pid int) error
}

// Opener puts a repo in front of you: the working client moves to that repo's
// Session, brought up if nothing is running there yet. It is what Enter on a
// repo's row does, and what the picker does with the repo you chose.
type Opener interface {
	Open(dir string) error
}

// Inventory is every repo the harness can reach — the whole discovered
// inventory, which is what the picker offers and the Dashboard deliberately
// does not show.
type Inventory interface {
	Repos() ([]string, error)
}

// Activity is where the harness remembers when you were last working in each
// repo. It is what keeps a repo in the working set once its Sessions have
// ended, and it is a file rather than a map because a Dashboard that forgot
// this on restart would have no working set worth the name.
type Activity interface {
	Active() map[string]time.Time
	Touch(root string, at time.Time)
	Save() error
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

// Harness is everything the Dashboard reaches the rest of the world through.
// Any of them may be absent: a Dashboard missing one does less, and still
// draws.
type Harness struct {
	// Jumper puts a Session in front of you, and Opener a repo.
	Jumper Jumper
	Opener Opener
	// Strip carries the Attention counts to the working client's status line.
	Strip Strip
	// Seen reports a Session as looked at, which is what clears Ready.
	Seen Seen
	// Inventory is what the picker offers.
	Inventory Inventory
	// Activity is the harness's memory of where you have been working.
	Activity Activity
}

// Model is the Dashboard's bubbletea model.
type Model struct {
	width, height int
	sessions      <-chan []session.Session
	harness       Harness
	rows          []row
	cursor        int
	// chosen is the row you put the cursor on, so that the selection follows
	// that row however far it moves as the tree is rebuilt. Until you have
	// moved it the cursor belongs to the tree, and sits on whatever is most
	// urgent — which is the row a Dashboard you have just glanced at should
	// be describing.
	chosen string
	// latest is the last account of the Sessions the state model reported,
	// kept so the tree can be rebuilt when the working set changes underneath
	// it — a repo picked, a repo opened — with no Session having moved.
	latest []session.Session
	// working is the working set: the Main roots the Dashboard shows.
	working []string
	// picker is the fuzzy repo picker, open or not.
	picker picker
	// roots remembers which Main root a Session's directory belongs to.
	roots map[string]string
	// waiting is what the Sessions on show are asking of you: the header
	// counts it, and the strip carries it.
	waiting session.Attention
	// written is what the strip was last told, so that a tree rebuilt with the
	// same Attention in it is not written out again — and shown says whether it
	// has been told anything at all, since a Dashboard opening on a quiet
	// morning still has to clear whatever the last one left there.
	written session.Attention
	shown   bool
	// notice is the last thing the Dashboard was asked to do and could not.
	notice string
}

// New returns a Dashboard drawing the Sessions that arrive on sessions and
// acting through harness. It is sized for the sidepanel until the terminal
// says otherwise.
//
// The working set is worked out here rather than waited for, so that the very
// first frame already shows the repos you were last working in — a Dashboard
// that opened empty and filled in once something moved would read as one that
// had lost them.
func New(sessions <-chan []session.Session, harness Harness) Model {
	m := Model{
		width:    topology.SidepanelWidth,
		height:   45,
		sessions: sessions,
		harness:  harness,
		roots:    map[string]string{},
	}
	return m.rebuilt()
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

// waitFor takes the next account of the Sessions off the watch.
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
		// A repo can fall out of the working set with nothing having moved:
		// the recency window closes on the clock, not on an event.
		return m.rebuilt(), ticking()
	case discovered:
		m.picker = m.picker.found(msg)
		return m, nil
	case tea.KeyMsg:
		return m.pressed(msg)
	}
	return m, nil
}

// showing takes in a fresh account of the Sessions.
//
// Every repo running one is recorded as worked in on the way past. That is
// what the recency window is measured from: a repo whose Sessions have all
// ended stays on the Dashboard for as long as the window says, because the
// harness saw you there while they were running.
func (m Model) showing(sessions []session.Session) Model {
	m.latest = sessions
	if m.harness.Activity != nil {
		now := time.Now()
		for _, s := range sessions {
			m.harness.Activity.Touch(m.rootOf(s.Dir), now)
		}
		// Recording is cheap and saving is nearly always a no-op: the stamps
		// are rounded before they go in, so a file saying the same thing is
		// not written again.
		if err := m.harness.Activity.Save(); err != nil {
			m.notice = err.Error()
		}
	}
	return m.rebuilt()
}

// rebuilt redraws the tree around the working set, leaving the selection on
// the row you put it on however far that row has moved.
func (m Model) rebuilt() Model {
	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.working = m.workingSet()
	m.rows = rowsOf(m.latest, m.working, m.rootOf)
	m.waiting = session.AttentionIn(m.latest)
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == m.chosen {
			m.cursor = i
			break
		}
	}
	return m
}

// moving is the cursor being put somewhere on purpose, which is what makes
// that row the one to follow from here on.
func (m Model) moving(to int) Model {
	m.cursor = to
	if to < len(m.rows) {
		m.chosen = m.rows[to].key()
	}
	return m
}

// workingSet is the repos the Dashboard shows: the ones with a Session running
// in them, and the ones the harness remembers you working in recently enough.
//
// Claimed roots belong in here too and are not passed yet — nothing can claim
// one until the Claim action exists. The rule already honours them, so that is
// a field to fill rather than a rule to revisit.
func (m Model) workingSet() []string {
	live := make([]string, 0, len(m.latest))
	for _, s := range m.latest {
		live = append(live, m.rootOf(s.Dir))
	}
	var active map[string]time.Time
	if m.harness.Activity != nil {
		active = m.harness.Activity.Active()
	}
	return workingset.Membership{Live: live, Active: active}.Roots(time.Now())
}

// counted carries the Sessions' Attention out to the strip.
//
// Counts that have not moved are not written again: writing the strip redraws
// every client on the Sessions server, the tree is rebuilt whenever anything
// at all moves, and flickering the Session you are typing in to tell
// you what it already said is worse than no strip.
//
// It is written here rather than handed to the runtime, which would run it in
// a goroutine of its own: two counts written out of order would leave the
// status line saying something that had stopped being true, and a Dashboard
// which believed it had already said the true thing would never correct it.
// One tmux call, on a count that has actually changed, is the cheaper end of
// that trade — and it is what the jump does too.
func (m Model) counted() Model {
	if m.harness.Strip == nil || (m.shown && m.waiting == m.written) {
		return m
	}
	// The strip is deliberate redundancy: everything it says is on the rail
	// already, so one that could not be written is not worth a word about. It
	// is worth trying again, though, which is why only a write that landed
	// counts as having been said.
	if err := m.harness.Strip.Show(m.waiting); err != nil {
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

	// The picker is a mode: while it is up every key belongs to it, because
	// the keys it needs are the printable ones every other action is bound to.
	// Ctrl+C is the exception, since quitting must never be behind a mode.
	if m.picker.open && msg.Type != tea.KeyCtrlC {
		return m.picking(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		// The Dashboard is meant to stay up for as long as the harness does,
		// so it answers to no quit key. Ctrl+C is left alone for the times you
		// are running it by hand — and on the way out it takes the strip with
		// it, since a count nobody is left to keep up to date is one that will
		// be wrong by morning. It goes out the same way every other count
		// does, so nothing can be left in flight behind it.
		if m.harness.Strip != nil && m.shown {
			_ = m.harness.Strip.Show(session.Attention{})
		}
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m = m.moving(m.cursor - 1)
		}
	case tea.KeyDown:
		if m.cursor+1 < len(m.rows) {
			m = m.moving(m.cursor + 1)
		}
	case tea.KeyEnter:
		m = m.jump()
	case tea.KeyRunes:
		if string(msg.Runes) == "g" {
			return m.opening()
		}
	}
	return m, nil
}

// jump puts the selected row in front of you. On a Session that is the pane it
// is running in, and the moment it counts as seen; on a repo's header row it
// is the repo — which may have nothing running in it at all, which is exactly
// why the Dashboard shows repos rather than only Sessions.
func (m Model) jump() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	selected := m.rows[m.cursor]
	if selected.session == nil {
		return m.open(selected.root)
	}
	if m.harness.Jumper == nil {
		return m
	}
	if err := m.harness.Jumper.Jump(selected.session.PID); err != nil {
		// A jump that could not be made left you where you were, so the
		// Session has not been seen and its badge stays.
		m.notice = err.Error()
		return m
	}
	if m.harness.Seen != nil {
		m.harness.Seen(selected.session.ID)
	}
	return m
}

// open takes you to a repo and puts it in the working set, which are the two
// halves of the same thing: where you are working is what the Dashboard shows.
//
// A repo it could not take you to does neither. Recording a repo you never
// reached would leave a row on the Dashboard as the only trace of a jump that
// did not happen.
func (m Model) open(root string) Model {
	if m.harness.Opener == nil {
		return m
	}
	if err := m.harness.Opener.Open(root); err != nil {
		m.notice = err.Error()
		return m
	}
	if m.harness.Activity != nil {
		m.harness.Activity.Touch(root, time.Now())
		if err := m.harness.Activity.Save(); err != nil {
			m.notice = err.Error()
		}
	}
	m = m.rebuilt()
	return m.selecting(root)
}

// selecting puts the cursor on a repo's own row, so that the repo you were
// just taken to is the one the SELECTED box is describing.
func (m Model) selecting(root string) Model {
	for i, r := range m.rows {
		if r.session == nil && r.root == root {
			return m.moving(i)
		}
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
	if m.picker.open {
		return m.pickerView()
	}

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
// Nothing waiting on you is drawn as nothing: a count that is always there is
// one you stop seeing.
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
// Dashboard which has broken. It also says where the rest of them are, since
// an empty working set is exactly when you need the picker.
func (m Model) nothingRunning() []string {
	return []string{
		quietStyle.Render(truncate("No sessions.", m.width)),
		"",
		quietStyle.Render(truncate("Repos with a live Session, a", m.width)),
		quietStyle.Render(truncate("Claimed root, or recent activity", m.width)),
		quietStyle.Render(truncate("appear here.", m.width)),
		"",
		quietStyle.Render(truncate("g — go to any repo", m.width)),
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
			quietStyle.Render(truncate("⏎ go to repo", m.width)),
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
