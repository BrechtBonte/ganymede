package dashboard

import (
	"strings"

	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// claiming is the Claim dialog open over a Free repo header: the note being
// typed, and the busy-popup mention (§8) to carry through to claimingView.
//
// It holds the root and label rather than the row, for the same reason every
// other inline dialog does (spawning's own note): the working set is rebuilt
// out from under this dialog every time any Session anywhere moves.
type claiming struct {
	root, label string
	note        string
	popupNote   string
}

// with appends text to the note.
func (c *claiming) with(text string) *claiming {
	typed := *c
	typed.note += text
	return &typed
}

// back drops the last character of the note.
func (c *claiming) back() *claiming {
	typed := *c
	typed.note = trimRune(typed.note)
	return &typed
}

// takingOver is the Takeover confirmation open over an InUse repo header
// whose only occupant is Idle: which Session it would end, held by pid and
// name rather than the row, so the confirmation can still name it — and
// still act on the right process — after the working set has moved on.
//
// note is whatever the root was already claimed with, read at the moment
// the confirmation opens rather than left to takeover's own Claim — a root
// can carry a Claim underneath a live occupant (state.go's own documented
// collision, since a live occupant always outranks a Claim on the state a
// row draws), and a Takeover ending that occupant must not wipe a note it
// never asked about.
type takingOver struct {
	root, label string
	pid         int
	name        string
	note        string
	popupNote   string
}

// tookOver is what the guard reports once it has tried a Takeover: ending
// the occupant and, only once that succeeded, claiming the root behind it.
//
// endFailed tells the two ways this can go wrong apart. End itself never
// landing is the ordinary guard mismatch, with the pane still there to focus
// (§7.2's honest fallback). End succeeding and the Claim behind it failing
// is a different shape entirely — the occupant is genuinely gone, so there
// is no pane left to focus, and the root is left Free rather than Claimed.
type tookOver struct {
	pid       int
	root      string
	endFailed bool
	err       error
}

// claim is the free key (§4.2, §7.3): what it does depends on the state the
// selected repo's root is already in — open the Claim dialog on a Free root,
// release at once on one you have already claimed, or open the Takeover
// confirmation on an InUse root whose only occupant is Idle. Anything else —
// a Session row, or an InUse root Takeover does not apply to — is refused
// the same way q is refused on a row it does not apply to: silently, because
// the row never offered the key in the first place.
func (m Model) claim() (Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if r.session != nil {
		return m, nil
	}

	switch r.state {
	case repo.Free:
		m.claiming = &claiming{root: r.root, label: r.label(), popupNote: popupMention(r)}
		return m, nil
	case repo.Claimed:
		return m.released(r.root), nil
	case repo.InUse:
		target, ok := takeoverTarget(m.rows, m.cursor)
		if !ok {
			return m, nil
		}
		var note string
		if m.harness.Claimer != nil {
			note, _ = m.harness.Claimer.NoteOf(r.root)
		}
		m.takingOver = &takingOver{root: r.root, label: r.label(), pid: target.PID, name: target.Name, note: note, popupNote: popupMention(r)}
		return m, nil
	}
	return m, nil
}

// popupMention is the busy-popup mention a Claim or Takeover confirmation
// carries (§8: "popup running: composer install") — empty when the header's
// own hidden Popup shell is not running anything worth mentioning.
func popupMention(r row) string {
	if !r.popup.Busy {
		return ""
	}
	return "popup running: " + r.popup.Command
}

// occupantsOf is the Sessions actually holding root — as against every
// Session merely grouped under the repo, a Worktree session included.
// rowsOf lays a repo header's own Sessions out contiguously right after it,
// so reading them costs nothing the tree has not already paid for.
func occupantsOf(rows []row, header int) []row {
	var occupants []row
	for i := header + 1; i < len(rows) && rows[i].session != nil; i++ {
		if rows[i].holdsRoot {
			occupants = append(occupants, rows[i])
		}
	}
	return occupants
}

// takeoverTarget is the sole Idle Session actually holding an InUse root,
// and whether there is exactly one — refused otherwise (§4.2: "claiming a
// root whose only occupant is an Idle session... Refused when the occupant
// is Working or Blocked"), and refused the same way when more than one
// Session holds the root at once.
func takeoverTarget(rows []row, header int) (session.Session, bool) {
	occupants := occupantsOf(rows, header)
	if len(occupants) != 1 || occupants[0].session.State != session.Idle {
		return session.Session{}, false
	}
	return *occupants[0].session, true
}

// released fires Release for root: no confirmation, the same low-ceremony
// gesture a toggle gets — releasing costs you nothing you were not already
// prepared to let go of, unlike ending a Session.
func (m Model) released(root string) Model {
	if m.harness.Claimer == nil {
		m.notice = "no root claiming is configured"
		return m
	}
	if err := m.harness.Claimer.Release(root); err != nil {
		m.notice = err.Error()
		return m
	}
	return m.rebuilt()
}

// claimingKey is the Claim dialog's own key handling. While it is up every
// key belongs to it, the same way the ticket-setting input owns the
// keyboard.
func (m Model) claimingKey(msg tea.KeyMsg) Model {
	switch msg.Type {
	case tea.KeyEsc:
		// Abandoned: nothing is claimed, and the root is left as it was.
		m.claiming = nil
	case tea.KeyEnter:
		m = m.claimed()
	case tea.KeyRunes:
		m.claiming = m.claiming.with(string(msg.Runes))
	case tea.KeySpace:
		m.claiming = m.claiming.with(" ")
	case tea.KeyBackspace:
		m.claiming = m.claiming.back()
	}
	return m
}

// claimed fires Claim for the open dialog's root, with whatever note was
// typed — empty is a Claim with none, which is still a Claim.
func (m Model) claimed() Model {
	c := m.claiming
	if m.harness.Claimer == nil {
		m.notice = "no root claiming is configured"
		m.claiming = nil
		return m
	}
	if err := m.harness.Claimer.Claim(c.root, strings.TrimSpace(c.note)); err != nil {
		m.notice = err.Error()
		m.claiming = nil
		return m
	}
	m.claiming = nil
	return m.rebuilt()
}

// claimingView is the SELECTED box for as long as the Claim dialog is open:
// the repo it is about, the busy-popup mention when there is one, and the
// note field with the cursor drawn over it.
func (m Model) claimingView() []string {
	c := m.claiming
	lines := []string{elide(c.label, m.width)}
	if c.popupNote != "" {
		lines = append(lines, quietStyle.Render(truncate(c.popupNote, m.width)))
	}
	lines = append(lines, ticketColour.Render(tail("note › "+c.note+"▌", m.width)))
	return append(lines, quietStyle.Render(truncate("⏎ claim · esc cancel", m.width)))
}

// takingOverKey is the Takeover confirmation's own key handling.
func (m Model) takingOverKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Abandoned: nothing is ended, and nothing is claimed.
		m.takingOver = nil
	case tea.KeyEnter:
		return m.takeover()
	}
	return m, nil
}

// takeover fires the guarded End against the confirmed occupant and, only
// once that has actually succeeded, Claims the root behind it with whatever
// note it already carried — "ends that session and claims in one action"
// (§4.2).
//
// The send runs off the main loop, the same reason every other guarded
// action does (approve.go's respond, stop.go's end): capture-pane, the
// paste and its own re-verify, send-keys and a last re-verify are tmux round
// trips and the redraws they wait out.
func (m Model) takeover() (Model, tea.Cmd) {
	t := m.takingOver
	if t == nil {
		return m, nil
	}
	if m.harness.Stopper == nil || m.harness.Claimer == nil {
		m.notice = "no Takeover is configured"
		m.takingOver = nil
		return m, nil
	}
	if m.pending[t.pid] {
		return m, nil
	}
	if m.pending == nil {
		m.pending = map[int]bool{}
	}
	m.pending[t.pid] = true
	m.takingOver = nil
	stopper, claimer := m.harness.Stopper, m.harness.Claimer
	root, pid, note := t.root, t.pid, t.note
	return m, func() tea.Msg {
		if err := stopper.End(pid); err != nil {
			return tookOver{pid: pid, root: root, endFailed: true, err: err}
		}
		return tookOver{pid: pid, root: root, err: claimer.Claim(root, note)}
	}
}

// stoppedTakeover is what the guard reports once it has tried a Takeover.
//
// The two ways this can fail are told apart rather than answered alike.
// endFailed is the ordinary guard mismatch (§7.2's honest fallback): the
// occupant never actually left, so its pane is still there to focus. A
// Claim failing after End has already succeeded is a different shape — the
// occupant is genuinely gone, there is no pane left to send a jump to, and
// the root is left Free rather than Claimed, which the notice says plainly
// rather than pointing you at a pid that no longer exists.
func (m Model) stoppedTakeover(msg tookOver) (Model, tea.Cmd) {
	delete(m.pending, msg.pid)
	switch {
	case msg.err == nil:
		return m.rebuilt(), nil
	case msg.endFailed:
		m.notice = msg.err.Error()
		return m.focusPane(msg.pid), nil
	default:
		m.notice = "ended the session, but could not claim the root: " + msg.err.Error()
		return m.rebuilt(), nil
	}
}

// takingOverView is the SELECTED box for as long as the Takeover
// confirmation is open: the repo it is about, the busy-popup mention when
// there is one, and which Session ending would end.
func (m Model) takingOverView() []string {
	t := m.takingOver
	lines := []string{elide(t.label, m.width)}
	if t.popupNote != "" {
		lines = append(lines, quietStyle.Render(truncate(t.popupNote, m.width)))
	}
	lines = append(lines, styleOf(session.Idle).Render(truncate("end "+t.name+" and claim this root?", m.width)))
	return append(lines, quietStyle.Render(truncate("⏎ takeover · esc cancel", m.width)))
}

// repoOffering is a repo header row's own offering (dashboard.go's offering,
// for a Session row): the jump and spawn every header gets, and the free
// key's own label read off the state the root is actually in — omitted
// entirely on an InUse root Takeover does not apply to, since offering a key
// that would silently do nothing is worse than not offering it.
func (m Model) repoOffering(r row) string {
	keys := []string{"⏎ go to repo", "w spawn"}
	switch r.state {
	case repo.Free:
		keys = append(keys, "c claim")
	case repo.Claimed:
		keys = append(keys, "c release")
	case repo.InUse:
		if _, ok := takeoverTarget(m.rows, m.cursor); ok {
			keys = append(keys, "c takeover")
		}
	}
	return fitKeys(keys, m.width)
}

// claimedWarning is the mention a spawn dialog carries when the repo it
// would spawn into has a Claim on it (§4.2: "warns agent spawns away") —
// empty when the root is not claimed, which is the ordinary case.
func (m Model) claimedWarning(root string) string {
	if m.harness.Claimer == nil {
		return ""
	}
	note, claimed := m.harness.Claimer.NoteOf(root)
	if !claimed {
		return ""
	}
	if note == "" {
		return "claimed"
	}
	return "claimed — " + note
}
