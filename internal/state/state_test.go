package state_test

import (
	"slices"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/reconciler"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/state"
)

// start is an hour ago. Every moment in these tests hangs off it, because the
// model turns on which of the registry and a hook moved last — and in life the
// registry always timestamps a Session waiting some moments after the hook
// that put it there fired.
var start = time.Now().Add(-time.Hour)

// watching is a state model fed by hand: what the registry says, and what the
// hooks report, in the order a test puts them.
type watching struct {
	t         *testing.T
	model     *state.Model
	snapshots chan []session.Session
	checks    chan reconciler.Reconciled
	hooks     chan hooks.Event
	merged    <-chan []session.Session
	alerts    <-chan state.Alert
}

func watch(t *testing.T) *watching {
	t.Helper()
	w := &watching{
		t:         t,
		model:     state.New(),
		snapshots: make(chan []session.Session),
		checks:    make(chan reconciler.Reconciled),
		hooks:     make(chan hooks.Event),
	}
	w.merged = w.model.Watch(t.Context(), w.snapshots, w.checks, w.hooks)
	w.alerts = w.model.Alerts()
	return w
}

// alerted waits for the next Alert the model raises.
//
// It is read after the working set that the same event produced: apply runs
// to completion — Alert and all — before the model ever blocks trying to hand
// the merged set to the test, so by the time shown() has returned, an Alert
// the event raised is already sitting in the (buffered) channel waiting to be
// read without a race.
func (w *watching) alerted() state.Alert {
	w.t.Helper()
	select {
	case a := <-w.alerts:
		return a
	case <-time.After(2 * time.Second):
		w.t.Fatal("the state model raised no Alert")
		return state.Alert{}
	}
}

// noAlert asserts that nothing has been raised.
func (w *watching) noAlert() {
	w.t.Helper()
	select {
	case a := <-w.alerts:
		w.t.Fatalf("the state model raised an Alert it should not have: %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
}

// registry is what the registry says the working set is now.
func (w *watching) registry(sessions ...session.Session) []session.Session {
	w.t.Helper()
	w.snapshots <- sessions
	return w.shown()
}

// crossCheck is what `claude agents --json` reported, having been asked at a
// moment.
func (w *watching) crossCheck(at time.Time, sessions ...session.Session) []session.Session {
	w.t.Helper()
	w.checks <- reconciler.Reconciled{At: at, Sessions: sessions}
	return w.shown()
}

// hook is an event a Session reported.
func (w *watching) hook(event hooks.Event) []session.Session {
	w.t.Helper()
	if event.At.IsZero() {
		event.At = time.Now()
	}
	w.hooks <- event
	return w.shown()
}

// shown is the working set the model puts up next.
func (w *watching) shown() []session.Session {
	w.t.Helper()
	select {
	case set, ok := <-w.merged:
		if !ok {
			w.t.Fatal("the state model stopped")
		}
		return set
	case <-time.After(2 * time.Second):
		w.t.Fatal("the state model showed nothing")
		return nil
	}
}

// only is the one Session on show.
func only(t *testing.T, set []session.Session) session.Session {
	t.Helper()
	if len(set) != 1 {
		t.Fatalf("the working set holds %d Sessions, want one: %+v", len(set), set)
	}
	return set[0]
}

// running is a Session as the registry describes it, in a state, having been
// in it since a moment.
func running(state session.State, since time.Time) session.Session {
	return session.Session{PID: 72144, ID: "s1", Dir: "/repos/service-billing",
		Name: "FIRE-2841-paging", State: state, Since: since}
}

// crossChecked is that same Session as `claude agents --json` describes one:
// what it is doing, with no word on when it entered that state or why.
func crossChecked(state session.State) session.Session {
	return running(state, time.Time{})
}

// missed is a Session only the cross-check knows about — one the registry
// watch never reported at all.
func missed(state session.State) session.Session {
	return session.Session{PID: 88021, ID: "s2", Dir: "/repos/service-ai-assistant",
		Name: "ai-assistant-c4", State: state}
}

// names is the working set as it reads down the Dashboard.
func names(set []session.Session) []string {
	found := make([]string, len(set))
	for i, s := range set {
		found[i] = s.Name
	}
	return found
}

func turnEnded(said string) hooks.Event {
	return hooks.Event{Kind: hooks.Finished, Session: "s1", Snippet: said}
}

func needsADecision(why string, at time.Time) hooks.Event {
	return hooks.Event{Kind: hooks.Blocked, Session: "s1", Reason: why, At: at}
}

// Ready is the state the registry cannot give: the turn ended, and nothing has
// said you saw it. The message it ended on rides along, because an unread
// badge you cannot read anything of is only half a badge.
func TestATurnEndingLeavesTheSessionReady(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	w.hook(turnEnded("The integration suite is green (214 passed)."))
	shown := only(t, w.registry(running(session.Idle, start.Add(time.Minute))))

	if shown.State != session.Ready {
		t.Errorf("the Session is %s, want %s", shown.State, session.Ready)
	}
	if shown.Snippet != "The integration suite is green (214 passed)." {
		t.Errorf("Ready carries %q, want what the turn ended on", shown.Snippet)
	}
}

// Ready is Idle plus an unread turn, and nothing else. A Session still working
// — the hook having arrived before the registry caught up — is Working, and a
// row that said otherwise would put it in Attention while it is busy.
func TestASessionStillWorkingIsNotReadyHoweverTheHookLanded(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	shown := only(t, w.hook(turnEnded("done")))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want %s until the registry says otherwise", shown.State, session.Working)
	}
}

// Seeing the Session is what clears the badge — the harness tracks that
// itself, since the registry has no idea what you have looked at.
func TestSeeingASessionClearsReady(t *testing.T) {
	for _, c := range []struct {
		what  string
		event hooks.Event
	}{
		{"focus landing on its pane", hooks.Event{Kind: hooks.Seen, Session: "s1"}},
		{"a new prompt", hooks.Event{Kind: hooks.Prompted, Session: "s1"}},
	} {
		t.Run(c.what, func(t *testing.T) {
			w := watch(t)
			w.registry(running(session.Idle, start))
			w.hook(turnEnded("done"))

			shown := only(t, w.hook(c.event))

			if shown.State != session.Idle {
				t.Errorf("after %s the Session is %s, want %s", c.what, shown.State, session.Idle)
			}
			if shown.Snippet != "" {
				t.Errorf("a Session that has been seen still carries %q", shown.Snippet)
			}
		})
	}
}

// Jumping to a Session is seeing it. The Dashboard says so itself rather than
// waiting for tmux to report the focus, because it is the one that moved you.
func TestJumpingToASessionClearsReady(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("done"))

	w.model.Seen("s1")

	if shown := only(t, w.shown()); shown.State != session.Idle {
		t.Errorf("after jumping the Session is %s, want %s", shown.State, session.Idle)
	}
}

// The whole reason for the hooks: a permission prompt goes up in the pane and
// the row says so now, not when the registry gets around to it a second later.
func TestAPermissionPromptBlocksTheRowBeforeTheRegistryCatchesUp(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	shown := only(t, w.hook(needsADecision("permission: Bash", start.Add(time.Minute))))

	if shown.State != session.Blocked {
		t.Errorf("the Session is %s, want %s", shown.State, session.Blocked)
	}
	if shown.Reason != "permission: Bash" {
		t.Errorf("the Blocked row reads %q, want the reason the hook carried", shown.Reason)
	}
}

// Blocked is always displayed with its reason. The registry knows a Session is
// waiting but does not always say what for, and the hook that put it there
// does.
func TestABlockedRowWithoutAReasonTakesTheHooks(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))

	// The registry catches up: waiting, and saying nothing about why.
	shown := only(t, w.registry(running(session.Blocked, start.Add(2*time.Minute))))

	if shown.Reason != "permission: Bash" {
		t.Errorf("the Blocked row reads %q, want the reason the hook carried", shown.Reason)
	}
}

// The registry is the authority. Where it has an account of its own, that is
// the one on show.
func TestTheRegistrysOwnReasonIsTheOneOnShow(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))

	blocked := running(session.Blocked, start.Add(2*time.Minute))
	blocked.Reason = "permission: Write(config.yaml)"
	shown := only(t, w.registry(blocked))

	if shown.Reason != "permission: Write(config.yaml)" {
		t.Errorf("the Blocked row reads %q, want what the registry says", shown.Reason)
	}
}

// A hook edge is only ever early, never a second opinion: once the registry
// has moved since the hook fired, the registry has the row back. Answering the
// dialog is what this looks like from the pane.
func TestAnsweringTheDialogGivesTheRowBackToTheRegistry(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))

	// The Session goes back to work, which the registry timestamps after the
	// hook fired.
	shown := only(t, w.registry(running(session.Working, start.Add(2*time.Minute))))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want %s once the registry has moved on", shown.State, session.Working)
	}
	if shown.Reason != "" {
		t.Errorf("a Session that is working again still reads %q", shown.Reason)
	}
}

// Attention has an order: Blocked outranks Ready. A Session that finished a
// turn and then asked for a decision is Blocked, and the unread turn waits.
func TestBlockedOutranksAnUnreadTurn(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("shall I push?"))

	shown := only(t, w.hook(needsADecision("permission: Bash", start.Add(time.Minute))))

	if shown.State != session.Blocked {
		t.Errorf("the Session is %s, want %s", shown.State, session.Blocked)
	}
}

// A Session that starts or ends leaves nothing behind. Ids are Claude Code's
// to reuse or not, and a badge that outlived the Session it was about would be
// unclearable.
func TestASessionStartingLeavesNothingOfTheLastOneBehind(t *testing.T) {
	for _, kind := range []hooks.Kind{hooks.Started, hooks.Ended} {
		t.Run(string(kind), func(t *testing.T) {
			w := watch(t)
			w.registry(running(session.Idle, start))
			w.hook(turnEnded("done"))

			shown := only(t, w.hook(hooks.Event{Kind: kind, Session: "s1"}))

			if shown.State != session.Idle {
				t.Errorf("after %s the Session is %s, want %s", kind, shown.State, session.Idle)
			}
		})
	}
}

// Sessions that were running before the harness came up — or before the hooks
// were installed — never report anything. They have to show what the registry
// says about them, which is every state but Ready.
func TestASessionTheHooksNeverReportedShowsWhatTheRegistrySays(t *testing.T) {
	w := watch(t)

	for _, want := range []session.State{session.Working, session.Blocked, session.Idle, session.Shell} {
		if shown := only(t, w.registry(running(want, start))); shown.State != want {
			t.Errorf("a Session the registry calls %s shows as %s", want, shown.State)
		}
	}
}

// Hooks arrive for Sessions the Dashboard has never drawn: one started outside
// the harness, one whose registry file has not been written yet. Nothing about
// that is worth an empty Dashboard or a panic.
func TestAnEventForASessionOnNoRowIsHarmless(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))

	shown := only(t, w.hook(hooks.Event{Kind: hooks.Finished, Session: "someone-else", Snippet: "hello"}))

	if shown.State != session.Idle {
		t.Errorf("another Session's event changed this one to %s", shown.State)
	}
	w.model.Seen("nobody-at-all")
}

// The registry watch can stop while the Dashboard stays up. What it last said
// is better than nothing, and the hooks keep working over it.
func TestTheModelKeepsTheLastWorkingSetWhenTheRegistryWatchStops(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))

	close(w.snapshots)
	shown := only(t, w.hook(turnEnded("done")))

	if shown.State != session.Ready {
		t.Errorf("the Session is %s, want %s", shown.State, session.Ready)
	}
}

// The registry is undocumented, and a record that does not say when it last
// moved is a record no hook edge can be weighed against. Weighing one against
// the start of the Unix epoch instead would leave a single permission prompt
// pinning the row Blocked for the rest of the Session's life.
func TestARegistryRecordWithNoClockIsNotOutweighedByAHook(t *testing.T) {
	w := watch(t)
	noClock := running(session.Working, time.Time{})
	w.registry(noClock)

	shown := only(t, w.hook(needsADecision("permission: Bash", time.Now())))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want the registry's %s", shown.State, session.Working)
	}
}

// And a hook edge already held over such a Session has to be let go of, rather
// than waiting for a clock that is never coming.
func TestAHookEdgeIsLetGoOfWhenTheRegistryLosesItsClock(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))

	shown := only(t, w.registry(running(session.Working, time.Time{})))

	if shown.State != session.Working || shown.Reason != "" {
		t.Errorf("the Session is %s (%q), want the registry's %s", shown.State, shown.Reason, session.Working)
	}
}

// What the reconciler is for: a Session the registry watch never reported —
// because the files moved, or their shape did — is on the Dashboard as soon as
// the cross-check has been made.
func TestASessionTheRegistryNeverReportedAppearsOnTheCrossCheck(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	shown := w.crossCheck(start.Add(time.Minute), crossChecked(session.Working), missed(session.Blocked))

	if want := []string{"FIRE-2841-paging", "ai-assistant-c4"}; !slices.Equal(names(shown), want) {
		t.Errorf("the working set holds %v, want %v", names(shown), want)
	}
}

// The registry files are undocumented and `claude agents --json` is not, so
// where the two describe the same Session differently, the documented one is
// believed.
func TestTheCrossCheckWinsWhereItDisagreesWithTheRegistry(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))

	shown := only(t, w.crossCheck(start.Add(time.Minute), crossChecked(session.Working)))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want the cross-check's %s", shown.State, session.Working)
	}
}

// But a registry that has moved the Session on since the cross-check ran is
// not disagreeing with it — it is ahead of it, and the cross-check is the
// slower of the two by design. Handing the row back to a picture taken half a
// minute ago is the flicker this must not have.
func TestTheRegistryKeepsARowItHasMovedSinceTheCrossCheck(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.crossCheck(start.Add(time.Minute), crossChecked(session.Idle))

	shown := only(t, w.registry(running(session.Working, start.Add(2*time.Minute))))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want the registry's newer %s", shown.State, session.Working)
	}
}

// A registry record that cannot say when it last moved cannot show it is the
// fresher of the two, and a record whose clock the harness can no longer read
// is exactly the drift the cross-check is insurance against.
func TestARegistryRecordWithNoClockLosesToTheCrossCheck(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, time.Time{}))

	shown := only(t, w.crossCheck(start, crossChecked(session.Working)))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want the cross-check's %s", shown.State, session.Working)
	}
}

// The cross-check runs for as long as the Dashboard is up, and almost every
// one of them agrees with the registry. Those must change nothing at all —
// not the state, and not the reason and wait age the cross-check has no word
// for and would otherwise wipe.
func TestACrossCheckThatAgreesChangesNothing(t *testing.T) {
	w := watch(t)
	blocked := running(session.Blocked, start)
	blocked.Reason = "permission: Write(config.yaml)"
	before := w.registry(blocked)

	after := w.crossCheck(start.Add(time.Minute), crossChecked(session.Blocked))

	if !slices.Equal(before, after) {
		t.Errorf("the cross-check redrew\n\t%+v\nas\n\t%+v", before, after)
	}
}

// Taking the cross-check's word for what a Session is doing means letting go
// of the registry's account of why it was waiting, which was about the state
// just overruled.
func TestASessionTheCrossCheckMovesOffBlockedLosesTheReasonWithIt(t *testing.T) {
	w := watch(t)
	blocked := running(session.Blocked, time.Time{})
	blocked.Reason = "permission: Write(config.yaml)"
	w.registry(blocked)

	shown := only(t, w.crossCheck(start, crossChecked(session.Working)))

	if shown.Reason != "" {
		t.Errorf("a Session that is working again still reads %q", shown.Reason)
	}
}

// The cross-check settles what the registry says; the hooks still lie over
// both. Blocked is always displayed with its reason, and the cross-check never
// carries one.
func TestTheHooksLieOverTheCrossCheck(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, time.Time{}))
	w.hook(needsADecision("permission: Bash", start))

	shown := only(t, w.crossCheck(start.Add(time.Minute), crossChecked(session.Blocked)))

	if shown.State != session.Blocked {
		t.Errorf("the Session is %s, want %s", shown.State, session.Blocked)
	}
	if shown.Reason != "permission: Bash" {
		t.Errorf("the Blocked row reads %q, want the reason the hook carried", shown.Reason)
	}
}

// Ready is the harness's own, over whichever of the two put the Session at the
// prompt — including a Session the registry never reported at all.
func TestASessionOnlyTheCrossCheckKnowsAboutCanStillBeReady(t *testing.T) {
	w := watch(t)
	w.registry()
	w.crossCheck(start, missed(session.Idle))

	shown := only(t, w.hook(hooks.Event{Kind: hooks.Finished, Session: "s2", Snippet: "done"}))

	if shown.State != session.Ready {
		t.Errorf("the Session is %s, want %s", shown.State, session.Ready)
	}
}

// The cross-check is always the older picture of the two, so a Session it has
// not heard of is far likelier to be one that has just started than one that
// has gone. Taking rows off the Dashboard is the registry's to do, by the
// liveness check the cross-check runs no better.
func TestTheCrossCheckNeverTakesARowTheRegistryHas(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start), missed(session.Idle))

	shown := w.crossCheck(start.Add(time.Minute), crossChecked(session.Working))

	if want := []string{"FIRE-2841-paging", "ai-assistant-c4"}; !slices.Equal(names(shown), want) {
		t.Errorf("the working set holds %v, want %v", names(shown), want)
	}
}

// The hooks are earlier than either of the other two, and a cross-check must
// not deafen the harness to them. Taking the row from the registry means
// standing where the registry stood — including being answerable to a hook
// that fires after the cross-check was asked.
func TestAHookEdgeStillTakesARowTheCrossCheckCorrected(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.crossCheck(start.Add(time.Minute), crossChecked(session.Working))

	shown := only(t, w.hook(needsADecision("permission: Bash", start.Add(2*time.Minute))))

	if shown.State != session.Blocked {
		t.Errorf("the Session is %s, want %s", shown.State, session.Blocked)
	}
	if shown.Reason != "permission: Bash" {
		t.Errorf("the Blocked row reads %q, want the reason the hook carried", shown.Reason)
	}
}

// And a Session only the cross-check ever knew about is answerable to them
// too. It is the one kind of row that exists because something drifted, which
// makes it the last row that should also go quiet.
func TestASessionOnlyTheCrossCheckKnowsAboutCanBeBlockedByAHook(t *testing.T) {
	w := watch(t)
	w.registry()
	w.crossCheck(start, missed(session.Idle))

	shown := only(t, w.hook(hooks.Event{Kind: hooks.Blocked, Session: "s2",
		Reason: "permission: Bash", At: start.Add(time.Minute)}))

	if shown.State != session.Blocked {
		t.Errorf("the Session is %s, want %s", shown.State, session.Blocked)
	}
	if shown.Reason != "permission: Bash" {
		t.Errorf("the Blocked row reads %q, want the reason the hook carried", shown.Reason)
	}
}

// A row the cross-check took has been in that state since the cross-check said
// so, as far as anything here knows. No time at all would be worse than
// under-reporting: the tree orders Attention by longest-waiting first, so a
// Session with no clock reads as one that has been Blocked since 1970 and
// pushes the one that has really been waiting down the list.
func TestACorrectedRowHasBeenInItsStateSinceTheCrossCheck(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	asked := start.Add(time.Minute)

	shown := only(t, w.crossCheck(asked, crossChecked(session.Blocked)))

	if !shown.Since.Equal(asked) {
		t.Errorf("the Session has been Blocked since %v, want the moment of the cross-check %v", shown.Since, asked)
	}
}

// Which goes for a Session the registry never reported at all, on the same
// reasoning: it has been waiting since the harness first heard of it.
func TestASessionOnlyTheCrossCheckKnowsAboutWaitsFromTheCrossCheck(t *testing.T) {
	w := watch(t)

	shown := only(t, w.crossCheck(start, missed(session.Blocked)))

	if !shown.Since.Equal(start) {
		t.Errorf("the Session has been Blocked since %v, want the moment of the cross-check %v", shown.Since, start)
	}
}

// A cross-check that could not read what a Session is doing has no opinion to
// prefer. Idle claims the least about a Session nothing can read, which is the
// right answer for a reader with nothing to fall back on — and the wrong one
// here, where it would blank every good row on the Dashboard the day Claude
// Code renames a status.
func TestACrossCheckThatCannotReadAStatusLeavesTheRowAlone(t *testing.T) {
	w := watch(t)
	blocked := running(session.Blocked, start)
	blocked.Reason = "permission: Write(config.yaml)"
	before := w.registry(blocked)

	unreadable := crossChecked(session.Idle)
	unreadable.State = ""
	after := w.crossCheck(start.Add(time.Minute), unreadable)

	if !slices.Equal(before, after) {
		t.Errorf("a cross-check that could not read the status redrew\n\t%+v\nas\n\t%+v", before, after)
	}
}

// But it still puts the Session on the Dashboard, where the registry never
// reported one. Idle is the right answer here after all: there is nothing to
// fall back on, and a row you can see and jump to beats a Session nothing
// mentions.
func TestASessionOnlyTheCrossCheckKnowsAboutWithNoReadableStatusIsIdle(t *testing.T) {
	w := watch(t)

	unreadable := missed(session.Idle)
	unreadable.State = ""
	shown := only(t, w.crossCheck(start, unreadable))

	if shown.State != session.Idle {
		t.Errorf("the Session is %s, want %s", shown.State, session.Idle)
	}
}

// One process is one row. The Dashboard keys its rows by process, so a second
// row for the same one could never be selected — the cursor would spring back
// to the first every time.
func TestTwoCrossCheckRecordsForOneProcessAreOneRow(t *testing.T) {
	w := watch(t)

	shown := w.crossCheck(start, missed(session.Idle), missed(session.Working))

	if len(shown) != 1 {
		t.Errorf("the working set holds %d rows for one process, want one: %+v", len(shown), shown)
	}
}

// The two are matched on the process, which is the field the harness has
// checked — it names something alive, and no two live Sessions share one —
// while the session id comes from a file whose shape the cross-check is here
// to insure against. A drifted id must cost a reason, not double the row.
func TestTheTwoAreMatchedOnTheProcess(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, time.Time{}))

	renamed := crossChecked(session.Working)
	renamed.ID = "not-the-id-the-registry-gave"
	shown := only(t, w.crossCheck(start, renamed))

	if shown.State != session.Working {
		t.Errorf("the Session is %s, want the cross-check's %s", shown.State, session.Working)
	}
}

// The whole reason Alerts exist: a permission prompt going up is worth a
// notification beyond the Dashboard the moment the hook says so.
func TestAPermissionPromptRaisesABlockedAlert(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))

	alert := w.alerted()
	if alert.Kind != state.AlertBlocked {
		t.Errorf("alert kind = %s, want %s", alert.Kind, state.AlertBlocked)
	}
	if alert.Session != "s1" {
		t.Errorf("the alert names Session %q, want %q", alert.Session, "s1")
	}
	if alert.Reason != "permission: Bash" {
		t.Errorf("alert reason = %q, want %q", alert.Reason, "permission: Bash")
	}
}

// Going Blocked is edge-triggered: a second hook arriving while the first
// decision is still open is not a new one to ping about. Alerts style already
// keeps the first banner up, and no-re-nagging is the whole point of it.
func TestGoingBlockedAgainWhileAlreadyBlockedDoesNotAlertTwice(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))
	w.alerted()

	w.hook(needsADecision("permission: Write(config.yaml)", start.Add(2*time.Minute)))

	w.noAlert()
}

// But a block that resolved and then recurred is a new decision, and gets its
// own Alert — the edge is on standing Blocked, not on the Session ever having
// been Blocked before.
func TestBlockedAlertsAgainAfterResolvingAndBlockingAgain(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))
	w.hook(needsADecision("permission: Bash", start.Add(time.Minute)))
	w.alerted()
	w.registry(running(session.Working, start.Add(2*time.Minute)))

	w.hook(needsADecision("permission: Write(config.yaml)", start.Add(3*time.Minute)))

	alert := w.alerted()
	if alert.Reason != "permission: Write(config.yaml)" {
		t.Errorf("alert reason = %q, want the new reason", alert.Reason)
	}
}

// Ready itself is silent — dashboard badge only. It is the first-party 60s
// idle_prompt signal, and only that, which can turn it into an Alert.
func TestATurnEndingRaisesNoAlertOnItsOwn(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))

	w.hook(turnEnded("shall I push?"))

	w.noAlert()
}

// The escalation the whole design is for: idle_prompt arrives, the turn is
// still unread, and one notification fires — carrying what the turn ended on,
// since that is what makes it worth reading.
func TestAnUnseenReadyEscalatesWhenTheIdlePromptArrives(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("shall I push?"))

	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})

	alert := w.alerted()
	if alert.Kind != state.AlertReady {
		t.Errorf("alert kind = %s, want %s", alert.Kind, state.AlertReady)
	}
	if alert.Snippet != "shall I push?" {
		t.Errorf("alert snippet = %q, want what the turn ended on", alert.Snippet)
	}
}

// The harness gates the escalation on its own seen-tracking: a Session
// already looked at must never ping just because idle_prompt happened to
// arrive anyway.
func TestASeenReadyDoesNotEscalate(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("done"))
	w.hook(hooks.Event{Kind: hooks.Seen, Session: "s1"})

	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})

	w.noAlert()
}

// A Session that was never Ready in the first place — still Working, say —
// has nothing for idle_prompt to escalate.
func TestEscalationForASessionThatIsNotReadyDoesNothing(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Working, start))

	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})

	w.noAlert()
}

// Exactly one notification fires per Ready cycle (§9), however many times
// idle_prompt itself arrives.
func TestEscalationFiresOnlyOncePerReadyCycle(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("done"))
	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})
	w.alerted()

	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})

	w.noAlert()
}

// A Session seen and left Ready again by a later turn is a new cycle, and can
// escalate again.
func TestEscalationCanFireAgainOnANewReadyCycle(t *testing.T) {
	w := watch(t)
	w.registry(running(session.Idle, start))
	w.hook(turnEnded("first"))
	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})
	w.alerted()
	w.hook(hooks.Event{Kind: hooks.Seen, Session: "s1"})

	w.hook(turnEnded("second"))
	w.hook(hooks.Event{Kind: hooks.Escalate, Session: "s1"})

	alert := w.alerted()
	if alert.Snippet != "second" {
		t.Errorf("alert snippet = %q, want %q", alert.Snippet, "second")
	}
}
