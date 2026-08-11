package dashboard_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

// promptCall is one pid and text the guard was asked to deliver.
type promptCall struct {
	pid  int
	text string
}

// prompts records every call the Dashboard asked to have delivered, standing
// in for the guarded send-keys engine actually touching tmux.
type prompts struct {
	sent, interrupted []promptCall
	err               error
}

func (p *prompts) Send(pid int, text string) error {
	p.sent = append(p.sent, promptCall{pid, text})
	return p.err
}

func (p *prompts) InterruptAndSend(pid int, text string) error {
	p.interrupted = append(p.interrupted, promptCall{pid, text})
	return p.err
}

// withPrompter is a Dashboard sized for the sidepanel, showing sessions and
// wired to prompter and jumper — the two hands the prompt dialog shares.
func withPrompter(prompter dashboard.Prompter, jumper dashboard.Jumper, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Prompter: prompter})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// sendingKey delivers a full key message and, when the guard has something
// to send, lets its answer come back before returning — the send itself runs
// off the main loop, so a test has to run the command it was handed the same
// way the runtime would.
func sendingKey(model tea.Model, msg tea.KeyMsg) tea.Model {
	model, cmd := model.Update(msg)
	if cmd == nil {
		return model
	}
	model, _ = model.Update(cmd())
	return model
}

// typeInto types each rune of text into whatever dialog is currently open.
func typeInto(model tea.Model, text string) tea.Model {
	for _, r := range text {
		model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

// p then typing then Enter is the whole of the prompt action on an Idle
// Session (§7.3).
func TestPThenTypingAndEnterSendsThePrompt(t *testing.T) {
	prompter := &prompts{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(prompter.sent) != 1 || prompter.sent[0] != (promptCall{idle.PID, "fix it"}) {
		t.Errorf("sent %v, want [{%d %q}]", prompter.sent, idle.PID, "fix it")
	}
}

// Plain Enter on a Working Session queues the same way Send always has —
// Claude Code's own queuing is what tells the two apart, not the guard
// (§7.3).
func TestEnterOnAWorkingSessionQueues(t *testing.T) {
	prompter := &prompts{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withPrompter(prompter, &jumps{}, working)
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(prompter.sent) != 1 || prompter.sent[0] != (promptCall{working.PID, "fix it"}) {
		t.Errorf("sent %v, want [{%d %q}]", prompter.sent, working.PID, "fix it")
	}
	if len(prompter.interrupted) != 0 {
		t.Errorf("interrupted %v, want plain Enter to queue rather than interrupt", prompter.interrupted)
	}
}

// alt+⏎ on a Working Session interrupts the turn first, then delivers the
// prompt (§7.3's Ctrl+Enter row).
func TestAltEnterOnAWorkingSessionInterruptsAndSends(t *testing.T) {
	prompter := &prompts{}
	working := live("ganymede-78", "/repos/ganymede", session.Working)
	model := withPrompter(prompter, &jumps{}, working)
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	if len(prompter.interrupted) != 1 || prompter.interrupted[0] != (promptCall{working.PID, "fix it"}) {
		t.Errorf("interrupted %v, want [{%d %q}]", prompter.interrupted, working.PID, "fix it")
	}
	if len(prompter.sent) != 0 {
		t.Errorf("sent %v, want alt+⏎ to interrupt-and-send rather than plain send", prompter.sent)
	}
}

// p is never offered on Blocked, where Enter would answer the dialog
// instead, or Shell, where you are the occupant (§7.3).
func TestPIsUnavailableOnBlockedAndShellRows(t *testing.T) {
	for _, state := range []session.State{session.Blocked, session.Shell} {
		prompter := &prompts{}
		model := withPrompter(prompter, &jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
		model = typeInto(model, "fix it")
		model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

		if len(prompter.sent) != 0 {
			t.Errorf("a %s Session sent %v, want p offered nowhere on this row", state, prompter.sent)
		}
	}
}

// A repo's header row is not a Session and has no input box to open.
func TestPOnARepoHeaderDoesNothing(t *testing.T) {
	prompter := &prompts{}
	model := withPrompter(prompter, &jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(prompter.sent) != 0 {
		t.Errorf("sent %v, want nothing done from a repo header", prompter.sent)
	}
}

// Esc abandons the dialog: nothing is sent, and the row goes on saying what
// it said.
func TestEscCancelsThePromptWithoutSending(t *testing.T) {
	prompter := &prompts{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(prompter.sent) != 0 {
		t.Errorf("sent %v, want esc to cancel before Enter could send anything", prompter.sent)
	}
	if strings.Contains(detail(model), "prompt ›") {
		t.Errorf("the input is still open after esc:\n%s", detail(model))
	}
}

// A guard mismatch is not left hanging: the pane is the honest fallback
// (§7.2), and why is said rather than left for the jump to explain.
func TestASendThatTheGuardCouldNotVerifyFocusesThePane(t *testing.T) {
	prompter := &prompts{err: errors.New("pane does not show an empty input box to send into")}
	jumper := &jumps{}
	focuser := &focuses{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Prompter: prompter, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{idle})
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(jumper.pids) != 1 || jumper.pids[0] != idle.PID {
		t.Errorf("jumped to %v, want the guard's own mismatch to focus the pane", jumper.pids)
	}
	if !strings.Contains(detail(model), "does not show an empty input box") {
		t.Errorf("SELECTED = %q, want the guard's own mismatch explained", detail(model))
	}
	if focuser.n != 0 {
		t.Errorf("Focus called %d times, want the async fallback to leave keyboard focus alone", focuser.n)
	}
}

// A guard mismatch focuses the pane, but that is not the same as sending
// having earned the Ready Session its clear — a delivery nobody could
// verify has not earned it, even though the pane you are shown is the real
// one.
func TestASendThatTheGuardCouldNotVerifyDoesNotClearAReadyBadge(t *testing.T) {
	seen := &seeing{}
	prompter := &prompts{err: errors.New("pane does not show an empty input box to send into")}
	ready := live("ganymede-78", "/repos/ganymede", session.Ready)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Prompter: prompter, Seen: seen.Seen})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{ready})
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(seen.ids) != 0 {
		t.Errorf("the Dashboard reported %v seen after a send the guard could not verify", seen.ids)
	}
}

// A Dashboard with no Prompter wired must close the dialog with a notice
// rather than sit there swallowing every keystroke with nothing to explain
// why Enter did nothing — the same word a Spawn with no Spawner gets.
func TestSendWithNoPrompterConfiguredClosesTheDialogWithANotice(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if strings.Contains(detail(model), "prompt ›") {
		t.Errorf("the dialog is still open with no Prompter configured:\n%s", detail(model))
	}
	if !strings.Contains(detail(model), "no prompt delivery is configured") {
		t.Errorf("SELECTED = %q, want a notice explaining why nothing was sent", detail(model))
	}
}

// alt+⏎ only means interrupt-then-send on a Working row, the one row it is
// ever offered on. Pressed on an Idle or Ready row's dialog it falls back to
// a plain send instead of firing an Escape into a Session with no turn to
// interrupt.
func TestAltEnterOnANonWorkingSessionSendsPlainly(t *testing.T) {
	prompter := &prompts{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, &jumps{}, idle)
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	if len(prompter.interrupted) != 0 {
		t.Errorf("interrupted %v, want alt+⏎ on an Idle row to send plainly rather than interrupt", prompter.interrupted)
	}
	if len(prompter.sent) != 1 || prompter.sent[0] != (promptCall{idle.PID, "fix it"}) {
		t.Errorf("sent %v, want [{%d %q}]", prompter.sent, idle.PID, "fix it")
	}
}

// Sending counts as a prompt, so it clears Ready the same way seeing the
// Session does — the badge is gone before the registry even reports the
// Session Working.
func TestASuccessfulSendOnAReadySessionClearsTheBadge(t *testing.T) {
	seen := &seeing{}
	ready := live("ganymede-78", "/repos/ganymede", session.Ready)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Prompter: &prompts{}, Seen: seen.Seen})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{ready})
	model = press(model, tea.KeyDown)

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(seen.ids) != 1 || seen.ids[0] != ready.ID {
		t.Errorf("the Dashboard reported %v seen, want the Session it sent to (%s)", seen.ids, ready.ID)
	}
}

// Enter closes the dialog and fires the delivery in the same step, so there
// is never a second Enter to press against it — exactly one delivery for the
// one dialog, every time.
func TestEnterFiresExactlyOneDeliveryForTheDialog(t *testing.T) {
	prompter := &prompts{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, &jumps{}, idle)
	model = press(model, tea.KeyDown)
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the first Enter asked for no send at all")
	}
	model, _ = model.Update(cmd())

	if len(prompter.sent) != 1 {
		t.Errorf("sent %v, want exactly one delivery for the one dialog", prompter.sent)
	}
}

// The real double-send risk is reopening p on the same row before the first
// delivery has landed — startPrompt's own pending gate (mirroring respond's)
// is what has to catch that, since the dialog itself offers no second Enter
// to press.
func TestReopeningThePromptWhileASendIsPendingSendsOnlyOne(t *testing.T) {
	prompter := &prompts{}
	idle := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, &jumps{}, idle)
	model = press(model, tea.KeyDown)
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "first")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the first Enter asked for no send at all")
	}

	// The first delivery is still in flight (its Cmd has not been run yet).
	// Pressing p again on the same row must not reopen the dialog for a
	// second prompt to go in behind the first.
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if strings.Contains(detail(model), "prompt ›") {
		t.Fatalf("p reopened the dialog while a send was still pending:\n%s", detail(model))
	}

	model, _ = model.Update(cmd())

	if len(prompter.sent) != 1 || prompter.sent[0].text != "first" {
		t.Errorf("sent %v, want exactly the first prompt sent once", prompter.sent)
	}
}

// p and its label change with the row's own state (§7.3): "prompt" on Idle
// and Ready, "will queue" on Working, and offered on none of them until it
// is opened.
func TestOfferingListsPOnIdleReadyAndWorking(t *testing.T) {
	for _, tc := range []struct {
		state session.State
		want  string
	}{
		{session.Idle, "p prompt"},
		{session.Ready, "p prompt"},
		{session.Working, "p queue"},
	} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", tc.state))
		model = press(model, tea.KeyDown)

		if box := detail(model); !strings.Contains(box, tc.want) {
			t.Errorf("a %s Session's SELECTED box = %q, want it to offer %q", tc.state, box, tc.want)
		}
	}
}

// Blocked and Shell rows offer no prompt key at all — richer or none is the
// choice there, never a box Enter could answer wrongly.
func TestOfferingListsNoPromptKeyOnBlockedOrShell(t *testing.T) {
	for _, state := range []session.State{session.Blocked, session.Shell} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", state))
		model = press(model, tea.KeyDown)

		if box := detail(model); strings.Contains(box, "p prompt") || strings.Contains(box, "p queue") {
			t.Errorf("a %s Session offers a prompt key:\n%s", state, box)
		}
	}
}
