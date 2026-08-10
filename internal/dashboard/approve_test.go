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

// call is one pid and reason the guard was asked to answer.
type call struct {
	pid    int
	reason string
}

// approvals records every call the Dashboard asked to have answered,
// standing in for the guarded send-keys engine actually touching tmux.
type approvals struct {
	approved, denied []call
	err              error
}

func (a *approvals) Approve(pid int, reason string) error {
	a.approved = append(a.approved, call{pid, reason})
	return a.err
}

func (a *approvals) Deny(pid int, reason string) error {
	a.denied = append(a.denied, call{pid, reason})
	return a.err
}

// withApprover is a Dashboard sized for the sidepanel, showing sessions and
// wired to approver and jumper — the two hands y and n share between them.
func withApprover(approver dashboard.Approver, jumper dashboard.Jumper, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Approver: approver})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// blocked is a Dashboard with one Blocked Session selected — where y and n
// are pressed from.
func blocked(approver dashboard.Approver, jumper dashboard.Jumper) (tea.Model, session.Session) {
	s := session.Session{PID: 4242, ID: "sess-1", Dir: "/repos/service-billing",
		Name: "FIRE-2841-paging", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	model := withApprover(approver, jumper, s)
	model = press(model, tea.KeyDown)
	return model, s
}

// answering presses key and, when the guard has something to send, lets its
// answer come back before returning — the send itself runs off the main
// loop (respond), so a test has to run the command it was handed the same
// way the runtime would.
func answering(model tea.Model, key string) tea.Model {
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if cmd == nil {
		return model
	}
	model, _ = model.Update(cmd())
	return model
}

// y is the dialog's own default row: the whole point is answering it without
// ever leaving the Dashboard.
func TestYApprovesTheSelectedBlockedSession(t *testing.T) {
	approver := &approvals{}
	model, s := blocked(approver, &jumps{})

	model = answering(model, "y")

	if len(approver.approved) != 1 || approver.approved[0] != (call{s.PID, s.Reason}) {
		t.Errorf("approved %v, want [{%d %q}]", approver.approved, s.PID, s.Reason)
	}
}

// n is the decline — Esc under the guard, not N (§7.3).
func TestNDeniesTheSelectedBlockedSession(t *testing.T) {
	approver := &approvals{}
	model, s := blocked(approver, &jumps{})

	model = answering(model, "n")

	if len(approver.denied) != 1 || approver.denied[0] != (call{s.PID, s.Reason}) {
		t.Errorf("denied %v, want [{%d %q}]", approver.denied, s.PID, s.Reason)
	}
}

// A successful answer is inline: the whole point of y and n is that they
// never take you off the Dashboard the way Enter does.
func TestApprovingSucceedsWithoutJumpingAwayFromTheDashboard(t *testing.T) {
	jumper := &jumps{}
	model, _ := blocked(&approvals{}, jumper)

	model = answering(model, "y")

	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v after a successful approve, want to stay on the Dashboard", jumper.pids)
	}
}

// y and n are offered only on Blocked rows (§7.3) — nothing else on the
// Dashboard can be answered yes or no to.
func TestApproveAndDenyAreOfferedOnlyOnBlockedRows(t *testing.T) {
	for _, state := range []session.State{session.Working, session.Ready, session.Idle, session.Shell} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		if box := detail(model); strings.Contains(box, "approve") || strings.Contains(box, "deny") {
			t.Errorf("a %s Session offers approve/deny:\n%s", state, box)
		}
	}
}

// And are offered on the one row they apply to.
func TestApproveAndDenyAreOfferedOnABlockedRow(t *testing.T) {
	model, _ := blocked(&approvals{}, &jumps{})

	box := detail(model)
	if !strings.Contains(box, "y approve") || !strings.Contains(box, "n deny") {
		t.Errorf("SELECTED = %q, want approve and deny offered on a Blocked row", box)
	}
}

// The registry gate (§7.2 step 1): a Session that is not Blocked fails the
// action's precondition, and nothing is sent to it — the same as a key that
// was never offered on this row in the first place.
func TestYOnASessionThatIsNotBlockedDoesNothing(t *testing.T) {
	approver := &approvals{}
	jumper := &jumps{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withApprover(approver, jumper, working)
	model = press(model, tea.KeyDown)

	model = answering(model, "y")

	if len(approver.approved) != 0 {
		t.Errorf("approved %v, want nothing sent to a Session that is not Blocked", approver.approved)
	}
	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v, want a Session outside the action's precondition left alone", jumper.pids)
	}
}

// The other half of the same gate: a Blocked Session the registry never
// timestamped is one the guard cannot call fresh, and gets the same answer —
// nothing sent.
func TestYOnABlockedSessionWithNoTimestampDoesNothing(t *testing.T) {
	approver := &approvals{}
	jumper := &jumps{}
	untimed := live("ganymede-78", "/repos/ganymede", session.Blocked)
	untimed.Since = time.Time{}
	model := withApprover(approver, jumper, untimed)
	model = press(model, tea.KeyDown)

	model = answering(model, "y")

	if len(approver.approved) != 0 {
		t.Errorf("approved %v, want nothing sent with no statusUpdatedAt to trust", approver.approved)
	}
	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v, want nothing done rather than a guess", jumper.pids)
	}
}

// A row the gate already passed but the guard could not verify on the tmux
// side — a capture-pane mismatch, the Session having moved on since — is not
// left hanging: the pane is always the honest fallback (§7.2), and why is
// said rather than left for the jump to explain, since a jump that succeeds
// says nothing on its own.
func TestApproveTheGuardCouldNotVerifyFocusesThePane(t *testing.T) {
	approver := &approvals{err: errors.New("pane does not show the dialog it was reported waiting on")}
	jumper := &jumps{}
	model, s := blocked(approver, jumper)

	model = answering(model, "y")

	if len(jumper.pids) != 1 || jumper.pids[0] != s.PID {
		t.Errorf("jumped to %v, want the guard's own mismatch to focus the pane", jumper.pids)
	}
	if !strings.Contains(detail(model), "does not show the dialog") {
		t.Errorf("SELECTED = %q, want the guard's own mismatch explained", detail(model))
	}
}

// jumpTo is shared with the direct Enter gesture, but the guard's own
// mismatch fires from a background send with no idea what you're doing on
// the Dashboard right now — it must show the pane without also stealing
// your keyboard away from it.
func TestApproveTheGuardCouldNotVerifyDoesNotMoveFocus(t *testing.T) {
	approver := &approvals{err: errors.New("pane does not show the dialog it was reported waiting on")}
	jumper := &jumps{}
	focuser := &focuses{}
	s := session.Session{PID: 4242, ID: "sess-1", Dir: "/repos/service-billing",
		Name: "FIRE-2841-paging", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Approver: approver, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{s})
	model = press(model, tea.KeyDown)

	model = answering(model, "y")

	if len(jumper.pids) != 1 {
		t.Fatalf("jumped to %v, want the guard's own mismatch to still focus the pane", jumper.pids)
	}
	if focuser.n != 0 {
		t.Errorf("Focus called %d times, want the async fallback to leave keyboard focus alone", focuser.n)
	}
}

// A second y or n on the same row before the first answer has come back must
// not fire a second guarded send at a pane the first is still verifying.
func TestASecondPressBeforeTheFirstAnswerLandsSendsOnlyOne(t *testing.T) {
	approver := &approvals{}
	s := session.Session{PID: 4242, ID: "sess-1", Dir: "/repos/service-billing",
		Name: "FIRE-2841-paging", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	model := withApprover(approver, &jumps{}, s)
	model = press(model, tea.KeyDown)

	// The first press's Cmd has not been run yet — respond() has already
	// marked the Session pending by the time the second press is handled.
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	model, second := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if second != nil {
		t.Fatal("a second y before the first answer landed asked for another send")
	}
	if cmd == nil {
		t.Fatal("the first y asked for no send at all")
	}
	model, _ = model.Update(cmd())

	if len(approver.approved) != 1 {
		t.Errorf("approved %v, want exactly one send for the two presses", approver.approved)
	}
}

// A repo's header row is not a Session and has no dialog to answer.
func TestYOnARepoHeaderDoesNothing(t *testing.T) {
	approver := &approvals{}
	jumper := &jumps{}
	model := withApprover(approver, jumper, live("ganymede-78", "/repos/ganymede", session.Blocked))

	model = answering(model, "y")

	if len(approver.approved) != 0 || len(jumper.pids) != 0 {
		t.Errorf("approved %v, jumped %v, want nothing done from a repo header", approver.approved, jumper.pids)
	}
}

// A Dashboard with no guard wired up must not panic over a key it cannot
// act on.
func TestYWithNoApproverConfiguredDoesNothing(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Blocked))
	model = press(model, tea.KeyDown)

	model = answering(model, "y")
}
