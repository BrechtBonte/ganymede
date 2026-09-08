// Package tile carries the Blocked/Ready/Working counts to Ganymede's own
// Dock tile — the harness's presence outside the emulator window, where a
// standing count can be read from whatever application you are actually in.
//
// Ghostty's own tile cannot be badged: macOS keeps a Dock tile private to the
// process that owns it, and Ghostty offers no badge of its own. So the count
// goes on Ganymede.app, whose process this package spawns and then talks to
// down a pipe, one line of three counts per change. Everything the tile shows
// is decided here; the app bundle's own process renders and decides nothing.
package tile

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// Counts is what the Tile shows, in full: every tier the working set is in,
// not only the one the Dock badge counts. The dropdown reads all three; the
// Dock badge and the menu-bar title still read Blocked alone.
type Counts struct {
	Blocked int
	Ready   int
	Working int
}

// CountsIn counts a working set by tier, the way session.AttentionIn counts
// Blocked and Ready — with Working alongside them, because the dropdown is
// the one surface that has to say what is not waiting on you as well as what
// is.
func CountsIn(sessions []session.Session) Counts {
	var counted Counts
	for _, s := range sessions {
		switch s.State {
		case session.Blocked:
			counted.Blocked++
		case session.Ready:
			counted.Ready++
		case session.Working:
			counted.Working++
		}
	}
	return counted
}

// Tile is Ganymede's own Dock tile and menu-bar item, driven down a pipe to
// the app bundle's process.
//
// It is the second sink for the same working set the strip counts, and the
// only one that survives you leaving the window: the strip is inside
// Ghostty, this is beside every other application's icon.
type Tile struct {
	// Start launches the tile process and hands back the pipe its counts are
	// written to, alongside the report of how that run of it ends. Nil is a
	// harness whose launcher was never installed, which is not a failure — it
	// simply has no Tile.
	Start func() (io.WriteCloser, Ended, error)

	pipe    io.WriteCloser
	ended   Ended
	counts  Counts
	started bool
	retired bool
}

// Ended is how one run of the tile process finished, once it has: nil for a
// tile that quit itself — what Quit on its Dock menu does — and an error for
// one that was killed, crashed, or went down with something larger than
// itself. Exactly one report is ever sent.
type Ended <-chan error

// Badge shows the working set's Blocked, Ready and Working counts.
//
// The first call is what puts the tile on screen, with whatever the counts
// are at the time — the harness being up is worth showing on its own, and an
// icon that appeared only once something blocked would leave nothing to
// click for the rest of the day. After that, only counts that have actually
// moved are worth a write: the working set is rebuilt whenever anything at
// all changes.
//
// A write fails because the process behind it is gone, and how it went is
// what decides whether it comes back. Quit on its own Dock menu is a gesture
// to respect: the Tile retires, rather than arguing with you by reappearing
// on the next Session that blocks. Anything else — killed, crashed, or taken
// down with something larger than itself — was never asked for, and a
// Dashboard that stays up for weeks cannot spend the rest of them with no
// presence outside the emulator window, so the next working set brings the
// tile back and lands the standing count on it. The next working set, not the
// next count that differs: counts stand still for hours, and a tile lost
// during one of those would stay lost for the whole of it.
//
// A tile that cannot be started, or a replacement that will not take the
// count either, retires the Tile for good: whatever is wrong is not something
// spawning another process every time a Session blocks will fix.
func (t *Tile) Badge(counts Counts) error {
	if t.Start == nil {
		return nil
	}
	t.reconcile()
	if t.retired {
		return nil
	}
	if t.started && counts == t.counts {
		return nil
	}
	// Two passes at most: the first can fail on a tile that has gone since the
	// last count landed, and the second is the one it is brought back as.
	var failed error
	for attempt := 0; attempt < 2; attempt++ {
		if !t.started {
			pipe, ended, err := t.Start()
			if err != nil {
				t.retired = true
				return fmt.Errorf("start Ganymede's Dock tile: %w", err)
			}
			t.pipe, t.ended, t.started = pipe, ended, true
		}
		if _, failed = fmt.Fprintf(t.pipe, "%d %d %d\n", counts.Blocked, counts.Ready, counts.Working); failed == nil {
			t.counts = counts
			return nil
		}
		if t.quit() {
			t.retired = true
			return fmt.Errorf("Ganymede's Dock tile was quit, so %+v went unsaid: %w", counts, failed)
		}
		t.started = false
	}
	t.retired = true
	return fmt.Errorf("tell Ganymede's Dock tile about %+v: %w", counts, failed)
}

// reconcile takes account of a run that ended since the last count, before
// anything is decided about this one.
//
// Counts stand still for hours at a time, and nothing is written while they
// do — so a tile lost during one of them would go unnoticed for exactly as
// long, which is the whole of what this is meant to fix. Asking the run
// itself, rather than waiting for a write to fail, is what makes the next
// working set enough to bring the tile back.
func (t *Tile) reconcile() {
	if !t.started {
		return
	}
	select {
	case err := <-t.ended:
		if err == nil {
			t.retired = true
			return
		}
		t.started = false
	default:
	}
}

// lostGrace is how long a failed write waits to hear how the process behind it
// went. exec.Cmd.Wait closes the pipe just before it reports, so the report is
// always a moment behind the write that noticed — a moment worth waiting out,
// since it is the whole difference between a tile you quit and one you lost.
const lostGrace = 100 * time.Millisecond

// quit reports whether the process behind a failed write went on its own
// terms: NSApp.terminate exits cleanly, which is what Quit on the Dock menu
// does, where a tile that was killed or crashed reports the signal that ended
// it. A run that says nothing in time is taken as lost, because bringing back
// a tile you had quit is the smaller wrong of the two.
func (t *Tile) quit() bool {
	if t.ended == nil {
		return false
	}
	select {
	case err := <-t.ended:
		return err == nil
	case <-time.After(lostGrace):
		return false
	}
}

// appName is the bundle the launcher installs, executable is the binary inside
// it — the same one Spotlight runs, told by tileArg that this time it is the
// tile rather than the launcher.
const (
	appName    = "Ganymede.app"
	executable = "Contents/MacOS/Ganymede"
	tileArg    = "--tile"
)

// Default is the Tile in the bundle `make launcher` installs.
func Default() *Tile {
	home, err := os.UserHomeDir()
	if err != nil {
		// Nothing to badge and nothing worth saying: the Dashboard has no way
		// to tell you about this that would not corrupt the rail it draws.
		return &Tile{}
	}
	return New(filepath.Join(home, "Applications", appName))
}

// New is the Tile in bundle.
//
// A bundle that is not there leaves Start nil rather than failing later: the
// launcher is optional (`make launcher`), and a harness installed without it
// should be a harness with no Tile, not one reporting a missing app on the
// first Session that blocks.
func New(bundle string) *Tile {
	binary := filepath.Join(bundle, executable)
	if _, err := os.Stat(binary); err != nil {
		return &Tile{}
	}
	return &Tile{Start: func() (io.WriteCloser, Ended, error) {
		command := exec.Command(binary, tileArg)
		pipe, err := command.StdinPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := command.Start(); err != nil {
			return nil, nil, err
		}
		// The tile outlives this call and ends on its own once the pipe
		// closes, so nothing here waits for it — but something has to, or it
		// stays a zombie on the Dashboard's own process for as long as the
		// harness is up. What that wait answers is also the one thing that
		// tells a tile you quit from one that was taken from you.
		ended := make(chan error, 1)
		go func() { ended <- command.Wait() }()
		return pipe, ended, nil
	}}
}

// Close takes the tile down with the Dashboard. The process would read EOF on
// its own once this one ends — and has to, since a Dashboard killed outright
// runs no cleanup at all — but closing the pipe deliberately is what makes the
// icon go at the moment you quit rather than a beat afterwards.
func (t *Tile) Close() error {
	// Retiring here is what keeps a tile from being spawned behind a Dashboard
	// that is already leaving: closing is EOF, and every write after it fails.
	t.retired = true
	if t.pipe == nil {
		return nil
	}
	pipe := t.pipe
	t.pipe = nil
	return pipe.Close()
}
