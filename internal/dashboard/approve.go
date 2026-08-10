package dashboard

import (
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// Approver is the guarded send-keys engine's first two consumers: the
// default-row answer to a Blocked Session's dialog, and the decline (§7.2,
// §7.3). Both take the row's own Reason, which is what the guard checks the
// pane against before it sends anything.
type Approver interface {
	Approve(pid int, reason string) error
	Deny(pid int, reason string) error
}

// answered is what the guard reports once it has tried to act: the Session
// it was asked to answer, so a mismatch can still focus the exact pane it
// could not verify even if the cursor has since moved on, and whatever the
// guard could not confirm.
type answered struct {
	session session.Session
	err     error
}

// approve answers the selected Session's dialog with the guard's default row
// (§7.3: y = Y, the dialog's own default).
func (m Model) approve() (Model, tea.Cmd) {
	return m.respond(func(pid int, reason string) error { return m.harness.Approver.Approve(pid, reason) })
}

// deny declines the selected Session's dialog (§7.3: n = Esc).
func (m Model) deny() (Model, tea.Cmd) {
	return m.respond(func(pid int, reason string) error { return m.harness.Approver.Deny(pid, reason) })
}

// respond is what y and n share: the registry gate the guard's first step
// asks for (§7.2) — a Session selected, Blocked, and timestamped by
// something the registry actually reported, with no answer already in
// flight for it — before send is trusted to touch tmux at all. A row that
// fails that gate is a key that was never offered here in the first place,
// and gets the same nothing a repo header would.
//
// The send itself runs off the main loop: capture-pane, send-keys and the
// re-verify are three tmux round trips and the redraw the last of them
// waits out, and a Dashboard that waited on all of that inline would freeze
// the whole sidepanel over it on every single keystroke.
func (m Model) respond(send func(pid int, reason string) error) (Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if r.session == nil || m.harness.Approver == nil {
		return m, nil
	}
	s := *r.session
	if s.State != session.Blocked || s.Since.IsZero() || m.pending[s.PID] {
		return m, nil
	}
	if m.pending == nil {
		m.pending = map[int]bool{}
	}
	m.pending[s.PID] = true
	return m, func() tea.Msg {
		return answered{session: s, err: send(s.PID, s.Reason)}
	}
}
