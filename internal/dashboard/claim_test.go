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
	ended []stopCall
	err   error
}

func (s *stops) End(pid int) error {
	s.ended = append(s.ended, stopCall{pid})
	return s.err
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

// claims records every Claim and Release the Dashboard asked for, standing
// in for the harness's own persisted Claims (internal/claim).
type claims struct {
	kept                 map[string]string
	claimErr, releaseErr error
}

func (c *claims) Claim(root, note string) error {
	if c.claimErr != nil {
		return c.claimErr
	}
	if c.kept == nil {
		c.kept = map[string]string{}
	}
	c.kept[root] = note
	return nil
}

func (c *claims) Release(root string) error {
	if c.releaseErr != nil {
		return c.releaseErr
	}
	delete(c.kept, root)
	return nil
}

func (c *claims) Claimed() map[string]string {
	kept := make(map[string]string, len(c.kept))
	for root, note := range c.kept {
		kept[root] = note
	}
	return kept
}

func (c *claims) NoteOf(root string) (string, bool) {
	note, held := c.kept[root]
	return note, held
}

// withClaimer is a Dashboard sized for the sidepanel, showing sessions and
// wired to claimer, jumper and stopper — the hands the free key shares
// between Claim, Release and Takeover.
func withClaimer(claimer dashboard.Claimer, stopper dashboard.Stopper, jumper dashboard.Jumper, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Claimer: claimer, Stopper: stopper})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// c on a Free repo's header row opens the Claim dialog rather than claiming
// at once — the note is optional, and typing it is what the dialog is for.
func TestCOnAFreeRootOpensTheClaimDialog(t *testing.T) {
	model := onARepo(t, dashboard.Harness{Claimer: &claims{}}, "/repos/billing")

	model = types(model, "c")

	box := detail(model)
	if !strings.Contains(box, "note") {
		t.Errorf("SELECTED = %q, want the Claim dialog's note field", box)
	}
}

// Enter on the open dialog claims the root with whatever note was typed.
func TestClaimingWithANoteRecordsIt(t *testing.T) {
	fake := &claims{}
	model := onARepo(t, dashboard.Harness{Claimer: fake}, "/repos/billing")

	model = types(model, "c")
	model = typeInto(model, "reviewing PR #4123")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if fake.kept["/repos/billing"] != "reviewing PR #4123" {
		t.Errorf("Claimed %v, want billing claimed with the typed note", fake.kept)
	}
}

// A note is optional: Enter with nothing typed still claims the root.
func TestClaimingWithNoNoteRecordsClaimWithNone(t *testing.T) {
	fake := &claims{}
	model := onARepo(t, dashboard.Harness{Claimer: fake}, "/repos/billing")

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if note, held := fake.kept["/repos/billing"]; !held || note != "" {
		t.Errorf("Claimed %v, want billing claimed with no note", fake.kept)
	}
}

// Escape abandons the Claim dialog exactly like it abandons every other
// inline input.
func TestEscapeCancelsClaiming(t *testing.T) {
	fake := &claims{}
	model := onARepo(t, dashboard.Harness{Claimer: fake}, "/repos/billing")

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEsc})

	if len(fake.kept) != 0 {
		t.Errorf("Claimed %v, want nothing claimed after Escape", fake.kept)
	}
	if box := detail(model); strings.Contains(box, "note ›") {
		t.Errorf("SELECTED = %q, want the dialog closed", box)
	}
}

// c on a root you have already claimed releases it at once — no dialog, the
// same low-ceremony gesture a toggle gets.
func TestCOnAClaimedRootReleasesItImmediately(t *testing.T) {
	fake := &claims{}
	if err := fake.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	model := onARepo(t, dashboard.Harness{Claimer: fake}, "/repos/billing")

	model = types(model, "c")

	if _, held := fake.kept["/repos/billing"]; held {
		t.Errorf("Claimed %v, want billing released", fake.kept)
	}
	if box := detail(model); strings.Contains(box, "note ›") {
		t.Errorf("SELECTED = %q, want no dialog opened by a release", box)
	}
}

// c on an InUse root whose only occupant is Idle opens the Takeover
// confirmation rather than acting at once — ending a Session is the one
// destructive half of this key, and it gets the same ceremony q's own
// end-session confirmation does.
func TestCOnAnInUseRootWithAnIdleOccupantOpensTakeoverConfirmation(t *testing.T) {
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	model := withClaimer(&claims{}, &stops{}, &jumps{}, idle)

	model = types(model, "c")

	box := detail(model)
	if !strings.Contains(box, "ganymede-78") {
		t.Errorf("SELECTED = %q, want the Takeover confirmation naming the occupant", box)
	}
}

// Enter on the open Takeover confirmation ends the occupant and then claims
// the root, in that order.
func TestConfirmingTakeoverEndsTheOccupantAndClaims(t *testing.T) {
	fake := &claims{}
	stopper := &stops{}
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	model := withClaimer(fake, stopper, &jumps{}, idle)

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(stopper.ended) != 1 || stopper.ended[0] != (stopCall{idle.PID}) {
		t.Errorf("ended %v, want [{%d}]", stopper.ended, idle.PID)
	}
	if _, held := fake.kept["/repos/billing"]; !held {
		t.Errorf("Claimed %v, want billing claimed once the Takeover went through", fake.kept)
	}
}

// A root already claimed underneath a live occupant — the collision
// state.go documents, since a live occupant always outranks a Claim on the
// state a row draws — keeps its note through a Takeover rather than having
// it wiped to none: Takeover is "one action", not a correction to the note
// nobody asked for.
func TestTakeoverPreservesANoteAlreadyOnTheRoot(t *testing.T) {
	fake := &claims{}
	if err := fake.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	model := withClaimer(fake, &stops{}, &jumps{}, idle)

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if note := fake.kept["/repos/billing"]; note != "reviewing PR #4123" {
		t.Errorf("Claimed with note %q, want the note already on the root preserved", note)
	}
}

// If End actually succeeds but the Claim behind it fails, the two failures
// are told apart: no pane is focused for a Session that, as far as the
// guard could verify, has genuinely ended, and the notice says the End went
// through even though the Claim did not.
func TestTakeoverClaimFailureAfterASuccessfulEndDoesNotFocusADeadPane(t *testing.T) {
	stopper := &stops{}
	fake := &claims{claimErr: errors.New("write /state.json: read-only file system")}
	jumper := &jumps{}
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	model := withClaimer(fake, stopper, jumper, idle)

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(stopper.ended) != 1 || stopper.ended[0] != (stopCall{idle.PID}) {
		t.Errorf("ended %v, want the occupant actually ended", stopper.ended)
	}
	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v, want no pane focused for a Session that already ended", jumper.pids)
	}
	if box := detail(model); !strings.Contains(box, "ended the session") {
		t.Errorf("SELECTED = %q, want the notice to say the session ended even though the Claim failed", box)
	}
}

// Escape abandons the Takeover confirmation: nothing is ended, and nothing
// is claimed.
func TestEscapeCancelsTakeoverWithoutEndingOrClaiming(t *testing.T) {
	fake := &claims{}
	stopper := &stops{}
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	model := withClaimer(fake, stopper, &jumps{}, idle)

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEsc})

	if len(stopper.ended) != 0 {
		t.Errorf("ended %v, want Escape to cancel without ending anything", stopper.ended)
	}
	if len(fake.kept) != 0 {
		t.Errorf("Claimed %v, want nothing claimed after Escape", fake.kept)
	}
}

// A guard mismatch on End is the honest fallback every other guarded action
// gets: the pane is focused, and the root is never claimed over a Session
// that, as far as the guard could verify, never actually ended.
func TestTakeoverGuardMismatchFocusesThePaneAndDoesNotClaim(t *testing.T) {
	fake := &claims{}
	stopper := &stops{err: errors.New("pane still shows Claude Code's own prompt after /exit was sent")}
	jumper := &jumps{}
	focuser := &focuses{}
	idle := live("ganymede-78", "/repos/billing", session.Idle)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Claimer: fake, Stopper: stopper, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 45})
	model, _ = model.Update(dashboard.Sessions{idle})

	model = types(model, "c")
	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(jumper.pids) != 1 || jumper.pids[0] != idle.PID {
		t.Errorf("jumped to %v, want the guard's own mismatch to focus the pane", jumper.pids)
	}
	if len(fake.kept) != 0 {
		t.Errorf("Claimed %v, want no Claim over a Session the guard could not confirm had ended", fake.kept)
	}
	if focuser.n != 0 {
		t.Errorf("Focus called %d times, want the async fallback to leave keyboard focus alone", focuser.n)
	}
}

// Takeover is refused — silently, the same way q is refused on a row it does
// not apply to — when the occupant is Working: an interrupt has to come
// first, and this key does not do that on its own.
func TestCOnAnInUseRootWithAWorkingOccupantIsRefused(t *testing.T) {
	fake := &claims{}
	stopper := &stops{}
	working := live("ganymede-78", "/repos/billing", session.Working)
	model := withClaimer(fake, stopper, &jumps{}, working)

	model = types(model, "c")

	if len(stopper.ended) != 0 || len(fake.kept) != 0 {
		t.Errorf("ended %v, claimed %v, want nothing done against a Working occupant", stopper.ended, fake.kept)
	}
}

// Takeover is refused when the occupant is Blocked too — it cannot continue
// without you, which is a decision this key must not make for you.
func TestCOnAnInUseRootWithABlockedOccupantIsRefused(t *testing.T) {
	fake := &claims{}
	stopper := &stops{}
	blocked := live("ganymede-78", "/repos/billing", session.Blocked)
	model := withClaimer(fake, stopper, &jumps{}, blocked)

	model = types(model, "c")

	if len(stopper.ended) != 0 || len(fake.kept) != 0 {
		t.Errorf("ended %v, claimed %v, want nothing done against a Blocked occupant", stopper.ended, fake.kept)
	}
}

// "Only occupant" is load-bearing: two live Sessions both holding the root
// refuse a Takeover even though each of them, alone, would have been Idle.
func TestCOnAnInUseRootWithMoreThanOneOccupantIsRefused(t *testing.T) {
	fake := &claims{}
	stopper := &stops{}
	root := "/repos/billing"
	model := withClaimer(fake, stopper, &jumps{},
		live("ganymede-78", root, session.Idle),
		live("ganymede-79", root, session.Idle))

	model = types(model, "c")

	if len(stopper.ended) != 0 || len(fake.kept) != 0 {
		t.Errorf("ended %v, claimed %v, want nothing done with more than one occupant", stopper.ended, fake.kept)
	}
}

// c means nothing on a Session's own row — Claim, release and Takeover are
// all about the repo's Main root, and a Session row already has its own
// letters.
func TestCOnASessionRowDoesNothing(t *testing.T) {
	fake := &claims{}
	model := withClaimer(fake, &stops{}, &jumps{}, live("ganymede-78", "/repos/billing", session.Working))
	model = press(model, tea.KeyDown)

	model = types(model, "c")

	if len(fake.kept) != 0 {
		t.Errorf("Claimed %v, want nothing claimed from a Session row", fake.kept)
	}
}

// A busy hidden Popup shell is mentioned in the Claim confirmation, so a
// note typed there is typed knowing the root is not as quiet as it looks.
func TestClaimDialogMentionsABusyPopup(t *testing.T) {
	model := onARepo(t, dashboard.Harness{Claimer: &claims{}}, "/repos/billing")
	model, _ = model.Update(dashboard.PopupStatuses{"/repos/billing": {Command: "composer install", Busy: true}})

	model = types(model, "c")

	if box := detail(model); !strings.Contains(box, "composer install") {
		t.Errorf("SELECTED = %q, want the Claim dialog to mention the busy popup", box)
	}
}

// The Takeover confirmation mentions a busy Popup shell too — ending the
// occupant does not touch the popup, but you should know it is there before
// you take the root over.
func TestTakeoverConfirmationMentionsABusyPopup(t *testing.T) {
	root := "/repos/billing"
	model := withClaimer(&claims{}, &stops{}, &jumps{}, live("ganymede-78", root, session.Idle))
	model, _ = model.Update(dashboard.PopupStatuses{root: {Command: "make", Busy: true}})

	model = types(model, "c")

	if box := detail(model); !strings.Contains(box, "make") {
		t.Errorf("SELECTED = %q, want the Takeover confirmation to mention the busy popup", box)
	}
}

// Spawning into a Claimed root is not blocked — the state table only says it
// "warns" — but the dialog says so before you commit to it.
func TestSpawningIntoAClaimedRootWarns(t *testing.T) {
	fake := &claims{}
	if err := fake.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	model := onARepo(t, dashboard.Harness{Claimer: fake, Spawner: &spawns{}}, "/repos/billing")

	model = types(model, "w")

	if box := detail(model); !strings.Contains(box, "reviewing PR #4123") {
		t.Errorf("SELECTED = %q, want the spawn dialog to warn about the Claim", box)
	}
}

// The offered keys read the state the root is actually in: claim on Free,
// release on Claimed, takeover only once Takeover is actually on offer.
func TestOfferingReadsClaimReleaseOrTakeoverFromTheRootState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		harness func() dashboard.Harness
		want    string
	}{
		{"free", func() dashboard.Harness { return dashboard.Harness{Claimer: &claims{}} }, "c claim"},
		{"claimed", func() dashboard.Harness {
			fake := &claims{}
			_ = fake.Claim("/repos/billing", "")
			return dashboard.Harness{Claimer: fake}
		}, "c release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := onARepo(t, tc.harness(), "/repos/billing")
			if box := detail(model); !strings.Contains(box, tc.want) {
				t.Errorf("SELECTED = %q, want it to offer %q", box, tc.want)
			}
		})
	}
}

// Takeover is offered once the occupant is Idle...
func TestOfferingListsTakeoverOnAnInUseRootWithAnIdleOccupant(t *testing.T) {
	model := withClaimer(&claims{}, &stops{}, &jumps{}, live("ganymede-78", "/repos/billing", session.Idle))

	if box := detail(model); !strings.Contains(box, "c takeover") {
		t.Errorf("SELECTED = %q, want it to offer c takeover", box)
	}
}

// ...and not offered at all while the occupant is Working — there is
// nothing this key could do about that row, and offering it anyway would be
// a key that silently does nothing when pressed.
func TestOfferingListsNoCOnAnInUseRootWithAWorkingOccupant(t *testing.T) {
	model := withClaimer(&claims{}, &stops{}, &jumps{}, live("ganymede-78", "/repos/billing", session.Working))

	if box := detail(model); strings.Contains(box, "c takeover") || strings.Contains(box, "c claim") || strings.Contains(box, "c release") {
		t.Errorf("SELECTED = %q, want no c offered against a Working occupant", box)
	}
}
