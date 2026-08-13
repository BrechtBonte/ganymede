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
	// written to. Nil is a harness whose launcher was never installed, which
	// is not a failure — it simply has no Tile.
	Start func() (io.WriteCloser, error)

	pipe    io.WriteCloser
	counts  Counts
	started bool
	retired bool
}

// Badge shows the working set's Blocked, Ready and Working counts.
//
// The first call is what puts the tile on screen, with whatever the counts
// are at the time — the harness being up is worth showing on its own, and an
// icon that appeared only once something blocked would leave nothing to
// click for the rest of the day. After that, only counts that have actually
// moved are worth a write: the working set is rebuilt whenever anything at
// all changes.
//
// Any failure retires the Tile for good. A pipe to a child process does not
// fail transiently — it fails because the process is gone, which is what
// quitting the tile from its own Dock menu does, and answering that gesture
// with a fresh tile on the next Session that blocks would be the harness
// arguing with you.
func (t *Tile) Badge(counts Counts) error {
	if t.Start == nil || t.retired {
		return nil
	}
	if t.started && counts == t.counts {
		return nil
	}
	if !t.started {
		pipe, err := t.Start()
		if err != nil {
			t.retired = true
			return fmt.Errorf("start Ganymede's Dock tile: %w", err)
		}
		t.pipe, t.started = pipe, true
	}
	if _, err := fmt.Fprintf(t.pipe, "%d %d %d\n", counts.Blocked, counts.Ready, counts.Working); err != nil {
		t.retired = true
		return fmt.Errorf("tell Ganymede's Dock tile about %+v: %w", counts, err)
	}
	t.counts = counts
	return nil
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
	return &Tile{Start: func() (io.WriteCloser, error) {
		command := exec.Command(binary, tileArg)
		pipe, err := command.StdinPipe()
		if err != nil {
			return nil, err
		}
		if err := command.Start(); err != nil {
			return nil, err
		}
		// The tile outlives this call and ends on its own once the pipe
		// closes, so nothing here waits for it — but something has to, or it
		// stays a zombie on the Dashboard's own process for as long as the
		// harness is up.
		go func() { _ = command.Wait() }()
		return pipe, nil
	}}
}

// Close takes the tile down with the Dashboard. The process would read EOF on
// its own once this one ends — and has to, since a Dashboard killed outright
// runs no cleanup at all — but closing the pipe deliberately is what makes the
// icon go at the moment you quit rather than a beat afterwards.
func (t *Tile) Close() error {
	if t.pipe == nil {
		return nil
	}
	pipe := t.pipe
	t.pipe = nil
	return pipe.Close()
}
