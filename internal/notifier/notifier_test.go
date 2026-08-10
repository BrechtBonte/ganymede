package notifier_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/notifier"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/state"
	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// recording is a Sender that remembers what it was asked to show.
type recording struct {
	sent chan notifier.Notification
}

func recorder() *recording { return &recording{sent: make(chan notifier.Notification, 8)} }

func (r *recording) Send(n notifier.Notification) error {
	r.sent <- n
	return nil
}

// shown waits for the next Notification the Notifier sent.
func (r *recording) shown(t *testing.T) notifier.Notification {
	t.Helper()
	select {
	case n := <-r.sent:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("no Notification was sent")
		return notifier.Notification{}
	}
}

// nothingShown asserts that no Notification arrived.
func (r *recording) nothingShown(t *testing.T) {
	t.Helper()
	select {
	case n := <-r.sent:
		t.Fatalf("a Notification was sent, want none: %+v", n)
	case <-time.After(50 * time.Millisecond):
	}
}

// run starts a Notifier over fresh sessions and alerts channels, both ended
// when the test does.
func run(t *testing.T, n notifier.Notifier) (chan []session.Session, chan state.Alert) {
	t.Helper()
	sessions := make(chan []session.Session)
	alerts := make(chan state.Alert)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go n.Run(ctx, sessions, alerts)
	return sessions, alerts
}

// send hands v to ch, failing the test rather than hanging forever if the
// Notifier is not there to take it.
func send[T any](t *testing.T, ch chan T, v T) {
	t.Helper()
	select {
	case ch <- v:
	case <-time.After(2 * time.Second):
		t.Fatal("the Notifier did not take the value in time")
	}
}

var blockedSession = session.Session{PID: 4242, ID: "s1", Dir: "/repos/service-billing"}

// The whole reason the state model raises AlertBlocked: it is worth
// interrupting you for, with the sound to prove it.
func TestABlockedAlertSoundsAndCarriesTheReason(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{Send: sender})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	shown := sender.shown(t)
	if shown.Body != "permission: Bash" {
		t.Errorf("body = %q, want the Blocked reason", shown.Body)
	}
	if !shown.Sound {
		t.Error("a Blocked notification did not sound")
	}
}

// Ready's escalation is silent — a badge, not a bang.
func TestAReadyAlertIsSilentAndCarriesTheSnippet(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{Send: sender})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertReady, Session: "s1", Snippet: "shall I push?"})

	shown := sender.shown(t)
	if shown.Body != "shall I push?" {
		t.Errorf("body = %q, want what the turn ended on", shown.Body)
	}
	if shown.Sound {
		t.Error("a Ready escalation sounded, want it silent")
	}
}

// Focus-aware: the sidepanel already shows the same state while Ghostty is in
// front of you, so no banner fires.
func TestNoBannerFiresWhileGhosttyIsFrontmost(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:      sender,
		Frontmost: func() (bool, error) { return true, nil },
	})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	sender.nothingShown(t)
}

// The moment Ghostty is not in front, the same Alert fires.
func TestABannerFiresWhenGhosttyIsNotFrontmost(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:      sender,
		Frontmost: func() (bool, error) { return false, nil },
	})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	sender.shown(t)
}

// A question that could not be answered must not cost you an Alert the state
// model already decided was worth raising.
func TestAFrontmostErrorFailsOpen(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:      sender,
		Frontmost: func() (bool, error) { return false, errors.New("no System Events") },
	})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	sender.shown(t)
}

// fakeTickets answers Of the way the Dashboard's own Tickets would.
type fakeTickets map[string]ticket.Key

func (f fakeTickets) Of(dir, root string) ticket.Key { return f[dir] }

// The anatomy §9 asks for: repo, then ticket.
func TestTheTitleNamesTheRepoAndTicket(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:    sender,
		Tickets: fakeTickets{"/repos/service-billing": "FIRE-2841"},
	})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	shown := sender.shown(t)
	if want := "service-billing · FIRE-2841"; shown.Title != want {
		t.Errorf("title = %q, want %q", shown.Title, want)
	}
}

// A Session about no ticket still gets a title — just the repo.
func TestTheTitleIsJustTheRepoWithNoTicket(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{Send: sender, Tickets: fakeTickets{}})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	shown := sender.shown(t)
	if want := "service-billing"; shown.Title != want {
		t.Errorf("title = %q, want %q", shown.Title, want)
	}
}

// Clicking a notification has to know which Session to jump to — the pid is
// what Jump takes.
func TestTheClickCommandCarriesThePid(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:  sender,
		Click: func(pid int) []string { return []string{"ganymede", "notify-click", strconv.Itoa(pid)} },
	})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	shown := sender.shown(t)
	if want := []string{"ganymede", "notify-click", "4242"}; !slices.Equal(shown.Click, want) {
		t.Errorf("click = %v, want %v", shown.Click, want)
	}
}

// An Alert for a Session the working set has never mentioned — the hooks run
// ahead of the registry, so an Alert can arrive before its own Session does —
// must not panic, has no pid to jump to, and must not guess at a repo. A
// blank title beats a confidently wrong one.
func TestAnAlertForASessionNotInTheWorkingSetIsHarmless(t *testing.T) {
	sender := recorder()
	sessions, alerts := run(t, notifier.Notifier{
		Send:  sender,
		Click: func(pid int) []string { return []string{"ganymede", "notify-click", strconv.Itoa(pid)} },
	})
	send(t, sessions, []session.Session{})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "gone", Reason: "permission: Bash"})

	shown := sender.shown(t)
	if len(shown.Click) != 0 {
		t.Errorf("click = %v, want none for a Session with no pid", shown.Click)
	}
	if shown.Title != "" {
		t.Errorf("title = %q, want none for a Session this Notifier has never seen", shown.Title)
	}
}

// blockingSender stands in for a Sender whose OS call has stalled — a
// terminal-notifier hung on a first-run permission dialog, say.
type blockingSender struct {
	entered chan struct{}
	release <-chan struct{}
}

func (b *blockingSender) Send(notifier.Notification) error {
	close(b.entered)
	<-b.release
	return nil
}

// A stuck Send must not stop the Notifier from taking the next working set.
// sessions and alerts are fed from the same fan-out as the Dashboard's own
// channel (main.go's fanned): a Notifier that cannot keep reading would back
// that fan-out up all the way to the state model, freezing session updates
// along with notifications — not just the notification that is stuck.
func TestAStuckSendDoesNotBlockLaterSessionUpdates(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	sender := &blockingSender{entered: make(chan struct{}), release: release}

	sessions, alerts := run(t, notifier.Notifier{Send: sender})
	send(t, sessions, []session.Session{blockedSession})
	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})

	select {
	case <-sender.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Send was never called")
	}

	// If fire ran inline in the Notifier's select loop, this would time out:
	// the loop would still be inside the stuck Send call above.
	send(t, sessions, []session.Session{{PID: 9999, ID: "s2", Dir: "/repos/service-ai-assistant"}})
}

// A Notifier given no Sender still decides — it just has nothing to hand the
// decision to. Nothing here may panic over it.
func TestANotifierWithNoSenderDoesNotPanic(t *testing.T) {
	sessions, alerts := run(t, notifier.Notifier{})
	send(t, sessions, []session.Session{blockedSession})

	send(t, alerts, state.Alert{Kind: state.AlertBlocked, Session: "s1", Reason: "permission: Bash"})
	// Nothing to assert: the point is that this returns at all. A later Alert
	// on the same channel proves the goroutine is still alive to take it.
	send(t, alerts, state.Alert{Kind: state.AlertReady, Session: "s1", Snippet: "done"})
}
