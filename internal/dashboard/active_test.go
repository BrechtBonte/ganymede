package dashboard_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
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
