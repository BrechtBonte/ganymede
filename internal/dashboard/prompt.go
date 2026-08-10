package dashboard

import (
	"strings"

	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// Prompter is the guarded send-keys engine's Idle/Ready/Working consumer:
// deliver a prompt into a Session's own input box, or on a Working Session
// interrupt its current turn first (§7.2, §7.3). The spec's own Ctrl+Enter
// row is bound to alt+⏎ here instead — bubbletea has no way to tell
// Ctrl+Enter apart from plain Enter without a Kitty-keyboard-protocol input
// reader, which this dependency does not have, while alt+⏎ is the one
// modified Enter it already parses natively.
type Prompter interface {
	Send(pid int, text string) error
	InterruptAndSend(pid int, text string) error
}

// prompting is the prompt-from-dashboard input open over a Session: which
// one it is about — by pid, registry id and the state it was opened on,
// since a Working Session's box reads "will queue" and offers alt+⏎ where an
// Idle or Ready one does not — and what has been typed into it so far.
//
// It holds these rather than the row, for the same reason the ticket-setting
// input holds a checkout instead of one: the working set is rebuilt out from
// under this dialog every time any Session anywhere changes state, and the
// row it was opened over may have moved or gone by the time Enter is
// pressed.
type prompting struct {
	pid   int
	id    string
	state session.State
	name  string
	typed string
}

// with appends text to the input.
func (p *prompting) with(text string) *prompting {
	typed := *p
	typed.typed += text
	return &typed
}

// back drops the last character of the input, the same rune-boundary trim
// spawning's own inputs already share (picker.go's trimRune).
func (p *prompting) back() *prompting {
	typed := *p
	typed.typed = trimRune(typed.typed)
	return &typed
}

// startPrompt opens the input over the selected Session: offered only on
// Idle, Ready and Working rows (§7.3) — never Blocked, where Enter would
// answer the dialog instead, and never Shell, where you are the occupant.
// A Session with a delivery already in flight offers nothing either: the
// same registry gate respond() applies before y or n ever touch tmux, since
// reopening the dialog on top of a pending send would let a second prompt in
// behind the first without either being reported.
func (m Model) startPrompt() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	r := m.rows[m.cursor]
	if r.session == nil {
		return m
	}
	s := *r.session
	switch s.State {
	case session.Idle, session.Ready, session.Working:
	default:
		return m
	}
	if m.pending[s.PID] {
		return m
	}
	m.prompting = &prompting{pid: s.PID, id: s.ID, state: s.State, name: s.Name}
	return m
}

// promptKey is the prompt dialog's own key handling. While it is up every
// key belongs to it, the same way the ticket-setting input owns the
// keyboard.
func (m Model) promptKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Abandoned: nothing is sent, and the row goes on saying what it said.
		m.prompting = nil
	case tea.KeyRunes:
		m.prompting = m.prompting.with(string(msg.Runes))
	case tea.KeySpace:
		m.prompting = m.prompting.with(" ")
	case tea.KeyBackspace:
		m.prompting = m.prompting.back()
	case tea.KeyEnter:
		// alt+⏎ only ever means interrupt-then-send on a Working row — the one
		// row promptingView offers it on. Anywhere else there is no turn to
		// interrupt, so it falls back to the plain send every other row uses.
		if msg.Alt && m.prompting.state == session.Working {
			return m.interruptSend()
		}
		return m.send()
	}
	return m, nil
}

// sent is what the guard reports once it has tried to deliver a prompt: the
// pid and registry id it was asked to reach, so a mismatch can still focus
// the exact pane it could not verify and, on success, clear that Session's
// Ready badge — even though the cursor may have moved on by the time the
// answer comes back.
type sent struct {
	pid int
	id  string
	err error
}

// send fires the guarded prompt-send for the open dialog: what plain Enter
// does on an Idle or Ready Session, and on a Working one too — Claude Code's
// own queuing is what tells "start a turn" and "queue behind one" apart, not
// anything the guard has to know about (§7.2, §7.3).
func (m Model) send() (Model, tea.Cmd) {
	return m.delivering(func(pid int, text string) error { return m.harness.Prompter.Send(pid, text) })
}

// interruptSend fires the guarded interrupt-then-send for the open dialog:
// alt+⏎ on a Working Session (§7.3's Ctrl+Enter row — see Prompter's own
// note on why alt+⏎ is bound instead).
func (m Model) interruptSend() (Model, tea.Cmd) {
	return m.delivering(func(pid int, text string) error { return m.harness.Prompter.InterruptAndSend(pid, text) })
}

// delivering is what send and interruptSend share: the registry gate the
// guard's first step asks for (§7.2) is already behind it by the time the
// dialog is open — startPrompt only ever opens over an Idle, Ready or
// Working row — so what is left is not sending twice while one delivery is
// still in flight, and not sending nothing at all.
//
// The send itself runs off the main loop, the same reason y and n do
// (respond): capture-pane, the paste and its own re-verify, send-keys and a
// last re-verify are tmux round trips and the redraws they wait out, and a
// Dashboard that waited on all of that inline would freeze over every
// keystroke.
func (m Model) delivering(deliver func(pid int, text string) error) (Model, tea.Cmd) {
	if m.prompting == nil {
		return m, nil
	}
	if m.harness.Prompter == nil {
		// The same word a Spawn with no Spawner wired gets (spawn.go's launch):
		// the dialog closes rather than sitting there swallowing every
		// keystroke with nothing to explain why Enter did nothing.
		m.notice = "no prompt delivery is configured"
		m.prompting = nil
		return m, nil
	}
	p := *m.prompting
	text := strings.TrimSpace(p.typed)
	if text == "" {
		return m, nil
	}
	if m.pending == nil {
		m.pending = map[int]bool{}
	}
	if m.pending[p.pid] {
		return m, nil
	}
	m.pending[p.pid] = true
	m.prompting = nil
	return m, func() tea.Msg {
		return sent{pid: p.pid, id: p.id, err: deliver(p.pid, text)}
	}
}

// promptingView is the SELECTED box for as long as the prompt dialog is
// open: the Session it is about, the input with the cursor drawn over it,
// and the keys it offers — "will queue" and alt+⏎ only on a Working Session,
// since Enter starts a turn everywhere else.
func (m Model) promptingView() []string {
	p := m.prompting
	label, hint := "prompt", "⏎ send · esc cancel"
	if p.state == session.Working {
		label, hint = "will queue", "⏎ queue · alt+⏎ interrupt+send · esc cancel"
	}
	return []string{
		elide(p.name, m.width),
		ticketColour.Render(tail(label+" › "+p.typed+"▌", m.width)),
		quietStyle.Render(truncate(hint, m.width)),
	}
}
