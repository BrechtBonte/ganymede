package reconciler_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/reconciler"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// claudeThat writes a stand-in for the Claude Code binary: one that takes a
// while to answer, prints says, and exits with code. It records what it was
// asked beside itself, which asked reads back.
func claudeThat(t *testing.T, takes time.Duration, says string, code int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\nsleep %v\ncat %q\nexit %d\n",
		filepath.Join(dir, "asked"), takes.Seconds(), filepath.Join(dir, "says"), code)

	if err := os.WriteFile(filepath.Join(dir, "says"), []byte(says), 0o644); err != nil {
		t.Fatalf("write what the cross-check says: %v", err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write the stand-in Claude Code: %v", err)
	}
	return path
}

// claudeSaying is a Claude Code that answers at once.
func claudeSaying(t *testing.T, says string) string {
	t.Helper()
	return claudeThat(t, 0, says, 0)
}

// asked is what the stand-in Claude Code was asked, the last time it was run.
func asked(t *testing.T, claude string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(filepath.Dir(claude), "asked"))
	if err != nil {
		t.Fatalf("read what Claude Code was asked: %v", err)
	}
	return string(body)
}

func crossChecking(claude string) reconciler.Reconciler {
	return reconciler.Reconciler{Claude: claude}
}

func read(t *testing.T, r reconciler.Reconciler) reconciler.Reconciled {
	t.Helper()
	checked, err := r.Read(t.Context())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return checked
}

// The whole point of the reconciler is that it asks the documented interface
// rather than reading the undocumented files. Which words it says are the one
// thing this component rests on.
func TestAsksClaudeCodeForItsAgentsAsJSON(t *testing.T) {
	claude := claudeSaying(t, `[]`)

	read(t, crossChecking(claude))

	if got := asked(t, claude); got != "agents --json" {
		t.Errorf("Claude Code was asked %q, want %q", got, "agents --json")
	}
}

// Every field the Dashboard draws that the cross-check can speak to.
func TestReadsWhatTheCrossCheckSaysAboutASession(t *testing.T) {
	claude := claudeSaying(t, `[
	  {
	    "pid": 72144,
	    "cwd": "/Users/brechtbonte/Projects/BrechtBonte/ganymede",
	    "kind": "interactive",
	    "startedAt": 1786272346700,
	    "sessionId": "eae57452-0438-4270-b521-d7a6d868fb75",
	    "name": "ganymede-78",
	    "status": "busy"
	  }
	]`)

	checked := read(t, crossChecking(claude))

	want := session.Session{
		PID:   72144,
		ID:    "eae57452-0438-4270-b521-d7a6d868fb75",
		Dir:   "/Users/brechtbonte/Projects/BrechtBonte/ganymede",
		Name:  "ganymede-78",
		State: session.Working,
	}
	if len(checked.Sessions) != 1 {
		t.Fatalf("read %d Sessions, want 1: %+v", len(checked.Sessions), checked.Sessions)
	}
	if checked.Sessions[0] != want {
		t.Errorf("read\n\t%+v\nwant\n\t%+v", checked.Sessions[0], want)
	}
}

// The same status vocabulary the registry writes, read the same way — Ready
// aside, which is the harness's own and neither of theirs.
func TestTheCrossChecksStatusBecomesASessionState(t *testing.T) {
	for _, c := range []struct {
		status string
		want   session.State
	}{
		{"busy", session.Working},
		{"waiting", session.Blocked},
		{"idle", session.Idle},
		{"shell", session.Shell},
	} {
		t.Run(c.status, func(t *testing.T) {
			claude := claudeSaying(t, `[{"pid":1,"status":"`+c.status+`"}]`)

			if got := read(t, crossChecking(claude)).Sessions[0].State; got != c.want {
				t.Errorf("status %q reads as %s, want %s", c.status, got, c.want)
			}
		})
	}
}

// A status from a Claude Code newer than this harness is no state at all,
// rather than the Idle a reader with nothing else to go on would settle for.
// What this reader reports is about to be preferred over the registry's
// account, and a default dressed up as an answer would take a Dashboard full
// of good rows down with it the day a status gets renamed.
func TestAStatusTheHarnessCannotReadIsNoStateAtAll(t *testing.T) {
	claude := claudeSaying(t, `[{"pid":1,"status":"transcendent"}]`)

	if got := read(t, crossChecking(claude)).Sessions[0].State; got != "" {
		t.Errorf("a status this harness cannot read is %s, want no state at all", got)
	}
}

// A cross-check that never answers must not take the reconciler with it. The
// watch makes them one after another, so one Claude Code that hangs would be
// the last cross-check ever made — and the picture it left behind would go on
// overruling the registry for the rest of the day.
func TestACrossCheckThatHangsIsGivenUpOn(t *testing.T) {
	r := crossChecking(claudeThat(t, 10*time.Second, `[]`, 0))
	r.Timeout = 100 * time.Millisecond

	waited := time.Now()
	_, err := r.Read(t.Context())

	if err == nil {
		t.Error("a cross-check that hung reported a working set rather than an error")
	}
	if held := time.Since(waited); held > 5*time.Second {
		t.Errorf("the cross-check was waited on for %v, want giving up after %v", held, r.Timeout)
	}
}

// The cross-check says when a Session started, which is not when it entered
// the state it is in. Reading one as the other would put a wait age of hours
// on a Session that has been Blocked for a moment.
func TestTheCrossCheckCannotSayWhenASessionEnteredItsState(t *testing.T) {
	claude := claudeSaying(t, `[{"pid":1,"status":"waiting","startedAt":1786272346700}]`)

	if got := read(t, crossChecking(claude)).Sessions[0].Since; !got.IsZero() {
		t.Errorf("Since = %v, want no time at all", got)
	}
}

// The stamp is what the state model weighs a registry record against, so it
// has to be the moment the picture was asked for. Stamping it when the answer
// came back would let a slow cross-check overrule a registry record written
// while it was still running.
func TestTheStampIsTheMomentTheCrossCheckWasAsked(t *testing.T) {
	claude := claudeThat(t, 300*time.Millisecond, `[]`, 0)

	asking := time.Now()
	checked := read(t, crossChecking(claude))

	if late := checked.At.Sub(asking); late > 100*time.Millisecond {
		t.Errorf("stamped %v after the cross-check was asked, want the moment it was asked", late)
	}
}

// A Claude Code that cannot answer is not a working set of no Sessions: the
// Dashboard would lose every row the registry never saw.
func TestACrossCheckThatCannotAnswerIsAnError(t *testing.T) {
	for _, c := range []struct {
		what   string
		claude string
	}{
		{"a Claude Code that failed", claudeThat(t, 0, "", 1)},
		{"an answer in a shape the harness cannot read", claudeSaying(t, "not JSON at all")},
		{"no Claude Code on the machine", filepath.Join(t.TempDir(), "claude")},
	} {
		t.Run(c.what, func(t *testing.T) {
			if _, err := crossChecking(c.claude).Read(t.Context()); err == nil {
				t.Errorf("%s reported a working set rather than an error", c.what)
			}
		})
	}
}

// A record whose shape has moved far enough that the harness cannot even find
// the process costs its own Session and no more.
func TestARecordWithNoProcessIsNotASession(t *testing.T) {
	claude := claudeSaying(t, `[{"pid":100,"name":"readable","status":"idle"},{"name":"shapeless","status":"idle"}]`)

	found := read(t, crossChecking(claude)).Sessions

	if len(found) != 1 || found[0].Name != "readable" {
		t.Errorf("read %+v, want only the Session the harness could read", found)
	}
}

// awaiting takes the next cross-check off the watch.
func awaiting(t *testing.T, checks <-chan reconciler.Reconciled, description string) reconciler.Reconciled {
	t.Helper()
	select {
	case checked, ok := <-checks:
		if !ok {
			t.Fatalf("the watch stopped before reporting %s", description)
		}
		return checked
	case <-time.After(5 * time.Second):
		t.Fatalf("the watch never reported %s", description)
		return reconciler.Reconciled{}
	}
}

// names is what the cross-check called the Sessions it found.
func names(checked reconciler.Reconciled) string {
	found := make([]string, len(checked.Sessions))
	for i, s := range checked.Sessions {
		found[i] = s.Name
	}
	return strings.Join(found, ", ")
}

// The cross-check runs without being asked, and the first one does not wait
// for the timer: a Session the registry watch cannot see at all should not
// stay off the Dashboard for the length of a slow tick after it comes up.
func TestWatchCrossChecksBeforeItsFirstTick(t *testing.T) {
	r := crossChecking(claudeSaying(t, `[{"pid":100,"name":"ganymede-78","status":"busy"}]`))
	r.Every = time.Hour

	checked := awaiting(t, r.Watch(t.Context()), "the first cross-check")

	if got := names(checked); got != "ganymede-78" {
		t.Errorf("the first cross-check found %q, want the running Session", got)
	}
}

// And it keeps going, which is what makes it a reconciler rather than a
// reading taken at startup.
func TestWatchKeepsCrossCheckingOnItsTimer(t *testing.T) {
	r := crossChecking(claudeSaying(t, `[{"pid":100,"name":"ganymede-78","status":"busy"}]`))
	r.Every = 20 * time.Millisecond
	checks := r.Watch(t.Context())

	first := awaiting(t, checks, "the first cross-check")
	next := awaiting(t, checks, "the cross-check after it")

	if !next.At.After(first.At) {
		t.Errorf("the second cross-check is stamped %v, want a moment after the first at %v", next.At, first.At)
	}
}

// A cross-check that cannot be made says nothing at all, leaving the harness
// with whatever the registry and the last cross-check gave it. It is the
// insurance, not something the Dashboard cannot run without.
func TestWatchSaysNothingWhenTheCrossCheckCannotBeMade(t *testing.T) {
	r := crossChecking(filepath.Join(t.TempDir(), "claude"))
	r.Every = 20 * time.Millisecond
	checks := r.Watch(t.Context())

	select {
	case checked, ok := <-checks:
		if ok {
			t.Errorf("the watch reported %+v from a Claude Code that is not there", checked)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// The watch belongs to whoever started it: ending its context ends it, rather
// than leaving a goroutine running Claude Code forever.
func TestWatchEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := crossChecking(claudeSaying(t, `[{"pid":100,"name":"ganymede-78","status":"busy"}]`))
	r.Every = 20 * time.Millisecond
	checks := r.Watch(ctx)
	awaiting(t, checks, "the first cross-check")

	cancel()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-checks:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the watch is still cross-checking after its context ended")
		}
	}
}
