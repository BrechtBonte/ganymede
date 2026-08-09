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
	"github.com/BrechtBonte/ganymede/internal/ticket"
	"github.com/BrechtBonte/ganymede/internal/topology"
	"github.com/BrechtBonte/ganymede/internal/workingset"
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
	Touch(root string, at time.Time) error
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

// Tickets is everything the harness does with a JIRA ticket: read which one a
// Session's checkout is about, keep the correction when you set one by hand, and
// show it. All three are the same small idea — an ID and a link, no JIRA API —
// so the Dashboard asks one thing for all of them.
type Tickets interface {
	// Of is the ticket the Session working in dir under Main root root is
	// about, and the empty Key when it is about none.
	Of(dir, root string) ticket.Key
	// Set records the ticket by hand, or clears the correction when key is
	// empty and lets the checkout speak for itself again.
	Set(dir, root string, key ticket.Key) error
	// Open shows the ticket in the browser.
	Open(key ticket.Key) error
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
	// Tickets is what each Session's work is about.
	Tickets Tickets
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
	// set is the Sessions the rows were built from, kept so that they can be
	// built again when something other than a Session has moved — a repo
	// picked, a repo opened, the recency window closing on the clock.
	set []session.Session
	// working is the working set: the Main roots the Dashboard shows.
	working []string
	// picker is the fuzzy repo picker, open or not.
	picker picker
	// roots remembers which Main root a Session's directory belongs to.
	roots map[string]string
	// tickets remembers what each Session's directory is about, so that the
	// question is asked of git once rather than once a redraw. It is let go of
	// on the tick, which is what a branch switched in a Main root waits for.
	tickets map[string]ticket.Key
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
	// setting is the ticket being typed, and nil when none is.
	setting *setting
}

// setting is a ticket being set by hand: the checkout it is about, the name it
// was opened over, and what has been typed for it so far.
//
// It holds the checkout rather than the row. The working set is rebuilt
// underneath the input every time any Session anywhere changes state, and a
// correction that landed on whichever row had moved into that position would be
// worse than one that could not be made at all. The name is held for the same
// reason: it is what the box says is being corrected, and the row it was read
// off may be gone by the time it is read out.
type setting struct {
	dir, root string
	name      string
	typed     string
}

// New returns a Dashboard drawing the Sessions that arrive on sessions and
// acting through harness. It is sized for the sidepanel until the terminal
// says otherwise.
//
// The tree is left empty until the first Sessions arrive, rather than drawn
// from the remembered repos alone. The registry is read before the watch is
// handed over, so the first account arrives at once — and a tree drawn ahead
// of it would put the cursor on a quiet repo and then keep it there, since the
// selection follows the row it is on.
func New(sessions <-chan []session.Session, harness Harness) Model {
	return Model{
		width:    topology.SidepanelWidth,
		height:   45,
		sessions: sessions,
		harness:  harness,
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(waitFor(m.sessions), ticking()) }

// Tick is the Dashboard asking to be drawn again with nothing new to show.
type Tick struct{}

// ticking keeps honest the two things that move without the watch saying
// anything. A row that has been Blocked for an hour would go on saying four
// minutes for as long as no Session anywhere moved — and the branch under a
// Session is checked out and switched away from all day, while the Session
// itself sits at its prompt and reports nothing.
func ticking() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return Tick{} })
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
	case Tick:
		return m.asking(), ticking()
	case Discovered:
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
// harness saw you there while they were running. It happens on the tick too,
// through asking — a Session that sits at its prompt for three days reports
// nothing in all that time, and a window measured from the last thing it said
// would start closing while you were still working in it.
func (m Model) showing(sessions []session.Session) Model {
	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.set = sessions
	if m.harness.Activity != nil {
		now := time.Now()
		for _, s := range sessions {
			if root := m.rootOf(s.Dir); root != "" {
				if err := m.harness.Activity.Touch(root, now); err != nil {
					m.notice = err.Error()
				}
			}
		}
	}
	return m.rebuilt()
}

// rebuilt redraws the tree around the working set, leaving the selection on
// the row it was on however far that row has moved.
//
// The row, not the position. Everything the Dashboard does on Enter is done to
// whatever the cursor is on, and the tree re-sorts itself under your hands
// every time a Session changes state — so a cursor that stayed at an index
// would act on a repo you had not looked at, which is the one thing a jump
// must never do.
func (m Model) rebuilt() Model {
	var selected string
	if m.cursor < len(m.rows) {
		selected = m.rows[m.cursor].key()
	}

	if m.roots == nil {
		m.roots = map[string]string{}
	}
	if m.tickets == nil {
		m.tickets = map[string]ticket.Key{}
	}
	m.working = m.workingSet()
	m.rows = rowsOf(m.set, m.working, m.rootOf, m.ticketOf)
	m.waiting = session.AttentionIn(m.set)
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == selected {
			m.cursor = i
			break
		}
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
	live := make([]string, 0, len(m.set))
	for _, s := range m.set {
		if root := m.rootOf(s.Dir); root != "" {
			live = append(live, root)
		}
	}
	var active map[string]time.Time
	if m.harness.Activity != nil {
		active = m.harness.Activity.Active()
	}
	return workingset.Membership{Live: live, Active: active}.Roots(time.Now())
}

// asking lets go of the answers that go stale on their own, and draws the
// working set it already has around fresh ones.
//
// A ticket is the only one of those. Everything else on a row is reported to
// the Dashboard the moment it changes — that is what the watch, the hooks and
// the cross-check are — while the branch a Session is working on is changed by
// you, in a shell, and nothing anywhere says so. Half a minute is a long time
// to look at the ticket you were on before; it is a short time to have looked
// at it for, and it costs one question per Session.
func (m Model) asking() Model {
	clear(m.tickets)
	return m.showing(m.set)
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
//
// A Session with no directory at all has no root. The cross-check can report
// one — it checks the process and takes the rest as it finds it — and asking
// repo.Root about nothing answers with the Dashboard's own working directory,
// which would put the harness's own checkout in the working set and keep it
// there for a week.
func (m Model) rootOf(dir string) string {
	if dir == "" {
		return ""
	}
	if root, known := m.roots[dir]; known {
		return root
	}
	root := repo.Root(dir)
	m.roots[dir] = root
	return root
}

// ticketOf is which ticket a Session's checkout is about, remembering what it
// answered — for the same reason as the root, and unlike a root only until the
// next tick: a branch is checked out and switched away from all day, and the
// answer is only as good as the last time anybody asked.
func (m Model) ticketOf(dir, root string) ticket.Key {
	if about, asked := m.tickets[dir]; asked {
		return about
	}
	if m.harness.Tickets == nil {
		return ""
	}
	about := m.harness.Tickets.Of(dir, root)
	m.tickets[dir] = about
	return about
}

func (m Model) pressed(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Whatever the Dashboard last could not do has been read by now.
	m.notice = ""

	// An open input owns the keyboard. Every letter on the Dashboard is a key
	// that does something, and neither a ticket nor a repo can be typed on a
	// keyboard where half its letters jump, open a browser or end a Session.
	// Ctrl+C is the exception to both, since quitting must never be behind a
	// mode.
	switch {
	case msg.Type == tea.KeyCtrlC:
	case m.setting != nil:
		return m.typed(msg), nil
	case m.picker.open:
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
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	case tea.KeyEnter:
		m = m.jump()
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "o":
			m = m.open()
		case "t":
			m = m.setTicket()
		case "g":
			return m.opening()
		}
	}
	return m, nil
}

// setTicket opens the inline input over the selected Session.
//
// It opens empty, on a Session that already has a ticket as much as on one that
// has none. Correcting one is typing the right one, which is nine characters
// and no thought at all; the ticket it opened on would have to be got out of
// the way first, and there is nowhere on this keyboard for a select-all. The one
// it is replacing stays on the rail above the box the whole time, and clearing
// the correction is the same gesture with nothing typed.
func (m Model) setTicket() Model {
	if m.cursor >= len(m.rows) || m.harness.Tickets == nil {
		return m
	}
	// A repo header is not a Session and has no checkout to be about a ticket.
	if r := m.rows[m.cursor]; r.session != nil {
		m.setting = &setting{dir: r.session.Dir, root: r.root, name: r.session.Name}
	}
	return m
}

// typed is a keystroke going into the open input.
func (m Model) typed(msg tea.KeyMsg) Model {
	switch msg.Type {
	case tea.KeyRunes:
		m.setting = m.setting.with(string(msg.Runes))
	case tea.KeySpace:
		m.setting = m.setting.with(" ")
	case tea.KeyBackspace:
		m.setting = m.setting.back()
	case tea.KeyEsc:
		// Abandoned: nothing is set, and the row goes on saying what it said.
		m.setting = nil
	case tea.KeyEnter:
		m = m.keep()
	}
	return m
}

// with is the input with more typed into it.
func (s *setting) with(text string) *setting {
	typed := *s
	typed.typed += text
	return &typed
}

// back is the input with the last character taken off it.
func (s *setting) back() *setting {
	typed := *s
	if runes := []rune(typed.typed); len(runes) > 0 {
		typed.typed = string(runes[:len(runes)-1])
	}
	return &typed
}

// keep records what was typed as the selected checkout's ticket.
//
// What is read out of it is the first key in it, upper-cased first: a ticket
// arrives either off your fingers, where the shift key is somebody being right
// at your expense, or off the address bar of the tab it is open in, where the
// key is the last thing in a URL. Both are the ticket you meant.
//
// Nothing typed at all clears the correction, which is the only way back to
// letting the branch speak for itself.
func (m Model) keep() Model {
	typed := strings.TrimSpace(m.setting.typed)
	key, ok := ticket.In(strings.ToUpper(typed))
	if typed != "" && !ok {
		// Left open, with what you typed still in it: it is a thing to correct,
		// not a thing to type again.
		m.notice = "not a ticket: " + typed
		return m
	}

	if err := m.harness.Tickets.Set(m.setting.dir, m.setting.root, key); err != nil {
		// A state file that cannot be written will not be written by trying
		// again, so the input closes and the complaint stands in its place.
		m.setting, m.notice = nil, err.Error()
		return m
	}
	// The answer that was remembered for this directory is the old one.
	delete(m.tickets, m.setting.dir)
	m.setting = nil
	return m.showing(m.set)
}

// open shows the selected Session's ticket in the browser. A row with no ticket
// has no link, and says so rather than doing nothing at all — a key that
// silently ignores you reads as a harness that has broken.
func (m Model) open() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	r := m.rows[m.cursor]
	if r.session == nil {
		// A repo header is not a Session and is about no ticket.
		return m
	}
	// A Dashboard with nothing to ask about tickets has every row about none,
	// so this is also where it lands.
	if r.ticket == "" {
		m.notice = "no ticket — press t to set one"
		return m
	}
	if err := m.harness.Tickets.Open(r.ticket); err != nil {
		m.notice = err.Error()
	}
	return m
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
		return m.goTo(selected.root)
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

// goTo takes you to a repo and puts it in the working set, which are the two
// halves of the same thing: where you are working is what the Dashboard shows.
//
// A repo it could not take you to does neither. Recording a repo you never
// reached would leave a row on the Dashboard as the only trace of a jump that
// did not happen.
func (m Model) goTo(root string) Model {
	if m.harness.Opener == nil {
		return m
	}
	if err := m.harness.Opener.Open(root); err != nil {
		m.notice = err.Error()
		return m
	}
	if m.harness.Activity != nil {
		if err := m.harness.Activity.Touch(root, time.Now()); err != nil {
			m.notice = err.Error()
		}
	}
	return m.rebuilt().selecting(root)
}

// selecting puts the cursor on a repo's own row, so that the repo you were
// just taken to is the one the SELECTED box is describing.
func (m Model) selecting(root string) Model {
	for i, r := range m.rows {
		if r.session == nil && r.root == root {
			m.cursor = i
			return m
		}
	}
	return m
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	ruleStyle  = lipgloss.NewStyle().Faint(true)
	quietStyle = lipgloss.NewStyle().Faint(true)
	repoStyle  = lipgloss.NewStyle().Bold(true)
	// A ticket is a reference, not a state: it reads in the colour the
	// validated mock gives it, which is nobody's state colour.
	ticketColour = lipgloss.NewStyle().Foreground(lipgloss.Color("#a5d6ff"))
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
	// glyph, which is what the eye runs down; then the ticket and the age at
	// the far end. The age is what the ordering within a tier is made of — the
	// row above has been waiting on you longer, and the rail should be able to
	// show that — and the ticket is what tells two Sessions in one repo apart
	// before their names do.
	const indent = "  "
	glyph := r.session.State.Glyph()
	age := ageOf(*r.session)
	name := truncate(r.session.Name, m.width-lipgloss.Width(indent+glyph+" ")-lipgloss.Width(about(r.ticket)+" "+age)-1)
	if i == m.cursor {
		return selectedStyle.Width(m.width).Render(spread(indent+glyph+" "+name, about(r.ticket)+" "+age, m.width))
	}
	return spread(indent+styleOf(r.session.State).Render(glyph)+" "+name,
		ticketStyle(r.ticket).Render(about(r.ticket))+" "+quietStyle.Render(age), m.width)
}

// about is how a ticket reads on a row. A Session about no ticket says so,
// rather than leaving a gap that reads as a harness which has not worked it out
// yet — and never a placeholder key, which would read as an answer.
func about(key ticket.Key) string {
	if key == "" {
		return "no ticket"
	}
	return string(key)
}

// ticketStyle draws a ticket in its own colour, and the absence of one in the
// quiet the sidepanel keeps for what is not asking anything of you.
func ticketStyle(key ticket.Key) lipgloss.Style {
	if key == "" {
		return quietStyle
	}
	return ticketColour
}

// detail is the SELECTED box: what the highlighted row has no room to say.
func (m Model) detail() []string {
	lines := m.selected()
	if m.notice != "" {
		// The notice is the one thing in the box that is worth more than one
		// line. Everything else here repeats what the rail already showed, and
		// can be cut off at the edge without costing you anything; a complaint
		// cut off at the edge is a complaint whose reason you never read — and
		// the reason is the end of the sentence, every time.
		for _, line := range strings.Split(ansi.Wrap(m.notice, m.width, ""), "\n") {
			lines = append(lines, styleOf(session.Blocked).Render(line))
		}
	}
	return lines
}

func (m Model) selected() []string {
	if m.setting != nil {
		// The box is the input for as long as one is open. It says what is
		// being corrected rather than what is selected, because the two come
		// apart: the working set is rebuilt under the input every time any
		// Session anywhere moves, and the row you opened it over can end and
		// take the selection with it. The correction is about the checkout,
		// which is still there.
		return []string{
			elide(m.setting.name, m.width),
			// The cursor is drawn rather than placed: the Dashboard shares a
			// terminal with the working client, and the one real cursor
			// belongs over there.
			ticketColour.Render(tail("ticket › "+m.setting.typed+"▌", m.width)),
			quietStyle.Render(shorten(m.setting.dir, m.width)),
			quietStyle.Render(truncate("⏎ set · esc cancel", m.width)),
		}
	}
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
		ticketStyle(r.ticket).Render(truncate(about(r.ticket), m.width)),
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
		quietStyle.Render(truncate(offering(r), m.width)),
	)
}

// offering is what the selected row can be asked to do. A Session about no
// ticket is not offered a link to open, since there is none — but it is always
// offered the key that gives it one.
func offering(r row) string {
	keys := []string{"⏎ jump", "t ticket"}
	if r.ticket != "" {
		keys = append(keys, "o open")
	}
	return strings.Join(keys, " · ")
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
	return tail(underHome(path), width)
}

// tail fits a line into width by cutting from the front. It is for the lines
// whose end is the part worth reading: a path, and an input, where the end is
// where you are typing.
func tail(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if over := ansi.StringWidth(s) - width; over >= 0 {
		// One column of the budget goes to saying something was cut off.
		return ansi.TruncateLeft(s, over+1, "…")
	}
	return s
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
