package dashboard

import (
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// Stopper is the guarded send-keys engine's stop-a-session consumer: a bare
// interrupt with no send behind it (§7.3's "x" row), and the graceful exit a
// confirmed "q" fires once the Dashboard's own dialog has been answered.
type Stopper interface {
	Interrupt(pid int) error
	End(pid int) error
}

// ending is the end-session confirmation open over an Idle or Ready row:
// which Session it is about, and whether its Ready badge means the
// confirmation has to carry the unread-output warning (§7.3).
//
// It holds these rather than the row, for the same reason every other
// inline dialog does (prompting's own note): the working set is rebuilt out
// from under this dialog every time any Session anywhere changes state.
type ending struct {
	pid    int
	name   string
	unread bool
}

// interrupted is what the guard reports once it has tried a bare interrupt:
// the pid it was asked to reach, so a mismatch can still focus the exact
// pane it could not verify.
type interrupted struct {
	pid int
	err error
}

// ended is what the guard reports once it has tried a graceful exit: the pid
// it was asked to reach, so a mismatch can still focus the exact pane it
// could not verify.
type ended struct {
	pid int
	err error
}

// stopped is what interrupted and ended share once the guard has tried a
// bare interrupt or a graceful exit: clear the pending mark, and on a
// mismatch focus the pane and say why — the same honest fallback every
// other guarded action gets (§7.2). x has no confirm dialog of its own, so
// for it this is the only word a dialog it found in the way ever gets.
func (m Model) stopped(pid int, err error) (Model, tea.Cmd) {
	delete(m.pending, pid)
	if err != nil {
		m.notice = err.Error()
		return m.focusPane(pid), nil
	}
	return m, nil
}

// interrupt fires the guarded bare-interrupt for the selected row: x on a
// Working Session (§7.3). It has no confirmation dialog of its own — the
// guard's own precondition (an empty input box, i.e. no dialog on the pane)
// plus a deliberate key is already the safety the spec asks for.
func (m Model) interrupt() (Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if r.session == nil || m.harness.Stopper == nil {
		return m, nil
	}
	s := *r.session
	if s.State != session.Working || m.pending[s.PID] {
		return m, nil
	}
	if m.pending == nil {
		m.pending = map[int]bool{}
	}
	m.pending[s.PID] = true
	return m, func() tea.Msg {
		return interrupted{pid: s.PID, err: m.harness.Stopper.Interrupt(s.PID)}
	}
}

// startEnd opens the end-session confirmation over the selected row: offered
// only on Idle and Ready (§7.3) — refused on Working and Blocked, where an
// interrupt has to come first, and on Shell, where you are the occupant.
func (m Model) startEnd() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	r := m.rows[m.cursor]
	if r.session == nil {
		return m
	}
	s := *r.session
	switch s.State {
	case session.Idle, session.Ready:
	default:
		return m
	}
	if m.pending[s.PID] {
		return m
	}
	m.ending = &ending{pid: s.PID, name: s.Name, unread: s.State == session.Ready}
	return m
}

// endingKey is the end-session dialog's own key handling. While it is up
// every key belongs to it, the same way every other inline dialog owns the
// keyboard.
func (m Model) endingKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Abandoned: nothing is sent, and the row goes on saying what it said.
		m.ending = nil
	case tea.KeyEnter:
		return m.end()
	}
	return m, nil
}

// end fires the guarded graceful exit for the confirmed dialog: paste /exit
// and Enter into the Session's own input box (§7.3).
//
// The send runs off the main loop, the same reason every other guarded
// action does (approve.go's respond, prompt.go's delivering): capture-pane,
// the paste and its own re-verify, send-keys and a last re-verify are tmux
// round trips and the redraws they wait out, and a Dashboard that waited on
// all of that inline would freeze the whole sidepanel over every single
// keystroke.
func (m Model) end() (Model, tea.Cmd) {
	if m.ending == nil {
		return m, nil
	}
	if m.harness.Stopper == nil {
		// The same word a Send with no Prompter gets (prompt.go's delivering):
		// the dialog closes rather than sitting there swallowing every
		// keystroke with nothing to explain why Enter did nothing.
		m.notice = "no session ending is configured"
		m.ending = nil
		return m, nil
	}
	e := *m.ending
	if m.pending[e.pid] {
		return m, nil
	}
	if m.pending == nil {
		m.pending = map[int]bool{}
	}
	m.pending[e.pid] = true
	m.ending = nil
	return m, func() tea.Msg {
		return ended{pid: e.pid, err: m.harness.Stopper.End(e.pid)}
	}
}

// endingView is the SELECTED box for as long as the end-session dialog is
// open: the Session it is about, the unread-output warning when its Ready
// badge means there is one, and the keys it offers.
func (m Model) endingView() []string {
	e := m.ending
	lines := []string{elide(e.name, m.width)}
	if e.unread {
		lines = append(lines, styleOf(session.Ready).Render(truncate("unread output — end this session anyway?", m.width)))
	} else {
		lines = append(lines, quietStyle.Render(truncate("end this session?", m.width)))
	}
	return append(lines, quietStyle.Render(truncate("⏎ end · esc cancel", m.width)))
}
