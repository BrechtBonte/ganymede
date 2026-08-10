package dashboard_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// stopCall is one pid the guard was asked to act on.
type stopCall struct{ pid int }

// stops records every call the Dashboard asked to have acted on, standing in
// for the guarded send-keys engine actually touching tmux.
type stops struct {
	interrupted, ended []stopCall
	err                error
}

func (s *stops) Interrupt(pid int) error {
	s.interrupted = append(s.interrupted, stopCall{pid})
	return s.err
}

func (s *stops) End(pid int) error {
	s.ended = append(s.ended, stopCall{pid})
	return s.err
}

// withStopper is a Dashboard sized for the sidepanel, showing sessions and
// wired to stopper and jumper — the two hands x and q share between them.
func withStopper(stopper dashboard.Stopper, jumper dashboard.Jumper, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Stopper: stopper})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// x interrupts a Working Session's own turn (§7.3).
func TestXInterruptsTheSelectedWorkingSession(t *testing.T) {
	stopper := &stops{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withStopper(stopper, &jumps{}, working)
	model = press(model, tea.KeyDown)

	model = answering(model, "x")

	if len(stopper.interrupted) != 1 || stopper.interrupted[0] != (stopCall{working.PID}) {
		t.Errorf("interrupted %v, want [{%d}]", stopper.interrupted, working.PID)
	}
}

// x is offered only on a Working row (§7.3) — the registry gate before send
// ever touches tmux.
func TestXIsRefusedOnNonWorkingRows(t *testing.T) {
	for _, state := range []session.State{session.Idle, session.Ready, session.Blocked, session.Shell} {
		stopper := &stops{}
		model := withStopper(stopper, &jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		model = answering(model, "x")

		if len(stopper.interrupted) != 0 {
			t.Errorf("a %s Session was interrupted by x, want it offered only on Working", state)
		}
	}
}

// A repo's header row is not a Session and has no turn to interrupt or end.
func TestXAndQOnARepoHeaderDoNothing(t *testing.T) {
	stopper := &stops{}
	model := withStopper(stopper, &jumps{}, live("ganymede-78", "/repos/ganymede", session.Working))

	model = answering(model, "x")
	model = answering(model, "q")

	if len(stopper.interrupted) != 0 || len(stopper.ended) != 0 {
		t.Errorf("interrupted %v, ended %v, want nothing done from a repo header", stopper.interrupted, stopper.ended)
	}
}

// A Dashboard with no Stopper wired must not panic over a key it cannot act
// on.
func TestXWithNoStopperConfiguredDoesNothing(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working))
	model = press(model, tea.KeyDown)

	model = answering(model, "x")
}

// x has no confirm dialog of its own — the guard's own mismatch is the
// pane's honest fallback, the same as every other guarded action (§7.2,
// §7.3).
func TestInterruptTheGuardCouldNotVerifyFocusesThePane(t *testing.T) {
	stopper := &stops{err: errors.New("pane does not show an empty input box to interrupt")}
	jumper := &jumps{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withStopper(stopper, jumper, working)
	model = press(model, tea.KeyDown)

	model = answering(model, "x")

	if len(jumper.pids) != 1 || jumper.pids[0] != working.PID {
		t.Errorf("jumped to %v, want the guard's own mismatch to focus the pane", jumper.pids)
	}
	if !strings.Contains(detail(model), "does not show an empty input box") {
		t.Errorf("SELECTED = %q, want the guard's own mismatch explained", detail(model))
	}
}

// A second x before the first interrupt has come back must not fire a
// second guarded send at a pane the first is still verifying.
func TestASecondXBeforeTheFirstInterruptLandsSendsOnlyOne(t *testing.T) {
	stopper := &stops{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withStopper(stopper, &jumps{}, working)
	model = press(model, tea.KeyDown)

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model, second := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if second != nil {
		t.Fatal("a second x before the first interrupt landed asked for another send")
	}
	if cmd == nil {
		t.Fatal("the first x asked for no send at all")
	}
	model, _ = model.Update(cmd())

	if len(stopper.interrupted) != 1 {
		t.Errorf("interrupted %v, want exactly one send for the two presses", stopper.interrupted)
	}
}

// q opens the end-session confirmation over an Idle row, with no
// unread-output warning — there is nothing unread to warn about.
func TestQOpensEndConfirmationOnIdle(t *testing.T) {
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withStopper(&stops{}, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = answering(model, "q")

	box := detail(model)
	if !strings.Contains(box, "end this session?") {
		t.Errorf("SELECTED = %q, want the end-session confirmation open", box)
	}
	if strings.Contains(box, "unread") {
		t.Errorf("SELECTED = %q, want no unread-output warning on an Idle Session", box)
	}
}

// q on a Ready row carries the unread-output warning (§7.3): there is
// something the confirmation has to own up to before it proceeds.
func TestQOpensEndConfirmationWithAnUnreadWarningOnReady(t *testing.T) {
	ready := live("ganymede-78", "/repos/ganymede", session.Ready)
	model := withStopper(&stops{}, &jumps{}, ready)
	model = press(model, tea.KeyDown)

	model = answering(model, "q")

	if box := detail(model); !strings.Contains(box, "unread") {
		t.Errorf("SELECTED = %q, want the unread-output warning on a Ready Session", box)
	}
}

// q is refused on Working and Blocked, where an interrupt has to come
// first, and on Shell, where you are the occupant (§7.3).
func TestQIsRefusedOnWorkingBlockedAndShellRows(t *testing.T) {
	for _, state := range []session.State{session.Working, session.Blocked, session.Shell} {
		model := withStopper(&stops{}, &jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		model = answering(model, "q")

		if box := detail(model); strings.Contains(box, "end this session?") {
			t.Errorf("a %s Session opened the end-session confirmation:\n%s", state, box)
		}
	}
}

// Enter confirms the open dialog: paste /exit and Enter at the Session's own
// prompt (§7.3).
func TestEnterConfirmsTheEndDialogAndCallsEnd(t *testing.T) {
	stopper := &stops{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withStopper(stopper, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = answering(model, "q")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(stopper.ended) != 1 || stopper.ended[0] != (stopCall{idle.PID}) {
		t.Errorf("ended %v, want [{%d}]", stopper.ended, idle.PID)
	}
}

// Esc abandons the confirmation: nothing is sent, and the row goes on
// saying what it said.
func TestEscCancelsTheEndDialogWithoutCallingEnd(t *testing.T) {
	stopper := &stops{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withStopper(stopper, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = answering(model, "q")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEsc})

	if len(stopper.ended) != 0 {
		t.Errorf("ended %v, want esc to cancel without ending", stopper.ended)
	}
	if strings.Contains(detail(model), "end this session?") {
		t.Errorf("the end dialog is still open after esc:\n%s", detail(model))
	}
}

// A guard mismatch on the confirmed end is not left hanging: the pane is
// the honest fallback, the same as every other guarded action (§7.2).
func TestEndTheGuardCouldNotVerifyFocusesThePane(t *testing.T) {
	stopper := &stops{err: errors.New("pane does not show an empty input box to send into")}
	jumper := &jumps{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withStopper(stopper, jumper, idle)
	model = press(model, tea.KeyDown)

	model = answering(model, "q")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(jumper.pids) != 1 || jumper.pids[0] != idle.PID {
		t.Errorf("jumped to %v, want the guard's own mismatch to focus the pane", jumper.pids)
	}
	if !strings.Contains(detail(model), "does not show an empty input box") {
		t.Errorf("SELECTED = %q, want the guard's own mismatch explained", detail(model))
	}
}

// A Dashboard with no Stopper wired must close the dialog with a notice
// rather than sit there swallowing every keystroke — the same word a Send
// with no Prompter gets.
func TestQWithNoStopperConfiguredClosesTheDialogWithANotice(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))
	model = press(model, tea.KeyDown)

	model = answering(model, "q")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if strings.Contains(detail(model), "end this session?") {
		t.Errorf("the dialog is still open with no Stopper configured:\n%s", detail(model))
	}
	if !strings.Contains(detail(model), "no session ending is configured") {
		t.Errorf("SELECTED = %q, want a notice explaining why nothing was sent", detail(model))
	}
}

// Reopening q on the same row before the first end has come back must not
// fire a second guarded send at a pane the first is still verifying.
func TestReopeningQWhileAnEndIsPendingDoesNothing(t *testing.T) {
	stopper := &stops{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withStopper(stopper, &jumps{}, idle)
	model = press(model, tea.KeyDown)
	model = answering(model, "q")

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the first Enter asked for no send at all")
	}

	model = answering(model, "q")
	if strings.Contains(detail(model), "end this session?") {
		t.Fatalf("q reopened the dialog while an end was still pending:\n%s", detail(model))
	}

	model, _ = model.Update(cmd())
	if len(stopper.ended) != 1 {
		t.Errorf("ended %v, want exactly one end for the two presses", stopper.ended)
	}
}

// x and q are offered on the rows they apply to (§7.3).
func TestOfferingListsXOnWorkingAndQOnIdleAndReady(t *testing.T) {
	for _, tc := range []struct {
		state session.State
		want  string
	}{
		{session.Working, "x interrupt"},
		{session.Idle, "q end"},
		{session.Ready, "q end"},
	} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", tc.state))
		model = press(model, tea.KeyDown)

		if box := detail(model); !strings.Contains(box, tc.want) {
			t.Errorf("a %s Session's SELECTED box = %q, want it to offer %q", tc.state, box, tc.want)
		}
	}
}

// Blocked and Shell rows offer neither key: an interrupt has nothing to
// interrupt there, and an end has to go through an interrupt first.
func TestOfferingListsNoXOrQOnBlockedOrShell(t *testing.T) {
	for _, state := range []session.State{session.Blocked, session.Shell} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		if box := detail(model); strings.Contains(box, "x interrupt") || strings.Contains(box, "q end") {
			t.Errorf("a %s Session offers x or q:\n%s", state, box)
		}
	}
}
