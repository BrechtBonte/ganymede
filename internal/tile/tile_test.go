package tile_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/tile"
)

// CountsIn counts a working set by tier — Blocked, Ready and Working each
// tallied on their own, and Idle/Shell contributing to none of the three:
// they are states nothing on the dropdown is about.
func TestCountsInCountsEachTierAndIgnoresTheRest(t *testing.T) {
	for _, c := range []struct {
		name     string
		sessions []session.Session
		want     tile.Counts
	}{
		{"empty", nil, tile.Counts{}},
		{"one of each", []session.Session{
			{State: session.Blocked}, {State: session.Ready}, {State: session.Working},
		}, tile.Counts{Blocked: 1, Ready: 1, Working: 1}},
		{"several of one tier", []session.Session{
			{State: session.Working}, {State: session.Working}, {State: session.Working},
		}, tile.Counts{Working: 3}},
		{"idle and shell count toward nothing", []session.Session{
			{State: session.Idle}, {State: session.Shell}, {State: session.Blocked},
		}, tile.Counts{Blocked: 1}},
	} {
		if got := tile.CountsIn(c.sessions); got != c.want {
			t.Errorf("%s: CountsIn(%+v) = %+v, want %+v", c.name, c.sessions, got, c.want)
		}
	}
}

// pipe is the tile process's stdin, as a test can read it back.
type pipe struct {
	written strings.Builder
	closed  bool
	err     error
}

func (p *pipe) Write(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	return p.written.Write(b)
}

func (p *pipe) Close() error {
	p.closed = true
	return nil
}

// spawning is a Tile whose process is the pipe handed back here, counting how
// many times it was asked for one. The run it hands back never reports an end
// of its own: it is a tile that is still up.
func spawning(p *pipe, err error) (*tile.Tile, *int) {
	starts := 0
	return &tile.Tile{Start: func() (io.WriteCloser, tile.Ended, error) {
		starts++
		return p, make(chan error), err
	}}, &starts
}

// ending is a run that has already finished: nil for a tile that quit itself,
// an error for one that was lost.
func ending(err error) tile.Ended {
	report := make(chan error, 1)
	report <- err
	return report
}

// run is one run of the tile process a test sets up: the pipe its counts go
// down, and how that run ends.
type run struct {
	pipe  *pipe
	ended tile.Ended
}

// spawningRuns is a Tile handing back the given runs in order, one per start,
// so a test can say what the tile it is replacing did with its last breath.
func spawningRuns(t *testing.T, runs ...run) (*tile.Tile, *int) {
	t.Helper()
	starts := 0
	return &tile.Tile{Start: func() (io.WriteCloser, tile.Ended, error) {
		if starts == len(runs) {
			t.Fatalf("the Tile was started %d times, one more than the test set up", starts+1)
		}
		starts++
		return runs[starts-1].pipe, runs[starts-1].ended, nil
	}}, &starts
}

// The first Badge is what puts the Tile on screen, even with every count at
// zero: the harness being up is itself worth showing, and the icon appearing
// only once something blocked would leave nothing to click the rest of the
// time.
func TestTheFirstBadgeStartsTheTileWithAllCountsAtZero(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	if err := tl.Badge(tile.Counts{}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
	if p.written.String() != "0 0 0\n" {
		t.Errorf("the Tile was sent %q, want all three counts at zero", p.written.String())
	}
}

// One line per change, all three counts on it, so the tile process can read a
// whole set of counts at once.
func TestBadgeSendsAllThreeCountsAsALine(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)

	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if p.written.String() != "2 0 0\n" {
		t.Errorf("the Tile was sent %q, want %q", p.written.String(), "2 0 0\n")
	}
}

// The working set is rebuilt whenever anything at all moves. Counts that have
// not moved are not worth a second write, but unlike the Dock badge and the
// menu-bar title — which only ever read Blocked — the dropdown reads all
// three, so Ready or Working moving on their own is a real change here too.
func TestARepeatOfTheExactSameCountsIsNotSentAgainButAnyFieldMovingIs(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	for _, counts := range []tile.Counts{
		{Blocked: 1},
		{Blocked: 1},
		{Blocked: 1, Ready: 4},
		{Blocked: 1, Ready: 4},
		{Blocked: 1, Ready: 4, Working: 2},
	} {
		if err := tl.Badge(counts); err != nil {
			t.Fatalf("Badge: %v", err)
		}
	}

	want := "1 0 0\n1 4 0\n1 4 2\n"
	if p.written.String() != want {
		t.Errorf("the Tile was sent %q, want %q", p.written.String(), want)
	}
	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// Quitting the tile from its own Dock menu is a gesture, not a fault: the
// process goes on its own terms and says so by exiting cleanly. Answering that
// with a fresh tile on the next Session to block would be the harness arguing
// with you, so the Tile stays gone until the Dashboard is next started.
func TestATileQuitFromItsDockMenuStaysGone(t *testing.T) {
	quit := &pipe{err: errors.New("write |1: file already closed")}
	tl, starts := spawningRuns(t, run{quit, ending(nil)})

	if err := tl.Badge(tile.Counts{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a pipe that is gone")
	}
	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Errorf("a retired Tile complained again: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want the quit one left alone", *starts)
	}
}

// A tile that was killed, crashed, or went down with something larger than
// itself never chose to go. The Dashboard it belongs to stays up for weeks, so
// treating that the way a Quit is treated costs the harness its whole presence
// outside the emulator window for the rest of them. The count that noticed
// brings the tile back and lands on the one it comes back as.
func TestATileLostWithoutBeingQuitComesBack(t *testing.T) {
	lost := &pipe{err: errors.New("write |1: broken pipe")}
	replacement := &pipe{}
	tl, starts := spawningRuns(t,
		run{lost, ending(errors.New("signal: killed"))},
		run{replacement, ending(nil)},
	)

	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge on a tile that was lost: %v", err)
	}

	if *starts != 2 {
		t.Errorf("the Tile was started %d times, want the lost one replaced", *starts)
	}
	if replacement.written.String() != "1 0 0\n" {
		t.Errorf("the replacement tile was sent %q, want the count the lost one could not take", replacement.written.String())
	}
}

// Counts stand still for hours at a time, and a tile lost during one of them
// must not wait on the working set to move before it comes back: nothing
// would be written, so nothing would notice it had gone. The next working set
// brings it back whether or not it counts differently to the last.
func TestATileLostWhileTheCountsStoodStillComesBackAnyway(t *testing.T) {
	first := &pipe{}
	replacement := &pipe{}
	lost := make(chan error, 1)
	tl, starts := spawningRuns(t,
		run{first, lost},
		run{replacement, ending(nil)},
	)
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	lost <- errors.New("signal: killed")

	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge on a count that had not moved: %v", err)
	}

	if *starts != 2 {
		t.Errorf("the Tile was started %d times, want the lost one replaced", *starts)
	}
	if replacement.written.String() != "1 0 0\n" {
		t.Errorf("the replacement tile was sent %q, want the count that stood still", replacement.written.String())
	}
}

// A tile quit while the counts stood still is still a gesture: the Tile
// retires on the next working set rather than replacing it.
func TestATileQuitWhileTheCountsStoodStillStaysGone(t *testing.T) {
	first := &pipe{}
	quit := make(chan error, 1)
	tl, starts := spawningRuns(t, run{first, quit})
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	quit <- nil

	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Errorf("a retired Tile complained: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want the quit one left alone", *starts)
	}
}

// A replacement that will not take the count either is the end of it: the
// bundle is gone, or something about this machine will not run it, and a
// Dashboard retrying on every Session that blocks would be a Dashboard
// spawning processes all day for an icon that never appears.
func TestATileWhoseReplacementAlsoFailsIsRetired(t *testing.T) {
	lost := &pipe{err: errors.New("write |1: broken pipe")}
	alsoLost := &pipe{err: errors.New("write |1: broken pipe")}
	tl, starts := spawningRuns(t,
		run{lost, ending(errors.New("signal: killed"))},
		run{alsoLost, ending(errors.New("signal: killed"))},
	)

	if err := tl.Badge(tile.Counts{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a tile that would not take the count twice")
	}
	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Errorf("a retired Tile complained again: %v", err)
	}

	if *starts != 2 {
		t.Errorf("the Tile was started %d times, want two tries and no more", *starts)
	}
}

// A tile that could not be started is retired the same way, so a Dashboard
// does not try to spawn a process it has already failed to spawn on every
// Session that blocks for the rest of the day.
func TestATileThatCouldNotBeStartedIsNotStartedAgain(t *testing.T) {
	tl, starts := spawning(&pipe{}, errors.New("fork/exec: no such file or directory"))

	if err := tl.Badge(tile.Counts{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a tile that could not be started")
	}
	_ = tl.Badge(tile.Counts{Blocked: 2})

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// A harness whose launcher was never installed has no Tile, which is not a
// failure and not worth an error: everything else about the Dashboard works
// exactly as it did.
func TestATileWithNoAppIsSilent(t *testing.T) {
	tl := &tile.Tile{}

	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Errorf("a harness with no launcher installed reported %v", err)
	}
}

// Closing is what a Dashboard quit by hand does on the way out. EOF would
// take the tile down anyway once the process ends, but the pipe closing is
// what makes the icon go at the moment you quit.
func TestCloseClosesThePipe(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)
	_ = tl.Badge(tile.Counts{Blocked: 1})

	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !p.closed {
		t.Error("Close left the pipe open")
	}
}

// A Dashboard on its way out closes the Tile, and nothing brings one back
// behind it: a tile spawned after the process telling it the count has gone
// would stand for a count nobody is left to correct.
func TestAClosedTileIsNotBroughtBack(t *testing.T) {
	p := &pipe{}
	tl, starts := spawningRuns(t, run{p, ending(nil)})
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}
	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Errorf("a closed Tile complained: %v", err)
	}
	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want none spawned behind a closed one", *starts)
	}
}

// Closing a Tile that never started is not an error either — a Dashboard
// quits the same way whether or not anything ever blocked.
func TestClosingATileThatNeverStartedIsFine(t *testing.T) {
	tl, _ := spawning(&pipe{}, nil)

	if err := tl.Close(); err != nil {
		t.Errorf("Close on an unstarted Tile: %v", err)
	}
}

// bundled is an app bundle whose executable is the tile process minus AppKit:
// a script recording the arguments it was given and every line it was sent,
// so a test can read back exactly what a real tile would have been told.
func bundled(t *testing.T) (bundle, record string) {
	t.Helper()
	bundle = filepath.Join(t.TempDir(), "Ganymede.app")
	binary := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(binary, 0o755); err != nil {
		t.Fatalf("build a bundle: %v", err)
	}
	record = filepath.Join(bundle, "record")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + record + "\nwhile IFS= read -r line; do printf 'label=%s\\n' \"$line\" >> " + record + "\ndone\n"
	if err := os.WriteFile(filepath.Join(binary, "Ganymede"), []byte(script), 0o755); err != nil {
		t.Fatalf("write the bundle's executable: %v", err)
	}
	return bundle, record
}

// recorded is what the bundle's executable has written down by now. The
// process runs alongside the test, so this waits for what it is looking for
// rather than reading once and racing it.
func recorded(t *testing.T, record, want string) string {
	t.Helper()
	var body []byte
	settled(func() bool {
		body, _ = os.ReadFile(record)
		return strings.Contains(string(body), want)
	})
	return string(body)
}

// The Tile the launcher installed: the bundle's own executable, told it is
// the tile rather than the launcher, reading counts off its stdin.
func TestTheTileRunsTheBundlesExecutableAsATile(t *testing.T) {
	bundle, record := bundled(t)
	tl := tile.New(bundle)

	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	body := recorded(t, record, "label=2 0 0")
	if !strings.Contains(body, "label=2 0 0") || !strings.Contains(body, "--tile") {
		t.Errorf("the bundle's executable was run with %q, want --tile and the counts", body)
	}
}

// A harness whose launcher was never installed has no Tile at all, rather
// than one that fails on the first Session to block.
func TestNoAppBundleMeansNoTile(t *testing.T) {
	tl := tile.New(filepath.Join(t.TempDir(), "Ganymede.app"))

	if tl.Start != nil {
		t.Error("a Tile was built for a bundle that is not installed")
	}
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Errorf("a Tile with no bundle reported %v", err)
	}
}

// Closing is EOF to the process, which is how it knows to clear the badge and
// go: a count nobody is left to keep up to date must not stay on screen.
func TestClosingTheTileEndsItsProcess(t *testing.T) {
	bundle, record := bundled(t)
	tl := tile.New(bundle)
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}
	recorded(t, record, "label=1 0 0")

	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !settled(func() bool { return running(bundle) == 0 }) {
		t.Error("the tile process outlived the Dashboard that was telling it the count")
	}
}

// A real tile process killed outright, and a real replacement in its place:
// the bundle's executable is running again by the next count, and that count
// is on it.
func TestATileKilledOutrightComesBackOnTheNextCount(t *testing.T) {
	bundle, record := bundled(t)
	tl := tile.New(bundle)
	t.Cleanup(func() { _ = tl.Close() })
	if err := tl.Badge(tile.Counts{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}
	recorded(t, record, "label=1 0 0")

	killed(t, bundle)
	if !settled(func() bool { return running(bundle) == 0 }) {
		t.Fatal("the tile process outlived being killed")
	}

	if err := tl.Badge(tile.Counts{Blocked: 2}); err != nil {
		t.Fatalf("Badge after the tile was killed: %v", err)
	}

	body := recorded(t, record, "label=2 0 0")
	if !strings.Contains(body, "label=2 0 0") {
		t.Errorf("the replacement tile was sent %q, want the count the killed one could not take", body)
	}
	if running(bundle) != 1 {
		t.Errorf("%d of the bundle's processes are up, want the one that replaced the killed tile", running(bundle))
	}
}

// killed ends this bundle's tile process the way anything but a deliberate
// Quit does: outright, with no chance to say it is going.
func killed(t *testing.T, bundle string) {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", filepath.Join(bundle, "Contents", "MacOS", "Ganymede")).Output()
	if err != nil {
		t.Fatalf("find the tile process to kill: %v", err)
	}
	for _, pid := range strings.Fields(string(out)) {
		if err := exec.Command("kill", "-9", pid).Run(); err != nil {
			t.Fatalf("kill the tile process %s: %v", pid, err)
		}
	}
}

// running is how many of this bundle's processes are still alive.
func running(bundle string) int {
	out, _ := exec.Command("pgrep", "-f", filepath.Join(bundle, "Contents", "MacOS", "Ganymede")).Output()
	return len(strings.Fields(string(out)))
}

// settled waits for what a process does in its own time.
func settled(done func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
