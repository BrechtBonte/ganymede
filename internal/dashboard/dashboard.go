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
	"sync"
	"time"

	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/ticket"
	"github.com/BrechtBonte/ganymede/internal/topology"
	"github.com/BrechtBonte/ganymede/internal/workingset"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// caution is the mark every git caution is read by: one column, in front of
// whatever it is cautioning about.
const caution = "⚠"

// frozen is the mark a row carries while its pane is holding a mode over the
// live Session: what that pane is showing you is a held view, and the keys you
// type into it are going to the mode rather than to Claude.
const frozen = "❄"

// popupBusy is the mark a row carries while its own hidden Popup shell is
// running something (§8) — never while it is only open and sitting at its
// prompt, which is not worth a word.
const popupBusy = "⏵"

// Sessions is a fresh account of the working set, as the state model reports
// it.
type Sessions []session.Session

// Cautions is what the Main roots on the rail are carrying, by root, as git
// last answered.
//
// It arrives as a message rather than being read where it is drawn, because
// reading it means asking git whether a tree is dirty — the one question here
// that is as big as the checkout it is asked about. A monorepo takes the better
// part of a second to answer it, and a sidepanel that stopped taking keystrokes
// for that long every half minute would be a sidepanel you stop reaching for.
type Cautions map[string]repo.Caution

// PopupStatuses is what every hidden Popup shell is doing, by the directory
// it opened in, as the harness last swept them.
//
// Like Cautions, it arrives as a message rather than being read where it is
// drawn: finding out costs a tmux round trip per sweep, not per directory,
// but it is still a subprocess the goroutine drawing the tree must not wait
// on.
type PopupStatuses map[string]popup.Status

// FrozenPanes is which Sessions are behind a pane holding a mode over the live
// view, by Claude session id, as the half-minute cross-check last found them.
//
// Like Cautions it arrives as a message rather than being read where it is
// drawn: asking tmux is a round trip, and a View that made one would make it
// several times a second.
type FrozenPanes map[string]bool

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

// Focuser moves keyboard focus to the working client's pane — the dock-level
// half of what alt+g does, given automatically once Enter has already put
// something in front of you.
type Focuser interface {
	Focus() error
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

// Popups is everything the Dashboard needs from the Popup shell (§8): which
// owners are busy, for the marker on their row, and where its own cursor is
// standing, for a popup opened with focus on the rail to know which repo it
// belongs to — the rail has no pane of its own to answer that.
type Popups interface {
	// Sweep kills every popup whose directory has no live Session left in
	// it, and reports the status of the ones that survive.
	Sweep(liveDirs []string) (map[string]popup.Status, error)
	// Selected tells the harness which directory the cursor is on.
	Selected(dir string) error
}

// Claimer is where a Main root Claim is kept (§4.2, §7.3's free key): claim
// it with an optional note, release it, and read which roots are claimed
// right now — which is what keeps a Claimed repo with nothing running in it
// on the rail (§5.1) and what a repo header's own row reads to draw its
// state.
type Claimer interface {
	Claim(root, note string) error
	Release(root string) error
	// Claimed is every root reserved right now, by root, with the note it
	// was claimed with.
	Claimed() map[string]string
	// NoteOf is the note root was claimed with, and whether it is claimed at
	// all — a Takeover reads it to preserve a note already on the root
	// rather than fetching every Claim just to read the one.
	NoteOf(root string) (string, bool)
}

// Harness is everything the Dashboard reaches the rest of the world through.
// Any of them may be absent: a Dashboard missing one does less, and still
// draws.
type Harness struct {
	// Jumper puts a Session in front of you, and Opener a repo.
	Jumper Jumper
	Opener Opener
	// Focuser moves keyboard focus into the working client once Jumper or
	// Opener has already put something new there — the alt+g Enter used to
	// still leave you needing.
	Focuser Focuser
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
	// Spawner starts a background Worktree session for a repo.
	Spawner Spawner
	// Popups is the Popup shell's busy status and the harness's memory of
	// where the cursor is.
	Popups Popups
	// Approver answers a Blocked Session's dialog: the guard's default row,
	// or the decline (§7.3).
	Approver Approver
	// Prompter delivers a prompt into a Session's own input box, on an Idle,
	// Ready or Working row (§7.3).
	Prompter Prompter
	// Stopper interrupts a Working Session's turn with a bare guarded Esc,
	// and ends an Idle or Ready one gracefully once its own confirmation has
	// been answered (§7.3). A Takeover reuses it to end the occupant before
	// claiming the root.
	Stopper Stopper
	// Claimer is where a Main root Claim is kept: claim it, release it, and
	// read which roots are claimed now (§4.2, §7.3's free key).
	Claimer Claimer
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
	// checkouts remembers which checkout a Session has its hands on, which is
	// what says whether it is holding its repo's Main root.
	checkouts map[string]string
	// tickets remembers what each Session's directory is about, so that the
	// question is asked of git once rather than once a redraw. It is let go of
	// on the tick, which is what a branch switched in a Main root waits for.
	tickets map[string]ticket.Key
	// cautions is what git last said each Main root is carrying. It is never
	// cleared, only laid over by the next answer: a marker that blinked out
	// while git was being asked again would be a marker you cannot read.
	cautions Cautions
	// popups is what the harness last said every hidden Popup shell is doing,
	// by the directory it opened in. Like cautions, never cleared on its
	// own — a sweep that found nothing new to report leaves the last one
	// standing rather than blanking a marker mid-command.
	popups PopupStatuses
	// frozen is which Sessions the harness last heard were behind a pane
	// holding a mode over the live view, by Claude session id. Never cleared
	// on its own, only laid over by the next answer, for the reason cautions
	// already documents: a mark that blinked out while tmux was being asked
	// again is a mark you stop reading.
	frozen map[string]bool
	// selectedWritten is the directory last told to the harness as the
	// cursor's own, so that a redraw the cursor has not moved through does
	// not tell it the same thing again.
	selectedWritten string
	// awaiting says a read of the roots is already in flight. The working set is
	// rebuilt several times a second while an agent is working, and the read is
	// the most expensive thing here — so the Dashboard asks once and waits for
	// the answer rather than asking again over the top of the question.
	awaiting bool
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
	// spawning is the worktree-spawn dialog, and nil when none is open.
	spawning *spawning
	// prompting is the prompt-from-dashboard input, and nil when none is open.
	prompting *prompting
	// ending is the end-session confirmation, and nil when none is open.
	ending *ending
	// claiming is the Claim dialog open over a Free repo header, and nil
	// when none is open.
	claiming *claiming
	// takingOver is the Takeover confirmation open over an InUse repo header
	// whose only occupant is Idle, and nil when none is open.
	takingOver *takingOver
	// pending is which Sessions, by pid, have a guarded answer in flight —
	// sent off the main loop and not yet back — so a second y or n on the
	// same row before the first lands cannot fire a second send at the pane
	// the first is still verifying.
	pending map[int]bool
	// spinner is the current tick of the Working spinner, advanced by Spin.
	spinner int
	// spinTicking says a Spin loop is already running, so a rebuild that
	// finds a fresh Working row while one is already ticking does not stack
	// a second loop on top of it.
	spinTicking bool
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

// Spin is the Dashboard asking to be drawn one frame further into whatever is
// spinning on the rail.
type Spin struct{}

// spinning drives the spinner clock. Unlike ticking()'s half minute, this one
// only exists to be fast — and it stops rescheduling itself the moment
// animating() says nothing needs it, rather than running forever in the
// background of a Dashboard sitting quiet at the prompt.
func spinning() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return Spin{} })
}

// animating says whether anything on the rail is mid-spin: a Working Session
// or a root whose holder is Working.
//
// state == InUse alone is not enough (state.go): it fires for any live
// occupant — Idle, Ready, Blocked, Shell included — and a spinner gated on it
// would never stop for as long as a root merely had somebody sitting in it,
// which is most of the time a repo is on the rail at all. holderWorking is
// the narrower question the animation actually needs: not "is somebody here"
// but "is that somebody's turn running."
func (m Model) animating() bool {
	for _, r := range m.rows {
		if r.session != nil {
			if r.session.State == session.Working {
				return true
			}
			continue
		}
		if r.holderWorking {
			return true
		}
	}
	return false
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
		m = m.showing(msg).counted()
		var spin tea.Cmd
		if m.animating() && !m.spinTicking {
			m.spinTicking = true
			spin = spinning()
		}
		// A root nothing has asked git about yet is asked about now, so that a
		// repo arriving on the rail is not drawn without its cautions for half a
		// minute. The roots already answered for wait for the tick: the working
		// set is rebuilt several times a second while an agent is working, and
		// asking git a question that size that often would be a git of one's
		// own running at all times.
		if cmd := m.readingUnread(); cmd != nil {
			m.awaiting = true
			return m, tea.Batch(waitFor(m.sessions), cmd, spin)
		}
		return m, tea.Batch(waitFor(m.sessions), spin)
	case Spin:
		m.spinner++
		if !m.animating() {
			m.spinTicking = false
			return m, nil
		}
		return m, spinning()
	case Cautions:
		m.cautions, m.awaiting = m.laidOver(msg), false
		m = m.showing(m.set)
		// A repo that arrived while git was being asked about the others is
		// asked about now: the answer that just landed was never about it.
		if cmd := m.readingUnread(); cmd != nil {
			m.awaiting = true
			return m, cmd
		}
		return m, nil
	case PopupStatuses:
		m.popups = msg
		m = m.rebuilt()
		return m, nil
	case FrozenPanes:
		m.frozen = msg
		m = m.rebuilt()
		return m, nil
	case watchEnded:
		return m, nil
	case Tick:
		m = m.asking()
		// The clock the cautions are re-read on. It goes ahead whatever is in
		// flight — a read that has not come back is a read whose answers are
		// older than this one's will be — and it is bounded to once every half
		// minute, which is the point of hanging it on the tick.
		m.awaiting = true
		return m, tea.Batch(ticking(), m.reading(), m.sweepingPopups())
	case Discovered:
		m.picker = m.picker.found(msg)
		return m, nil
	case answered:
		delete(m.pending, msg.session.PID)
		if msg.err != nil {
			// The guard's own mismatch: the gate passed but tmux could not verify
			// the send. Said before the fallback jump, which is silent on success
			// and would otherwise overwrite it with nothing — a y or n that did
			// not go through has to say why, not just leave you looking at the
			// pane wondering.
			m.notice = msg.err.Error()
			return m.jumpTo(msg.session, false), nil
		}
		return m, nil
	case sent:
		delete(m.pending, msg.pid)
		if msg.err != nil {
			// The guard's own mismatch: focus the pane and say why. Unlike a
			// mismatched approve or deny, this must not count as seen —
			// sending is what earns a Ready Session its clear ("Sending
			// counts as a prompt, so it clears Ready"), and a send the guard
			// never verified has not earned it, even once the pane it could
			// not confirm is the one now in front of you.
			m.notice = msg.err.Error()
			return m.focusPane(msg.pid), nil
		}
		// Sending counts as a prompt, so it clears Ready the same way seeing
		// the Session does (CONTEXT.md: "Ready -> Idle: seen ... or new
		// prompt") — the registry catches up to Working on its own.
		if m.harness.Seen != nil {
			m.harness.Seen(msg.id)
		}
		return m, nil
	case spawned:
		// The spawn already closed its dialog and reported nothing, on the
		// strength of tmux having taken the window. This is the window answering
		// for the session in it, which is the only answer worth having.
		m.notice = msg.said()
		return m, nil
	case interrupted:
		return m.stopped(msg.pid, msg.err)
	case ended:
		return m.stopped(msg.pid, msg.err)
	case tookOver:
		return m.stoppedTakeover(msg)
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
	if m.checkouts == nil {
		m.checkouts = map[string]string{}
	}
	if m.tickets == nil {
		m.tickets = map[string]ticket.Key{}
	}
	// Read once and closed over rather than asked per row: Claim and Release
	// are synchronous state-file writes, and there is no async round trip
	// here worth caching the way cautions and popups are — but every row
	// asking about the same working set should still see the same answer.
	claimed := map[string]string{}
	if m.harness.Claimer != nil {
		claimed = m.harness.Claimer.Claimed()
	}
	m.working = m.workingSet(claimed)
	m.rows = rowsOf(m.set, m.working, answers{
		root:     m.rootOf,
		checkout: m.checkoutOf,
		ticket:   m.ticketOf,
		caution:  m.cautionOf,
		popup:    m.popupOf,
		frozen:   m.frozenOf,
		claimed:  func(root string) (string, bool) { note, ok := claimed[root]; return note, ok },
	})
	m.waiting = session.AttentionIn(m.set)
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == selected {
			m.cursor = i
			break
		}
	}
	return m.noting()
}

// workingSet is the repos the Dashboard shows: the ones with a Session
// running in them, the ones you have claimed, and the ones the harness
// remembers you working in recently enough.
func (m Model) workingSet(claimed map[string]string) []string {
	live := make([]string, 0, len(m.set))
	for _, s := range m.set {
		if root := m.rootOf(s.Dir); root != "" {
			live = append(live, root)
		}
	}
	roots := make([]string, 0, len(claimed))
	for root := range claimed {
		roots = append(roots, root)
	}
	var active map[string]time.Time
	if m.harness.Activity != nil {
		active = m.harness.Activity.Active()
	}
	return workingset.Membership{Live: live, Claimed: roots, Active: active}.Roots(time.Now())
}

// asking lets go of the answers that go stale on their own, and draws the
// working set it already has around fresh ones.
//
// The ticket is one of those, and which checkout a Session is working in is the
// other. Everything else on a row is reported to the Dashboard the moment it
// changes — that is what the watch, the hooks and the cross-check are — while
// the branch a Session is on is switched by you, in a shell, and the worktree it
// is in can be removed from under it the same way. Half a minute is a long time
// to look at the ticket you were on before; it is a short time to have looked at
// it for. The checkout is let go of for a graver reason than staleness: a root
// whose occupant the Dashboard has stopped recognising is a root drawn Free with
// an agent in it, and that is the one wrong answer this must not give.
func (m Model) asking() Model {
	clear(m.tickets)
	clear(m.checkouts)
	return m.showing(m.set)
}

// reading asks git what every Main root on the rail is carrying, away from the
// goroutine that draws.
//
// The roots are read side by side. They have nothing to do with one another —
// separate checkouts, separate git processes — and read one after another the
// rail's slowest repo would put every root behind it further out of date than
// the last, until a sweep took longer than the clock that starts it.
func (m Model) reading() tea.Cmd {
	roots := m.railed()
	if len(roots) == 0 {
		return nil
	}
	return func() tea.Msg {
		carried := make([]repo.Caution, len(roots))
		var reading sync.WaitGroup
		for i, root := range roots {
			reading.Add(1)
			go func() {
				defer reading.Done()
				carried[i] = repo.CautionOf(root)
			}()
		}
		reading.Wait()

		read := make(Cautions, len(roots))
		for i, root := range roots {
			read[root] = carried[i]
		}
		return read
	}
}

// sweepingPopups asks the harness which hidden Popup shells are busy, on the
// same half-minute clock the cautions are re-read on — asking tmux is the
// same size of question as asking git, and deserves the same one. It is also
// where a popup whose owning Session has gone Gone is killed (§8): Sweep
// does both in the one round trip, since telling the two apart costs
// nothing once the live directories are already in hand.
func (m Model) sweepingPopups() tea.Cmd {
	if m.harness.Popups == nil {
		return nil
	}
	sweep := m.harness.Popups.Sweep
	// Live is a Session's own directory and a repo header's Main root alike:
	// a repo can be on the rail on Activity alone, with no live Session in
	// it at all, and a popup opened over its header row is exactly as live
	// as one opened over a Session's own pane.
	liveDirs := append(dirsOf(m.set), m.working...)
	return func() tea.Msg {
		statuses, err := sweep(liveDirs)
		if err != nil {
			// A sweep that failed said nothing about any popup, busy or
			// not — unlike Cautions, which always has an answer worth
			// laying over the last one, there is nothing here to lay
			// anywhere. Reporting no message at all leaves m.popups
			// exactly as it was, which is the only account of a running
			// popup that could still be true.
			return nil
		}
		return PopupStatuses(statuses)
	}
}

// dirsOf is the directory every live Session is working in.
func dirsOf(sessions []session.Session) []string {
	dirs := make([]string, len(sessions))
	for i, s := range sessions {
		dirs[i] = s.Dir
	}
	return dirs
}

// readingUnread asks git about the roots on the rail if any of them has never
// been asked about, and about nothing while an answer is already on its way.
func (m Model) readingUnread() tea.Cmd {
	if m.awaiting {
		return nil
	}
	for _, root := range m.railed() {
		if _, asked := m.cautions[root]; !asked {
			return m.reading()
		}
	}
	return nil
}

// railed is every Main root with a row on the rail.
func (m Model) railed() []string {
	roots := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		if r.session == nil {
			roots = append(roots, r.root)
		}
	}
	return roots
}

// laidOver is what the Dashboard knows about the roots on the rail once read has
// been laid over it.
//
// Two reads can land in the order they were asked in or the other one, and
// nothing here has a say in which — so an answer takes the place of the answer
// about the same root and of nothing else. A root the read does not mention
// keeps what was last said about it, and is asked about again; a root that has
// left the rail is let go of, which is the only thing that ever empties this.
func (m Model) laidOver(read Cautions) Cautions {
	known := make(Cautions, len(read))
	for _, root := range m.railed() {
		if now, answered := read[root]; answered {
			known[root] = now
			continue
		}
		if was, asked := m.cautions[root]; asked {
			known[root] = was
		}
	}
	return known
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

// checkoutOf is repo.Checkout, remembering what it answered — for the same
// reason as the root, and as permanently: a Session's directory is a worktree
// or it is not, and it does not become the other one while the Session is
// running in it.
func (m Model) checkoutOf(dir string) string {
	if checkout, known := m.checkouts[dir]; known {
		return checkout
	}
	checkout := repo.Checkout(dir)
	m.checkouts[dir] = checkout
	return checkout
}

// cautionOf is what git last said a Main root is carrying, and whether it
// has been asked at all. Unlike everything else a row is built from, it is
// not asked here: a root nobody has asked about yet carries nothing, and
// says so until the answer arrives — the second return is what tells that
// silence apart from a root git has actually looked at and found clean.
func (m Model) cautionOf(root string) (repo.Caution, bool) {
	caution, known := m.cautions[root]
	return caution, known
}

// popupOf is what the harness last swept a directory's hidden Popup shell
// doing. Like cautionOf, it is not asked here: a directory with no popup at
// all, or one the harness has not swept yet, reads as idle rather than busy.
func (m Model) popupOf(dir string) popup.Status { return m.popups[dir] }

// frozenOf says whether the Session with this id is behind a pane holding a
// mode over the live view. A Session nothing has been said about is not
// frozen, which is the right default: the mark claims something, and its
// absence claims nothing.
func (m Model) frozenOf(id string) bool { return m.frozen[id] }

// selectedDir is the directory the cursor is currently on: a Session's own
// on a Session row, or a repo header's Main root when it is not on one. It
// is what a popup opened with focus on the rail has to be told, since the
// rail has no pane of its own to answer that question.
func (m Model) selectedDir() string {
	if m.cursor >= len(m.rows) {
		return ""
	}
	if r := m.rows[m.cursor]; r.session != nil {
		return r.session.Dir
	}
	return m.rows[m.cursor].root
}

// noting tells the harness which directory the cursor is on, so that a popup
// opened from the rail opens in the right one.
//
// Skipped when nothing has moved: the tree is rebuilt several times a second
// while an agent is working, and telling tmux the same directory it was just
// told would be the same waste the strip's own write-guard exists to avoid.
func (m Model) noting() Model {
	if m.harness.Popups == nil {
		return m
	}
	dir := m.selectedDir()
	if dir == "" || dir == m.selectedWritten {
		return m
	}
	if err := m.harness.Popups.Selected(dir); err != nil {
		return m
	}
	m.selectedWritten = dir
	return m
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
	case m.spawning != nil:
		return m.spawningKey(msg)
	case m.prompting != nil:
		return m.promptKey(msg)
	case m.ending != nil:
		return m.endingKey(msg)
	case m.claiming != nil:
		return m.claimingKey(msg), nil
	case m.takingOver != nil:
		return m.takingOverKey(msg)
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
		case "w":
			m = m.spawn()
		case "p":
			m = m.startPrompt()
		case "y":
			return m.approve()
		case "n":
			return m.deny()
		case "x":
			return m.interrupt()
		case "q":
			m = m.startEnd()
		case "c":
			return m.claim()
		}
	}
	return m.noting(), nil
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
	return m.jumpTo(*selected.session, true)
}

// jumpTo puts s in front of you: the pane it is running in, and the moment
// it counts as seen. It is jump's own work over the selected row, factored
// out so the guard's asynchronous fallback (§7.2) can focus the exact
// Session it tried to answer, by the Session itself rather than whichever
// row the cursor has since moved to.
//
// moveFocus is true only for the direct Enter gesture in jump(): the async
// fallback shares this same call but must never steal keyboard focus from
// whatever you're doing on the Dashboard when it fires.
func (m Model) jumpTo(s session.Session, moveFocus bool) Model {
	if m.harness.Jumper == nil {
		return m
	}
	if err := m.harness.Jumper.Jump(s.PID); err != nil {
		// A jump that could not be made left you where you were, so the
		// Session has not been seen and its badge stays.
		m.notice = err.Error()
		return m
	}
	if m.harness.Seen != nil {
		m.harness.Seen(s.ID)
	}
	if moveFocus && m.harness.Focuser != nil {
		_ = m.harness.Focuser.Focus()
	}
	return m
}

// focusPane puts pid's own pane in front of you without counting it as seen
// — the honest fallback for a prompt-send the guard could not verify
// (§7.2). Sending is what earns a Ready Session its Ready clear, not merely
// being shown the pane after a delivery nobody could confirm, so unlike
// jumpTo this never touches Seen; and the notice already on show is the
// guard's own explanation of what went wrong, so a jump that also fails is
// left unsaid rather than overwriting it.
func (m Model) focusPane(pid int) Model {
	if m.harness.Jumper != nil {
		_ = m.harness.Jumper.Jump(pid)
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
	if m.harness.Focuser != nil {
		_ = m.harness.Focuser.Focus()
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
	// A caution is not a state either. It is the mock's amber, which is loud
	// enough to catch an eye running down the rail and quiet enough not to be
	// mistaken for a Session that has stopped.
	cautionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	// Claimed reads in the same amber as a caution — the validated mock's own
	// choice, and not a coincidence worth a second colour: both are the
	// harness telling you something worth a second look before a PR goes in.
	claimedStyle = cautionStyle
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
		return m.repoLine(r, i == m.cursor)
	}

	// Two columns of indent put a Session under its repo; then the state
	// glyph, which is what the eye runs down; then the ticket and the age at
	// the far end. The age is what the ordering within a tier is made of — the
	// row above has been waiting on you longer, and the rail should be able to
	// show that — and the ticket is what tells two Sessions in one repo apart
	// before their names do.
	const indent = "  "
	glyph := r.session.State.Frame(m.spinner)
	age := ageOf(*r.session)
	mark := marks(r)
	name := truncate(r.session.Name, m.width-lipgloss.Width(indent+glyph+" "+mark)-lipgloss.Width(about(r.ticket)+" "+age)-1)
	if i == m.cursor {
		return selectedStyle.Width(m.width).Render(spread(indent+glyph+" "+mark+name, about(r.ticket)+" "+age, m.width))
	}
	return spread(indent+styleOf(r.session.State).Render(glyph)+" "+mark+name,
		ticketStyle(r.ticket).Render(about(r.ticket))+" "+quietStyle.Render(age), m.width)
}

// marks are what you have done to a row, as against what its Session is
// doing: that its pane is frozen, and that its hidden Popup shell is running
// something (§8). They come with the trailing space that separates them from
// whatever follows, and a row carrying neither costs the layout nothing.
//
// Frozen comes first. Whether the pane is still showing you the live Session
// changes what the rest of the row means; what a popup underneath it is
// running is a footnote to that.
func marks(r row) string {
	var said []string
	if r.frozen {
		said = append(said, frozen)
	}
	if r.popup.Busy {
		said = append(said, popupBusy)
	}
	if len(said) == 0 {
		return ""
	}
	return strings.Join(said, " ") + " "
}

// repoLine draws a repo's header row: its name, and at the far end the mark of
// what its Main root is doing. The mark is the answer to the question the rail
// is asked most often about a repo — whether a PR can be checked out in it —
// and it sits in the same column on every header row, so that running an eye
// down the rail reads as a list of roots you can and cannot have.
// The cautions its checkout is carrying go in front of the mark, because they
// are read in that order: what the root is, then what is in it.
func (m Model) repoLine(r row, selected bool) string {
	glyph := m.repoGlyph(r)
	mark := strings.TrimRight(marks(r), " ")
	// What the row has left for a caution once the name, the mark and the
	// state glyph have had their columns. Where that leaves too little, the
	// caution says less rather than nothing, and the name is truncated to
	// make room for what is left.
	warning := carrying(r.caution, m.width-lipgloss.Width(r.label()+glyph+mark)-2)
	// Nothing is styled until there is something to style: a style applied to an
	// empty string is escape codes around nothing, which is empty to the eye and
	// a string with something in it to everything that measures one.
	marks := glyph
	if selected {
		if warning != "" {
			marks = warning + " " + glyph
		}
		if mark != "" {
			marks += " " + mark
		}
		return selectedStyle.Width(m.width).Render(spread(r.label(), marks, m.width))
	}
	marks = rootStyle(r.state).Render(glyph)
	if warning != "" {
		marks = cautionStyle.Render(warning) + " " + marks
	}
	if mark != "" {
		marks += " " + mark
	}
	return spread(repoStyle.Render(r.label()), marks, m.width)
}

// carrying is how a Main root's cautions read on its row, in the room the row
// has left for them.
//
// It says as much as fits and never part of a word: the whole of it, then the
// branch name shortened, then the marks without the branch at all, and at the
// very least the mark itself. That last one is said whether there is room for it
// or not, and the name gives the column up for it — a caution dropped for want
// of space would leave a root that is detached with work in it reading exactly
// like one that is clean, on the row you are looking at to find out which.
func carrying(c repo.Caution, room int) string {
	where := c.Branch
	if c.Detached {
		// A commit checked out by hash has no name to give, and the state
		// itself is the whole of what is worth saying about it.
		where = "detached"
	}
	dirty := ""
	if c.Dirty {
		dirty = "dirty"
	}
	if where == "" && dirty == "" {
		return ""
	}

	tail := ""
	if dirty != "" {
		tail = " · " + dirty
	}
	said := []string{caution + " " + dirty}
	if where != "" {
		said = []string{caution + " " + where + tail}
		// A branch name cut down to two or three columns says nothing at all,
		// so below that the row stops trying to name it.
		if spare := room - lipgloss.Width(caution+" "+tail); spare >= 4 {
			said = append(said, caution+" "+elide(where, spare)+tail)
		}
		if dirty != "" {
			said = append(said, caution+" "+dirty)
		}
	}
	for _, marks := range said {
		if lipgloss.Width(marks) <= room {
			return marks
		}
	}
	return caution
}

// rootStyle is how a Main root's state is drawn: a root with an agent in it
// reads in the agents' own colour, because an agent is what has it; a root
// you have claimed in the same amber every other Claimed mark reads in; and a
// free one in the quiet the sidepanel keeps for everything that is not asking
// anything of you.
func rootStyle(state repo.State) lipgloss.Style {
	switch state {
	case repo.InUse:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(session.Working.Colour()))
	case repo.Claimed:
		return claimedStyle
	}
	return quietStyle
}

// repoGlyph is a Main root's mark at the current frame: an InUse root whose
// holder is Working borrows Working's own animated mark — the same borrowing
// rootStyle already makes for its colour. An InUse root held by an Idle,
// Ready, Blocked, or Shell Session, and every other state, stand still on
// Glyph.
func (m Model) repoGlyph(r row) string {
	if r.state == repo.InUse && r.holderWorking {
		return session.Working.Frame(m.spinner)
	}
	return r.state.Glyph()
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
	if m.spawning != nil {
		return m.spawningView()
	}
	if m.prompting != nil {
		return m.promptingView()
	}
	if m.ending != nil {
		return m.endingView()
	}
	if m.claiming != nil {
		return m.claimingView()
	}
	if m.takingOver != nil {
		return m.takingOverView()
	}
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
		// The marks on the row in words: whether a PR can be checked out here is
		// the question a repo is on the rail to answer, and the box is where the
		// answer is spelled rather than drawn.
		lines := []string{
			repoStyle.Render(truncate(r.label(), m.width)),
			rootStyle(r.state).Render(m.repoGlyph(r)) + " " + truncate("root: "+string(r.state), m.width-2),
		}
		if r.state == repo.Claimed && r.claimNote != "" {
			lines = append(lines, quietStyle.Render(truncate("note: "+r.claimNote, m.width)))
		}
		lines = append(lines, m.carrying(r.caution)...)
		if r.state == repo.Claimed && r.cautionKnown && !r.caution.Any() {
			// The nudge a Claimed root earns once the review it was reserved
			// for looks done from here: back on the default branch, nothing
			// uncommitted left in it. Gated on cautionKnown too — a root git
			// has not actually been asked about yet must never read as
			// clean by default.
			lines = append(lines, claimedStyle.Render(truncate("clean on default — release?", m.width)))
		}
		return append(lines,
			quietStyle.Render(shorten(r.root, m.width)),
			quietStyle.Render(m.repoOffering(r)))
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
		quietStyle.Render(offering(r, m.width)),
	)
}

// carrying spells out what a Main root's checkout is carrying, a line each and
// in full — which is what the row above had no room for. The branch is named
// there because "off the default branch" is where the question starts and
// "which branch" is where it ends.
func (m Model) carrying(c repo.Caution) []string {
	var lines []string
	switch {
	case c.Detached:
		lines = append(lines, "detached — checked out by hash")
	case c.Branch != "":
		lines = append(lines, "off default: "+c.Branch)
	}
	if c.Dirty {
		lines = append(lines, "uncommitted changes")
	}
	for i, line := range lines {
		// Elided rather than cut: a branch name that runs off the end of the box
		// as well would leave you unable to tell how much of it you are reading.
		lines[i] = cautionStyle.Render(elide(caution+" "+line, m.width))
	}
	return lines
}

// offering is what the selected row can be asked to do, in the order it
// matters most: the jump every row offers, then the row's own guarded
// actions, then the ticket's — greedily filling width with whole keys and no
// more. A Session about no ticket is not offered a link to open, since there
// is none — but it is always offered the key that gives it one. Approve and
// deny only ever apply to a Session that cannot continue without you (§7.3)
// — anything else has nothing on this row to say yes or no to.
//
// A row with more keys than width has room for drops them whole off the
// tail rather than cutting the line off mid-word: "t ticke" left hanging
// reads as a Dashboard that has glitched, not one that ran out of room on
// purpose — and the keys that matter most (jump, the row's own action) are
// the ones offered first, so they are the last to give way.
func offering(r row, width int) string {
	keys := []string{"⏎ jump"}
	switch r.session.State {
	case session.Blocked:
		keys = append(keys, "y approve", "n deny")
	case session.Idle, session.Ready:
		keys = append(keys, "p prompt", "q end")
	case session.Working:
		keys = append(keys, "p queue", "x interrupt")
	}
	keys = append(keys, "t ticket")
	if r.ticket != "" {
		keys = append(keys, "o open")
	}
	return fitKeys(keys, width)
}

// fitKeys is offering's own greedy fit, shared with a repo header's row
// (repoOffering): as many whole keys as fit, in the order they matter most,
// and never one cut off mid-word.
func fitKeys(keys []string, width int) string {
	var line string
	for _, key := range keys {
		next := key
		if line != "" {
			next = line + " · " + key
		}
		if lipgloss.Width(next) > width {
			break
		}
		line = next
	}
	return line
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
