// Package tile carries the Blocked count to Ganymede's own Dock tile — the
// harness's presence outside the emulator window, where a standing count can
// be read from whatever application you are actually in.
//
// Ghostty's own tile cannot be badged: macOS keeps a Dock tile private to the
// process that owns it, and Ghostty offers no badge of its own. So the count
// goes on Ganymede.app, whose process this package spawns and then talks to
// down a pipe. Everything the tile shows is decided here; the app bundle's own
// process renders and decides nothing.
package tile

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// Label is what the Tile shows: how many Sessions cannot continue without your
// decision, and nothing at all when none of them can.
//
// Ready is deliberately absent. It already has the rail and its own delayed,
// silent notification, and a single number outside the Dashboard cannot say
// which tier it is about — so this one is always about the tier you have to
// act on.
func Label(waiting session.Attention) string {
	if waiting.Blocked == 0 {
		return ""
	}
	return strconv.Itoa(waiting.Blocked)
}

// Tile is Ganymede's own Dock tile and menu-bar item, driven down a pipe to
// the app bundle's process.
//
// It is the second sink for the same Attention the strip carries, and the only
// one that survives you leaving the window: the strip is inside Ghostty, this
// is beside every other application's icon.
type Tile struct {
	// Start launches the tile process and hands back the pipe its labels are
	// written to. Nil is a harness whose launcher was never installed, which
	// is not a failure — it simply has no Tile.
	Start func() (io.WriteCloser, error)

	pipe    io.WriteCloser
	label   string
	started bool
	retired bool
}

// Badge shows what is waiting on you.
//
// The first call is what puts the tile on screen, with whatever the count is
// at the time — the harness being up is worth showing on its own, and an icon
// that appeared only once something blocked would leave nothing to click for
// the rest of the day. After that, only a label that has actually moved is
// worth a write: the working set is rebuilt whenever anything at all changes,
// and almost none of it is about the Blocked count.
//
// Any failure retires the Tile for good. A pipe to a child process does not
// fail transiently — it fails because the process is gone, which is what
// quitting the tile from its own Dock menu does, and answering that gesture
// with a fresh tile on the next Session that blocks would be the harness
// arguing with you.
func (t *Tile) Badge(waiting session.Attention) error {
	if t.Start == nil || t.retired {
		return nil
	}
	label := Label(waiting)
	if t.started && label == t.label {
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
	if _, err := fmt.Fprintln(t.pipe, label); err != nil {
		t.retired = true
		return fmt.Errorf("tell Ganymede's Dock tile about %q: %w", label, err)
	}
	t.label = label
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
