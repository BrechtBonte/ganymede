package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/hooks"
)

const stopPayload = `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"done"}`

// shortDir is a throwaway directory with a short path. A unix socket path is
// capped at 104 bytes on macOS, and t.TempDir spends most of that on the name
// of the test asking for it.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gny")
	if err != nil {
		t.Fatalf("temporary directory: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// receiving starts a Dashboard's receiver on a throwaway socket, and returns
// the socket a Session would report to.
func receiving(t *testing.T) (string, <-chan hooks.Event, context.CancelFunc) {
	t.Helper()
	socket := filepath.Join(shortDir(t), "events.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	events, err := hooks.Listen(ctx, socket)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return socket, events, cancel
}

// forward reports an event the way a hook command does.
func forward(t *testing.T, socket, payload string) {
	t.Helper()
	if err := hooks.Forward(socket, []byte(payload)); err != nil {
		t.Fatalf("Forward: %v", err)
	}
}

// next is the event the receiver hands over, or a failure if none does.
func next(t *testing.T, events <-chan hooks.Event) hooks.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("the receiver stopped before the event arrived")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
		return hooks.Event{}
	}
}

// The whole point of the socket: what a Session's hook was handed reaches the
// Dashboard.
func TestAForwardedPayloadArrivesAsAnEvent(t *testing.T) {
	socket, events, _ := receiving(t)

	forward(t, socket, stopPayload)

	event := next(t, events)
	if event.Kind != hooks.Finished || event.Session != "s1" {
		t.Errorf("received %+v, want the Session's turn ending", event)
	}
	// Hook payloads carry no clock, and the state model weighs this against
	// the registry's, so the receiver has to stamp it.
	if event.At.IsZero() {
		t.Error("the event arrived without a time on it")
	}
}

// Events reach the state model in the order they happened. A turn ending and
// the jump that reads it are milliseconds apart, and the wrong way round they
// leave an unread badge on a Session you are looking at.
func TestEventsArriveInTheOrderTheyHappened(t *testing.T) {
	socket, events, _ := receiving(t)

	forward(t, socket, stopPayload)
	forward(t, socket, string(hooks.SeenPayload("s1")))

	if first := next(t, events).Kind; first != hooks.Finished {
		t.Errorf("first event is %s, want %s", first, hooks.Finished)
	}
	if second := next(t, events).Kind; second != hooks.Seen {
		t.Errorf("second event is %s, want %s", second, hooks.Seen)
	}
}

// Hook commands run inside a Session's turn. Nothing the harness does there is
// allowed to hold a Session up, least of all the Dashboard being closed.
func TestForwardingWithNoDashboardListeningGivesUpQuickly(t *testing.T) {
	nowhere := filepath.Join(shortDir(t), "events.sock")

	start := time.Now()
	err := hooks.Forward(nowhere, []byte(stopPayload))

	if err == nil {
		t.Error("forwarding to a Dashboard that is not there reported success")
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("forwarding took %s with nobody listening", took)
	}
}

// A Dashboard that was killed leaves its socket file behind, and the next one
// has to be able to start anyway — the harness comes up when you tell it to,
// not when the filesystem is tidy.
func TestTheReceiverTakesOverASocketLeftBehind(t *testing.T) {
	dir := shortDir(t)
	socket := filepath.Join(dir, "events.sock")
	if err := os.WriteFile(socket, []byte("a dead Dashboard's socket"), 0o600); err != nil {
		t.Fatalf("leave a socket behind: %v", err)
	}

	events, err := hooks.Listen(t.Context(), socket)
	if err != nil {
		t.Fatalf("Listen over a socket left behind: %v", err)
	}

	forward(t, socket, stopPayload)
	if event := next(t, events); event.Session != "s1" {
		t.Errorf("received %+v, want the Session's turn ending", event)
	}
}

// The receiver belongs to the Dashboard: when the Dashboard goes, so does the
// socket, or the next one inherits a path that answers to nobody.
func TestTheReceiverLetsGoOfItsSocketWhenItStops(t *testing.T) {
	socket, events, stop := receiving(t)

	stop()

	select {
	case _, ok := <-events:
		if ok {
			t.Error("the receiver handed over an event after it stopped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the receiver did not stop with its context")
	}
	if _, err := os.Stat(socket); err == nil {
		t.Errorf("the socket %s outlived the Dashboard", socket)
	}
}

// The socket is a hole in the Dashboard that anything on the machine can write
// to. Whatever arrives, the next real event still has to get through.
func TestNonsenseOnTheSocketDoesNotStopTheReceiver(t *testing.T) {
	socket, events, _ := receiving(t)

	forward(t, socket, "not json at all")
	forward(t, socket, `{"hook_event_name":"Stop"}`)
	forward(t, socket, strings.Repeat("x", 2<<20))
	forward(t, socket, stopPayload)

	if event := next(t, events); event.Session != "s1" {
		t.Errorf("received %+v, want the Session's turn ending", event)
	}
}

// One Dashboard owns the socket. A second one taking it would leave the first
// blocked on a socket nothing points at any more — still drawing the registry,
// never showing a Ready badge again, and saying nothing about why.
func TestASecondDashboardWillNotTakeTheSocketFromALiveOne(t *testing.T) {
	socket, events, _ := receiving(t)

	_, err := hooks.Listen(t.Context(), socket)

	if err == nil {
		t.Fatal("a second Dashboard took the socket from a live one")
	}
	forward(t, socket, stopPayload)
	if event := next(t, events); event.Session != "s1" {
		t.Errorf("the first Dashboard received %+v, want the event it was still listening for", event)
	}
}

// And when it stops, it takes its own socket with it and leaves anybody else's
// alone.
func TestAStoppedReceiverLeavesTheNextOnesSocketAlone(t *testing.T) {
	socket, events, stop := receiving(t)
	stop()
	<-events

	if _, err := hooks.Listen(t.Context(), socket); err != nil {
		t.Fatalf("Listen after the first stopped: %v", err)
	}
	// The first one's cleanup has already run; nothing of it may touch the
	// path the second is now listening on.
	if _, err := os.Stat(socket); err != nil {
		t.Errorf("the second Dashboard's socket is gone: %v", err)
	}
}
