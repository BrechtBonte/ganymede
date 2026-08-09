package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// watching starts a watch that ends with the test.
func watching(t *testing.T, r registry.Registry) <-chan []session.Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	snapshots, err := r.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	return snapshots
}

// awaiting waits for a working set matching want, failing the test if the
// watch never reports one.
func awaiting(t *testing.T, snapshots <-chan []session.Session, description string, want func([]session.Session) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case sessions, ok := <-snapshots:
			if !ok {
				t.Fatalf("the watch stopped before reporting %s", description)
			}
			if want(sessions) {
				return
			}
		case <-deadline:
			t.Fatalf("the watch never reported %s", description)
		}
	}
}

// holding matches a working set of exactly these Session names.
func holding(names ...string) func([]session.Session) bool {
	return func(sessions []session.Session) bool {
		if len(sessions) != len(names) {
			return false
		}
		for i, name := range names {
			if sessions[i].Name != name {
				return false
			}
		}
		return true
	}
}

// The Dashboard has to draw something the moment it starts, so the watch
// reports the working set as it finds it before waiting for anything to change.
func TestWatchReportsTheWorkingSetItStartsWith(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "busy"})

	snapshots := watching(t, living(dir))

	awaiting(t, snapshots, "the Session already running", holding("ganymede-78"))
}

// Starting a Claude Code session anywhere puts its row on the Dashboard,
// without the Dashboard being restarted.
func TestWatchSeesASessionStart(t *testing.T) {
	dir := registryOf(t)
	snapshots := watching(t, living(dir))
	awaiting(t, snapshots, "an empty working set", holding())

	write(t, filepath.Join(dir, "200.json"),
		`{"pid":200,"cwd":"/repos/service-billing","name":"service-billing-a1","status":"idle"}`)

	awaiting(t, snapshots, "the Session that just started", holding("service-billing-a1"))
}

// Ending a session takes its row off the Dashboard: Claude Code removes the
// registry file on the way out.
func TestWatchSeesASessionEnd(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "idle"})
	snapshots := watching(t, living(dir))
	awaiting(t, snapshots, "the running Session", holding("ganymede-78"))

	if err := os.Remove(filepath.Join(dir, "100.json")); err != nil {
		t.Fatalf("remove the registry file: %v", err)
	}

	awaiting(t, snapshots, "an empty working set", holding())
}

// A Session that is killed outright leaves its registry file behind, so
// nothing in the directory changes. The row still has to go.
func TestWatchNoticesAProcessDyingWithItsFileLeftBehind(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "busy"})
	var killed atomic.Bool
	r := registry.Registry{
		Dir:   dir,
		Alive: func(int) bool { return !killed.Load() },
		// Only the poll can notice this, so run it at test speed.
		Poll: 20 * time.Millisecond,
	}
	snapshots := watching(t, r)
	awaiting(t, snapshots, "the running Session", holding("ganymede-78"))

	killed.Store(true)

	awaiting(t, snapshots, "the killed Session gone", holding())
}

// Claude Code rewrites these files constantly, most often with the same
// contents. Redrawing the Dashboard for every write would make it flicker.
func TestWatchStaysQuietWhenNothingHasChanged(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "busy"})
	r := living(dir)
	// Long enough that only a directory change could report anything.
	r.Poll = time.Hour
	snapshots := watching(t, r)
	awaiting(t, snapshots, "the running Session", holding("ganymede-78"))

	write(t, filepath.Join(dir, "100.json"),
		`{"pid":100,"name":"ganymede-78","status":"busy","statusUpdatedAt":0}`)

	select {
	case sessions := <-snapshots:
		t.Errorf("the watch reported %+v after a write that changed nothing", sessions)
	case <-time.After(300 * time.Millisecond):
	}
}

// Claude Code writes a registry file in a burst, and a file caught between two
// of those writes is half a file. Nothing may read one — neither the directory
// watch nor the poll: the Session would blink off the Dashboard and take the
// selection with it, and a jump pressed at that moment would go nowhere.
func TestWatchDoesNotDropASessionWhoseFileIsBeingWritten(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "busy"})
	r := living(dir)
	// Faster than the settle delay, so a poll that read the moment it fired
	// would land inside the burst below many times over.
	r.Poll = 5 * time.Millisecond
	snapshots := watching(t, r)
	awaiting(t, snapshots, "the running Session", holding("ganymede-78"))

	path := filepath.Join(dir, "100.json")
	whole := `{"pid":100,"name":"ganymede-78","status":"busy","statusUpdatedAt":0}`
	burst := make(chan struct{})
	go func() {
		defer close(burst)
		for range 40 {
			_ = os.WriteFile(path, []byte(whole[:30]), 0o644)
			time.Sleep(5 * time.Millisecond)
			_ = os.WriteFile(path, []byte(whole), 0o644)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// The working set never changes, so the only snapshot the watch could
	// report is one that has lost the Session.
	for {
		select {
		case sessions, ok := <-snapshots:
			if !ok {
				t.Fatal("the watch stopped reporting")
			}
			t.Fatalf("the watch reported %+v while the Session's file was being written", sessions)
		case <-burst:
			return
		}
	}
}

// A registry the harness cannot read at all has to say so. Drawing "No
// sessions." would be a confident lie, with Sessions sitting Blocked behind it.
func TestWatchRefusesARegistryItCannotRead(t *testing.T) {
	unreadable := filepath.Join(t.TempDir(), "sessions")
	write(t, unreadable, "not a directory")

	_, err := living(unreadable).Watch(t.Context())

	if err == nil {
		t.Error("Watch reported an empty working set for a registry it cannot read")
	}
}

// A harness started before Claude Code has ever run finds no registry
// directory to watch at all. Sessions started afterwards still have to appear.
//
// This only pins down that they appear. That they appear promptly rests on the
// poll putting the directory watch back once there is one to put back, which
// is not something the watch can be asked about from out here.
func TestWatchPicksUpARegistryDirectoryCreatedLater(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	r := living(dir)
	r.Poll = 20 * time.Millisecond
	snapshots := watching(t, r)
	awaiting(t, snapshots, "an empty working set", holding())

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create the registry directory: %v", err)
	}
	write(t, filepath.Join(dir, "100.json"), `{"pid":100,"name":"ganymede-78","status":"idle"}`)

	awaiting(t, snapshots, "the Session started after the registry appeared", holding("ganymede-78"))
}

// The watch belongs to whoever started it: ending its context ends it, rather
// than leaving a goroutine on the registry directory.
func TestWatchEndsWithItsContext(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "ganymede-78", Status: "busy"})
	ctx, cancel := context.WithCancel(context.Background())
	snapshots, err := living(dir).Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	cancel()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-snapshots:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the watch is still reporting after its context ended")
		}
	}
}
