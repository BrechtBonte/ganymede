package state_test

import (
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/hooks"
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
	hooks     chan hooks.Event
	merged    <-chan []session.Session
}

func watch(t *testing.T) *watching {
	t.Helper()
	w := &watching{
		t:         t,
		model:     state.New(),
		snapshots: make(chan []session.Session),
		hooks:     make(chan hooks.Event),
	}
	w.merged = w.model.Watch(t.Context(), w.snapshots, w.hooks)
	return w
}

// registry is what the registry says the working set is now.
func (w *watching) registry(sessions ...session.Session) []session.Session {
	w.t.Helper()
	w.snapshots <- sessions
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
