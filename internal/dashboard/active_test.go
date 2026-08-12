package dashboard_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

// Browsing the cursor away from a Session you just jumped to must not erase
// the one mark saying what the working client is actually showing right
// now — only the cursor's own highlight should move on.
func TestTheJumpedToSessionStaysMarkedAfterBrowsingAway(t *testing.T) {
	jumper := &jumps{}
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks it active
	model = press(model, tea.KeyUp)    // back to the repo header row

	line, ok := rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for the session:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("session row = %q, want it still marked dim-reverse as the active row", line)
	}

	header, ok := rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", drawn(model))
	}
	if !strings.HasPrefix(header, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want the cursor's own plain reverse", header)
	}
}

// The guard's own mismatch fires from a background send with no idea what
// you're doing on the Dashboard right now (approve.go's respond): the
// cursor can move on before the answer comes back, and the row jumpTo
// points at must still pick up the mark even though moveFocus is false and
// the cursor never went near it.
func TestTheGuardsApproveMismatchMarksItsRowEvenAfterTheCursorMovedOn(t *testing.T) {
	approver := &approvals{err: errors.New("pane does not show the dialog it was reported waiting on")}
	jumper := &jumps{}
	a := session.Session{PID: 111, ID: "sess-a", Dir: "/repos/service-billing",
		Name: "aaa-blocked", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	b := session.Session{PID: 222, ID: "sess-b", Dir: "/repos/service-billing",
		Name: "bbb-blocked", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	model := withApprover(approver, jumper, a, b)
	model = press(model, tea.KeyDown) // onto a's row

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y on a Blocked row asked for no guarded send")
	}

	model = press(model, tea.KeyDown) // browse onto b's row before the answer lands

	model, _ = model.Update(cmd())

	line, ok := rawLineFor(model, "aaa-blocked")
	if !ok {
		t.Fatalf("no row for aaa-blocked:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("aaa-blocked row = %q, want it marked as the row the working client is actually showing", line)
	}
}

// A jump that fails outright — the harness could not reach the pane at
// all, as against Gone's "the process itself has ended" — leaves the
// working client showing whatever it already was. The row it failed on
// must not be marked active over that.
func TestAJumpThatFailsDoesNotMarkItsRowActive(t *testing.T) {
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	// Different-length names on the same dir so live()'s PID (len(name) +
	// len(dir)) does not collide between the two — a and b must stay two
	// distinct Sessions for this test to mean anything.
	a := live("aaa-idle", "/repos/ganymede", session.Idle)
	b := live("bbb-idle-2", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, a, b)

	model = press(model, tea.KeyDown)  // onto a's row
	model = press(model, tea.KeyEnter) // jump fails
	model = press(model, tea.KeyDown)  // onto b's row

	line, ok := rawLineFor(model, "aaa-idle")
	if !ok {
		t.Fatalf("no row for aaa-idle:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) || strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("aaa-idle row = %q, want no mark left behind by a jump that failed", line)
	}
}

// The send guard's own mismatch (prompt.go's delivering/dashboard.go's
// `sent` case) reaches the pane through focusPane, not jumpTo, and shares
// the same property: the cursor can move on before the async answer lands,
// and the row it focuses must still pick up the mark.
func TestTheGuardsSendMismatchMarksItsRowEvenAfterTheCursorMovedOn(t *testing.T) {
	prompter := &prompts{err: errors.New("pane does not show an empty input box to send into")}
	jumper := &jumps{}
	// Different-length names on the same dir so live()'s PID (len(name) +
	// len(dir)) does not collide between the two.
	a := live("aaa-idle", "/repos/ganymede", session.Idle)
	b := live("bbb-idle-2", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, jumper, a, b)
	model = press(model, tea.KeyDown) // onto a's row

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter in the prompt dialog asked for no guarded send")
	}

	model = press(model, tea.KeyDown) // browse onto b's row before the send lands

	model, _ = model.Update(cmd())

	line, ok := rawLineFor(model, "aaa-idle")
	if !ok {
		t.Fatalf("no row for aaa-idle:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("aaa-idle row = %q, want it marked as the row the working client is actually showing", line)
	}
}

// Opening a bare repo (Enter on a repo header with nothing running, or the
// picker's Enter) points the working client at a directory with no Session
// in it at all, so no row should keep reading as active once that happens.
func TestOpeningABareRepoClearsTheActiveRow(t *testing.T) {
	jumper := &jumps{}
	opener := &opens{}
	state := remembering(t)
	worked(t, state, "/repos/other-repo", time.Now())
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := dashboardOn(dashboard.Harness{Jumper: jumper, Opener: opener, Activity: state}, only)

	model = press(model, tea.KeyDown)  // ganymede's header row onto its Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active
	model = press(model, tea.KeyDown)  // onto the bare other-repo's header row
	model = press(model, tea.KeyEnter) // goTo: opens it

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/other-repo" {
		t.Fatalf("opened %v, want [/repos/other-repo]", opener.dirs)
	}

	line, ok := rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for ganymede-78:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("ganymede-78 row = %q, want the active mark cleared once a bare repo is opened", line)
	}
}

// A pid Jump has confirmed Gone can be handed by the OS to an unrelated
// process later; the registry then reports that as a brand new Session
// that was never jumped to, and it must not inherit the old one's mark.
func TestForgettingAGoneSessionClearsTheActiveMarkForWhoeverReusesItsPid(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active

	jumper.err = topology.GoneError{PID: only.PID}
	model = press(model, tea.KeyEnter) // jump fails Gone: forgets and drops the row

	// The registry catches up and stops reporting the pid at all — the same
	// step TestAForgottenPidIsPrunedOnceItsSourceMovesOn uses. Without it,
	// withoutForgotten would suppress the reused Session below as a stale
	// re-report of the same dead one, and this test would never reach the
	// row it is actually about.
	model, _ = model.Update(dashboard.Sessions{})

	reused := session.Session{PID: only.PID, ID: "different-id", Dir: "/repos/ganymede",
		Name: "ganymede-new", State: session.Idle, Since: epoch}
	model, _ = model.Update(dashboard.Sessions{reused})

	line, ok := rawLineFor(model, "ganymede-new")
	if !ok {
		t.Fatalf("no row for the reused pid's Session:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("ganymede-new row = %q, want a forgotten pid's old active mark not carried onto whoever reuses it", line)
	}
}
