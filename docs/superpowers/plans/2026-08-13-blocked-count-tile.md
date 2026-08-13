# Blocked-count Tile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the number of Blocked Sessions on Ganymede's own macOS Dock tile and menu bar, so the count is readable from any application without going back to the Ghostty window.

**Architecture:** The Dashboard already counts Attention every tick and writes it to the tmux attention strip. A second sink beside that one writes a label down a pipe to a small Swift process living inside `Ganymede.app`, which owns the Dock tile and a menu-bar item and decides nothing. The pipe is the lifeline: the Dashboard's exit or death closes it, the tile reads EOF and quits, so a badge can never outlive the Dashboard that vouched for it.

**Tech Stack:** Go 1.26.2 (stdlib only), tmux, AppKit through a single Swift file compiled by `swiftc` from the Command Line Tools, `make launcher`.

Design spec: `docs/superpowers/specs/2026-08-13-blocked-count-tile-design.md`. Read it before Task 1 — it carries the reasoning this plan only carries the shape of.

## Global Constraints

- **macOS only.** No Linux path, no build tags; the harness is already macOS-only.
- **No new Go dependencies.** `go.mod` must not gain a line.
- **Blocked only.** Ready never appears on either surface, in any count.
- **The label is decided in Go**, never in Swift: `tile.Label` is the only place that turns Attention into text.
- **The mark and the colour are `session.Blocked`'s own** — glyph `█`, colour `#f85149`. Never a second red.
- **`LSUIElement` stays `<true/>`** in `Info.plist`; the tile role flips to `.regular` at runtime.
- **Commit style:** free-form imperative subject, 72 chars max, body explaining why when it is not obvious, and the `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` trailer as a final `-m`. Use the `atomic-commits` skill.
- **Comment voice:** this codebase explains *why* in full sentences above the code, not what the line does. Match `internal/topology/strip.go` and `internal/dashboard/dashboard.go`. Every exported symbol gets a doc comment.
- **Run tests with** `go test ./...` from the repo root. Tests must pass before every commit.

## File Structure

| File | Responsibility |
|---|---|
| `internal/tile/tile.go` (create) | `Label` — Attention to badge text. `Tile` — the sink that spawns the tile process and writes labels to it, deduping and retiring. `New`/`Default` — where the bundle is. |
| `internal/tile/tile_test.go` (create) | All of the above, against a fake pipe and a stub app bundle. |
| `macos/launcher/Ganymede.swift` (create) | The bundle's executable, in two roles: today's launcher, and `--tile`. |
| `macos/launcher/Info.plist` (modify) | Gains `GanymedeRepo`, substituted at install time. |
| `macos/launcher/run.sh` (delete) | Replaced by the Swift launcher role. |
| `Makefile` (modify) | `launcher` compiles the Swift binary and substitutes the plist. |
| `internal/dashboard/dashboard.go` (modify) | The `Tile` interface, the `Harness` field, the reshaped `counted()`, the Ctrl+C close. |
| `internal/dashboard/dashboard_test.go` (modify) | The `tiles` fake and the Dashboard-side cases. |
| `cmd/ganymede/main.go` (modify) | Wires `tile.Default()` into the Dashboard's hands. |
| `CONTEXT.md`, `docs/ARCHITECTURE.md`, `README.md` (modify) | The vocabulary and the documented behaviour. |

Task order is bottom-up: the label, the sink, the bundle path, then the Swift app (verifiable by hand on its own), then the wiring that makes it live, then the docs. Nothing in the Dashboard changes until the app it talks to exists.

---

### Task 1: The label

**Files:**
- Create: `internal/tile/tile.go`
- Create: `internal/tile/tile_test.go`

**Interfaces:**
- Consumes: `session.Attention` (`internal/session/session.go:168`) — fields `Blocked`, `Ready`.
- Produces: `func Label(waiting session.Attention) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tile/tile_test.go`:

```go
package tile_test

import (
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/tile"
)

// Nothing Blocked is no badge at all. A tile reading 0 is a tile you stop
// looking at, and the count would lose the only thing it is for — the same
// call the strip makes when nothing is waiting on you.
func TestNothingBlockedIsNoLabel(t *testing.T) {
	if got := tile.Label(session.Attention{Ready: 3}); got != "" {
		t.Errorf("the Tile reads %q with nothing Blocked, want nothing", got)
	}
}

// The label is the Blocked count and nothing else: Ready has the rail and its
// own notification, and a number that moved for unread turns would stop
// meaning "something needs a decision".
func TestTheLabelIsTheBlockedCountAlone(t *testing.T) {
	for _, c := range []struct {
		waiting session.Attention
		want    string
	}{
		{session.Attention{Blocked: 1}, "1"},
		{session.Attention{Blocked: 2, Ready: 7}, "2"},
		{session.Attention{Blocked: 12}, "12"},
	} {
		if got := tile.Label(c.waiting); got != c.want {
			t.Errorf("%+v reads %q, want %q", c.waiting, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tile/`
Expected: FAIL — the package does not build, `undefined: tile.Label`.

- [ ] **Step 3: Write the implementation**

Create `internal/tile/tile.go`:

```go
// Package tile carries the Blocked count to Ganymede's own Dock tile — the
// harness's presence outside the emulator window, where a standing count can
// be read from whatever application you are actually in.
//
// Ghostty's own tile cannot be badged: macOS keeps a Dock tile private to the
// process that owns it, and Ghostty exposes no badge of its own. So the count
// goes on Ganymede.app, whose process this package spawns and then talks to
// down a pipe. Everything the tile shows is decided here; the app bundle's own
// process renders and decides nothing.
package tile

import (
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tile/`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tile/tile.go internal/tile/tile_test.go
git commit -m "Say what Ganymede's own tile reads" -m "The Blocked count alone, and nothing when nothing is Blocked: one number outside the Dashboard cannot say which tier it is about, so it is always the tier you have to act on." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The sink that writes labels down the pipe

**Files:**
- Modify: `internal/tile/tile.go`
- Modify: `internal/tile/tile_test.go`

**Interfaces:**
- Consumes: `tile.Label` from Task 1.
- Produces:
  - `type Tile struct { Start func() (io.WriteCloser, error) }` — the rest of its fields are unexported.
  - `func (t *Tile) Badge(waiting session.Attention) error`
  - `func (t *Tile) Close() error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tile/tile_test.go` (and add `"errors"`, `"io"`, `"strings"` to its imports):

```go
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
// many times it was asked for one.
func spawning(p *pipe, err error) (*tile.Tile, *int) {
	starts := 0
	return &tile.Tile{Start: func() (io.WriteCloser, error) {
		starts++
		return p, err
	}}, &starts
}

// The first Badge is what puts the Tile on screen, even with nothing Blocked:
// the harness being up is itself worth showing, and the icon appearing only
// once something blocked would leave nothing to click the rest of the time.
func TestTheFirstBadgeStartsTheTileWithNothingBlocked(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	if err := tl.Badge(session.Attention{}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
	if p.written.String() != "\n" {
		t.Errorf("the Tile was sent %q, want an empty label", p.written.String())
	}
}

// One line per change, so the tile process can read a whole label or nothing.
func TestBadgeSendsTheLabelAsALine(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)

	if err := tl.Badge(session.Attention{Blocked: 2}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if p.written.String() != "2\n" {
		t.Errorf("the Tile was sent %q, want %q", p.written.String(), "2\n")
	}
}

// The working set is rebuilt whenever anything at all moves, and almost none
// of it is about the Blocked count. A label that has not changed is not worth
// a write, and Ready moving is not the Tile's business at all.
func TestALabelThatHasNotMovedIsNotSentAgain(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	for _, waiting := range []session.Attention{
		{Blocked: 1},
		{Blocked: 1},
		{Blocked: 1, Ready: 4},
	} {
		if err := tl.Badge(waiting); err != nil {
			t.Fatalf("Badge: %v", err)
		}
	}

	if p.written.String() != "1\n" {
		t.Errorf("the Tile was sent %q, want the label once", p.written.String())
	}
	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// A pipe to a child process does not fail transiently: it fails because the
// process is gone, which is what quitting the tile from its own Dock menu
// does. That gesture is respected until the Dashboard is next started, rather
// than answered with a fresh tile by the next Session that blocks.
func TestAWriteThatFailedRetiresTheTile(t *testing.T) {
	p := &pipe{err: errors.New("write |1: broken pipe")}
	tl, starts := spawning(p, nil)

	if err := tl.Badge(session.Attention{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a pipe that is gone")
	}
	if err := tl.Badge(session.Attention{Blocked: 2}); err != nil {
		t.Errorf("a retired Tile complained again: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want the retired one left alone", *starts)
	}
}

// A tile that could not be started is retired the same way, so a Dashboard
// does not try to spawn a process it has already failed to spawn on every
// Session that blocks for the rest of the day.
func TestATileThatCouldNotBeStartedIsNotStartedAgain(t *testing.T) {
	tl, starts := spawning(&pipe{}, errors.New("fork/exec: no such file or directory"))

	if err := tl.Badge(session.Attention{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a tile that could not be started")
	}
	_ = tl.Badge(session.Attention{Blocked: 2})

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// A harness whose launcher was never installed has no Tile, which is not a
// failure and not worth an error: everything else about the Dashboard works
// exactly as it did.
func TestATileWithNoAppIsSilent(t *testing.T) {
	tl := &tile.Tile{}

	if err := tl.Badge(session.Attention{Blocked: 1}); err != nil {
		t.Errorf("a harness with no launcher installed reported %v", err)
	}
}

// Closing is what a Dashboard quit by hand does on the way out. EOF would
// take the tile down anyway once the process ends, but the pipe closing is
// what makes the icon go at the moment you quit.
func TestCloseClosesThePipe(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)
	_ = tl.Badge(session.Attention{Blocked: 1})

	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !p.closed {
		t.Error("Close left the pipe open")
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tile/`
Expected: FAIL — `undefined: tile.Tile`.

- [ ] **Step 3: Write the implementation**

Append to `internal/tile/tile.go`, and add `"fmt"` and `"io"` to its imports:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tile/`
Expected: PASS (10 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tile/tile.go internal/tile/tile_test.go
git commit -m "Carry the Blocked count down a pipe to the Tile" -m "One line per change, the first of them whatever the count is at the time, since the harness being up is worth showing on its own." -m "Any failure retires the sink. A pipe to a child does not fail transiently — it fails because the process is gone, which is what quitting the tile from its own Dock menu does, and a fresh tile spawned by the next blocked Session would be the harness arguing with you." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Where the app bundle is

**Files:**
- Modify: `internal/tile/tile.go`
- Modify: `internal/tile/tile_test.go`

**Interfaces:**
- Consumes: `Tile` from Task 2.
- Produces:
  - `func New(bundle string) *Tile` — a Tile driving `<bundle>/Contents/MacOS/Ganymede --tile`, with `Start` left nil when that executable is not there.
  - `func Default() *Tile` — `New("~/Applications/Ganymede.app")`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tile/tile_test.go` (adding `"os"`, `"path/filepath"`, `"time"` to its imports):

```go
// bundled is an app bundle whose executable is the tile process minus AppKit:
// a script recording the arguments it was given and every label it was sent,
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
	deadline := time.Now().Add(3 * time.Second)
	var body []byte
	for time.Now().Before(deadline) {
		body, _ = os.ReadFile(record)
		if strings.Contains(string(body), want) {
			return string(body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return string(body)
}

// The Tile the launcher installed: the bundle's own executable, told it is
// the tile rather than the launcher, reading labels off its stdin.
func TestTheTileRunsTheBundlesExecutableAsATile(t *testing.T) {
	bundle, record := bundled(t)
	tl := tile.New(bundle)

	if err := tl.Badge(session.Attention{Blocked: 2}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if body := recorded(t, record, "label=2"); !strings.Contains(body, "--tile") {
		t.Errorf("the bundle's executable was run with %q, want --tile", body)
	}
}

// A harness whose launcher was never installed has no Tile at all, rather
// than one that fails on the first Session to block.
func TestNoAppBundleMeansNoTile(t *testing.T) {
	tl := tile.New(filepath.Join(t.TempDir(), "Ganymede.app"))

	if tl.Start != nil {
		t.Error("a Tile was built for a bundle that is not installed")
	}
	if err := tl.Badge(session.Attention{Blocked: 1}); err != nil {
		t.Errorf("a Tile with no bundle reported %v", err)
	}
}

// Closing is EOF to the process, which is how it knows to clear the badge and
// go: a count nobody is left to keep up to date must not stay on screen.
func TestClosingTheTileEndsItsProcess(t *testing.T) {
	bundle, record := bundled(t)
	tl := tile.New(bundle)
	if err := tl.Badge(session.Attention{Blocked: 1}); err != nil {
		t.Fatalf("Badge: %v", err)
	}
	recorded(t, record, "label=1")

	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !settled(func() bool { return running(bundle) == 0 }) {
		t.Error("the tile process outlived the Dashboard that was telling it the count")
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
```

Add `"os/exec"` to the test imports as well.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tile/`
Expected: FAIL — `undefined: tile.New`.

- [ ] **Step 3: Write the implementation**

Append to `internal/tile/tile.go`, adding `"os"`, `"os/exec"`, `"path/filepath"` to its imports:

```go
// appName is the bundle the launcher installs, and executable is the binary
// inside it — the same one Spotlight runs, told by an argument that this time
// it is the tile rather than the launcher.
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tile/`
Expected: PASS (13 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tile/tile.go internal/tile/tile_test.go
git commit -m "Run the Tile out of the bundle the launcher installs" -m "A bundle that is not there leaves the Tile silent rather than failing later: the launcher is optional, so a harness installed without it should have no Tile at all rather than one reporting a missing app on the first Session that blocks." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: The app bundle's own process

**Files:**
- Create: `macos/launcher/Ganymede.swift`
- Modify: `macos/launcher/Info.plist`
- Delete: `macos/launcher/run.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: the pipe protocol Task 2 writes — one label per line, an empty line meaning nothing is Blocked, EOF meaning the Dashboard is gone.
- Produces: an executable that is today's launcher when run with no arguments, and the tile when run with `--tile`.

This task has no Go tests: there is nothing to assert on the Swift side but AppKit calls. Step 5's acceptance run stands in for them, and it is not optional.

- [ ] **Step 1: Write the bundle's executable**

Create `macos/launcher/Ganymede.swift`:

```swift
// Ganymede.app's executable, in two roles.
//
// With no arguments it is the launcher: Spotlight, a double-click or `open -a`
// runs it, it brings the harness up, and it exits. With --tile the Dashboard
// has spawned it as a child holding a pipe, and it is the Tile: the Dock tile
// and the menu-bar item carrying the Blocked count.
//
// The tile decides nothing. What the surfaces say arrives as one label per
// line on stdin, decided by internal/tile in Go where it can be tested; an
// empty line means nothing is Blocked. EOF means the Dashboard that was
// telling us has gone, and a count nobody is left to stand behind must not
// stay on screen — so the tile clears both surfaces and quits.
import AppKit

// repoKey is where `make launcher` writes this checkout's path, so the
// installed app always brings up the harness from the checkout it was built
// from.
let repoKey = "GanymedeRepo"

// blockedRed is session.Blocked.Colour() — the rail's own red for the one
// state this counts. The Dock badge keeps the system's red; this is for the
// menu bar, which has no badge of its own.
let blockedRed = NSColor(srgbRed: 0xF8 / 255, green: 0x51 / 255, blue: 0x49 / 255, alpha: 1)

// blockedGlyph and idleGlyph are session.Blocked.Glyph() and the app's own
// mark: the count reads as the same fact the rail and the strip show, and the
// menu bar still says the harness is up when nothing is waiting on you.
let blockedGlyph = "█"
let idleGlyph = "◑"

let ghostty = URL(fileURLWithPath: "/Applications/Ghostty.app")

// forward brings Ghostty to the front — what clicking either surface is for.
// The Dashboard is a keystroke away once you are in the window, so neither
// surface tries to jump to a particular Session.
func forward() {
    NSWorkspace.shared.openApplication(at: ghostty, configuration: NSWorkspace.OpenConfiguration())
}

// launch is the launcher role: bring the harness up from the checkout this
// bundle was installed from, and exit when it has.
//
// The explicit PATH matters here in a way it would not in a terminal: a
// process launched by LaunchServices inherits the login session's environment
// rather than an interactive shell's, and `up` shells out to tmux and looks
// terminal-notifier up by bare name — both Homebrew-installed.
func launch() -> Never {
    guard let repo = Bundle.main.infoDictionary?[repoKey] as? String, !repo.hasPrefix("@@") else {
        FileHandle.standardError.write("Ganymede.app does not know where its checkout is: run make launcher again\n".data(using: .utf8)!)
        exit(1)
    }
    let process = Process()
    process.executableURL = URL(fileURLWithPath: repo + "/bin/ganymede")
    process.arguments = ["up", repo]
    var environment = ProcessInfo.processInfo.environment
    environment["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (environment["PATH"] ?? "/usr/bin:/bin")
    process.environment = environment
    do {
        try process.run()
    } catch {
        FileHandle.standardError.write("Ganymede could not be brought up: \(error)\n".data(using: .utf8)!)
        exit(1)
    }
    process.waitUntilExit()
    exit(process.terminationStatus)
}

final class TileDelegate: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem?

    func applicationDidFinishLaunching(_ notification: Notification) {
        // The plist keeps LSUIElement, so the launcher role never flashes an
        // icon. Only the tile joins the Dock, and only for as long as the
        // Dashboard it belongs to is up.
        NSApp.setActivationPolicy(.regular)

        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.target = self
        item.button?.action = #selector(clicked)
        self.item = item
        show("")

        // stdin is read off the main thread: the runloop has to stay free to
        // draw the surfaces this is about to change.
        DispatchQueue.global().async {
            while let line = readLine(strippingNewline: true) {
                DispatchQueue.main.async { self.show(line) }
            }
            DispatchQueue.main.async {
                NSApp.dockTile.badgeLabel = nil
                NSApp.terminate(nil)
            }
        }
    }

    // show puts one label on both surfaces. An empty label is a quiet working
    // set: no badge at all, and the menu bar back to saying only that the
    // harness is up.
    private func show(_ label: String) {
        NSApp.dockTile.badgeLabel = label.isEmpty ? nil : label
        let title: NSAttributedString
        if label.isEmpty {
            title = NSAttributedString(string: idleGlyph, attributes: [
                .font: NSFont.menuBarFont(ofSize: 0),
            ])
        } else {
            title = NSAttributedString(string: blockedGlyph + " " + label, attributes: [
                .font: NSFont.menuBarFont(ofSize: 0),
                .foregroundColor: blockedRed,
            ])
        }
        item?.button?.attributedTitle = title
    }

    @objc private func clicked() {
        forward()
    }

    // Clicking the Dock icon of an application with no windows: hand the
    // click on to the window the harness actually lives in.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        forward()
        return false
    }
}

if CommandLine.arguments.contains("--tile") {
    let delegate = TileDelegate()
    NSApplication.shared.delegate = delegate
    NSApplication.shared.run()
} else {
    launch()
}
```

- [ ] **Step 2: Give the plist the checkout's path**

In `macos/launcher/Info.plist`, add the `GanymedeRepo` key immediately after `CFBundleExecutable`, leaving every existing key — `LSUIElement` included — exactly as it is:

```xml
	<key>GanymedeRepo</key>
	<string>@@REPO_DIR@@</string>
```

Then delete `macos/launcher/run.sh`: the Swift launcher role replaces it, and the `@@REPO_DIR@@` substitution it carried now happens in the plist.

```bash
git rm macos/launcher/run.sh
```

- [ ] **Step 3: Build it from the Makefile**

Replace the body of the `launcher` target in `Makefile` with this, leaving `LAUNCHER_APP`, `LSREGISTER` and the `.PHONY` line as they are — and update the comment above the target to say the bundle now holds a compiled binary:

```makefile
# launcher materializes a minimal .app bundle in ~/Applications so Spotlight
# can find and launch Ganymede directly, without a terminal — and so the
# Dashboard has a Dock tile to put the Blocked count on. Re-run after moving
# this checkout: the bundle bakes in its absolute path.
launcher: build
	mkdir -p "$(LAUNCHER_APP)/Contents/MacOS" "$(LAUNCHER_APP)/Contents/Resources"
	sed "s|@@REPO_DIR@@|$(CURDIR)|g" macos/launcher/Info.plist > "$(LAUNCHER_APP)/Contents/Info.plist"
	swiftc -O macos/launcher/Ganymede.swift -o "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	cp macos/launcher/Ganymede.icns "$(LAUNCHER_APP)/Contents/Resources/Ganymede.icns"
	$(LSREGISTER) -f "$(LAUNCHER_APP)"
	@echo "Installed $(LAUNCHER_APP) — search Spotlight for Ganymede."
```

- [ ] **Step 4: Build the bundle**

Run: `make launcher`
Expected: no compiler diagnostics, and `file ~/Applications/Ganymede.app/Contents/MacOS/Ganymede` reports a Mach-O executable. Confirm the substitution landed: `plutil -p ~/Applications/Ganymede.app/Contents/Info.plist | grep GanymedeRepo` must print this checkout's absolute path, not `@@REPO_DIR@@`.

- [ ] **Step 5: Drive the tile by hand**

This is the acceptance test for the whole task. Run it and check every line before committing:

```bash
{ printf '2\n'; sleep 20; } | ~/Applications/Ganymede.app/Contents/MacOS/Ganymede --tile
```

While it runs:
- The menu bar reads `█ 2`, the glyph in the rail's red.
- Revealing the Dock (this machine's Dock auto-hides — pointer to the bottom edge) shows the moon icon with a `2` badge on it.
- Clicking the menu-bar item brings Ghostty forward. Clicking the Dock icon does too.

Then let the `sleep` finish: both surfaces disappear as the process reads EOF and quits. Confirm nothing is left behind with `pgrep -f 'Ganymede --tile'`, which must print nothing.

Finally, the launcher role, unchanged from today: `open ~/Applications/Ganymede.app` brings the harness up (or refocuses it) with **no** Dock icon and no menu-bar item of its own.

- [ ] **Step 6: Commit**

```bash
git add macos/launcher/Ganymede.swift macos/launcher/Info.plist Makefile
git commit -m "Give Ganymede.app a tile to carry the count on" -m "The bundle's executable becomes a compiled binary with two roles: no arguments is today's launcher, --tile is the Dock tile and menu-bar item the Dashboard spawns as a child. LSUIElement stays on, so only the tile role ever joins the Dock, and only while the Dashboard is up." -m "It decides nothing: one label per line on stdin, and EOF — the Dashboard gone — clears both surfaces and quits, so a count nobody stands behind cannot stay on screen." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: The Dashboard's second sink

**Files:**
- Modify: `internal/dashboard/dashboard.go` — the `Tile` interface next to `Strip` (around line 135), the `Harness` field (around line 204), `counted()` (line 878), the Ctrl+C branch (line 1051)
- Modify: `internal/dashboard/dashboard_test.go`
- Modify: `cmd/ganymede/main.go:222`

**Interfaces:**
- Consumes: `tile.Default()` and `*tile.Tile`'s `Badge`/`Close` from Tasks 2 and 3.
- Produces: `dashboard.Tile` interface — `Badge(waiting session.Attention) error`, `Close() error` — and `dashboard.Harness.Tile`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/dashboard/dashboard_test.go`:

```go
// tiles records what the Dashboard put on Ganymede's own Dock tile, standing
// in for the app bundle's process.
type tiles struct {
	badged []session.Attention
	closed bool
	err    error
}

func (t *tiles) Badge(waiting session.Attention) error {
	t.badged = append(t.badged, waiting)
	return t.err
}

func (t *tiles) Close() error {
	t.closed = true
	return nil
}

// badging runs one working set after another past a Dashboard wired to both
// sinks, the way a live one is.
func badging(strip dashboard.Strip, tile dashboard.Tile, sets ...[]session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Strip: strip, Tile: tile})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	for _, set := range sets {
		model, _ = model.Update(dashboard.Sessions(set))
	}
	return model
}

// One working set, counted once: the Tile is told the same Attention the strip
// is, in the same update, so the two surfaces cannot disagree about how many
// Sessions are waiting on a decision.
func TestTheTileIsToldTheSameCountsAsTheStrip(t *testing.T) {
	strip, tile := &strips{}, &tiles{}

	badging(strip, tile, []session.Session{
		live("aaa-blocked", "/repos/service-billing", session.Blocked),
		live("bbb-blocked", "/repos/ganymede", session.Blocked),
		live("ccc-ready", "/repos/ganymede", session.Ready),
	})

	if len(tile.badged) == 0 {
		t.Fatal("the Dashboard never told the Tile anything")
	}
	if last, want := tile.badged[len(tile.badged)-1], (session.Attention{Blocked: 2, Ready: 1}); last != want {
		t.Errorf("the Tile was told %+v, want %+v", last, want)
	}
	if last := strip.shown[len(strip.shown)-1]; last != tile.badged[len(tile.badged)-1] {
		t.Errorf("the strip reads %+v and the Tile %+v, want one count", last, tile.badged[len(tile.badged)-1])
	}
}

// Counts that have not moved reach neither sink. The working set is rebuilt
// whenever anything at all changes, and neither surface is worth a write for
// news it already has.
func TestNeitherSinkHearsCountsThatHaveNotChanged(t *testing.T) {
	strip, tile := &strips{}, &tiles{}
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
	elsewhere := live("ganymede-78", "/repos/ganymede", session.Idle)
	working := elsewhere
	working.State = session.Working

	badging(strip, tile,
		[]session.Session{blocked, elsewhere},
		[]session.Session{blocked, working},
		[]session.Session{blocked, elsewhere},
	)

	if len(tile.badged) != 1 {
		t.Errorf("the Dashboard told the Tile %d times for one set of counts: %+v", len(tile.badged), tile.badged)
	}
	if len(strip.shown) != 1 {
		t.Errorf("the Dashboard wrote the strip %d times for one set of counts: %+v", len(strip.shown), strip.shown)
	}
}

// A harness whose launcher was never installed has no Tile at all, and the
// Dashboard is exactly the Dashboard it was without one.
func TestADashboardWithNoTileIsUnchanged(t *testing.T) {
	strip := &strips{}

	model := badging(strip, nil, []session.Session{live("ganymede-78", "/repos/ganymede", session.Blocked)})

	if last, want := strip.shown[len(strip.shown)-1], (session.Attention{Blocked: 1}); last != want {
		t.Errorf("the strip reads %+v, want %+v", last, want)
	}
	if view := drawn(model); len(sessionRows(view)) != 1 {
		t.Errorf("a harness with no Tile lost the rail its tree:\n%s", view)
	}
}

// The Tile is the third place the same count is shown, so a Tile that cannot
// be reached is not worth a word: the rail and the strip still say all of it,
// and the Tile retires itself.
func TestATileThatCannotBeReachedLeavesTheRestAlone(t *testing.T) {
	strip := &strips{}
	tile := &tiles{err: errors.New("write |1: broken pipe")}

	model := badging(strip, tile, []session.Session{live("ganymede-78", "/repos/ganymede", session.Blocked)})

	if last, want := strip.shown[len(strip.shown)-1], (session.Attention{Blocked: 1}); last != want {
		t.Errorf("a Tile that could not be reached cost the strip its count: %+v", last)
	}
	if view := drawn(model); len(sessionRows(view)) != 1 || strings.Contains(view, "broken pipe") {
		t.Errorf("a Tile that could not be reached reached the rail:\n%s", view)
	}
}

// A Dashboard that has gone takes its Tile with it, the same way it takes the
// strip: a badge nobody is left to keep up to date is a badge that will be
// wrong by morning.
func TestAClosedDashboardTakesItsTileWithIt(t *testing.T) {
	tile := &tiles{}
	model := badging(&strips{}, tile, []session.Session{live("ganymede-78", "/repos/ganymede", session.Blocked)})

	press(model, tea.KeyCtrlC)

	if !tile.closed {
		t.Error("a closed Dashboard left its Tile running")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/`
Expected: FAIL — `unknown field Tile in struct literal of type dashboard.Harness`.

- [ ] **Step 3: Add the interface and the field**

In `internal/dashboard/dashboard.go`, directly below the `Strip` interface (line 135-137):

```go
// Tile is Ganymede's own Dock tile and menu-bar item — the same counts again,
// in the one place they can be read from another application entirely. The
// strip is inside the window you are working in; this outlives you leaving it.
type Tile interface {
	// Badge shows what is waiting on you. Errors are the Tile's own to keep:
	// it retires itself, and the Dashboard has nowhere to put a complaint.
	Badge(waiting session.Attention) error
	// Close takes the Tile down with the Dashboard.
	Close() error
}
```

And in the `Harness` struct, immediately after the `Strip` field (line 203-204):

```go
	// Tile carries the Blocked count to Ganymede's own Dock tile. Nil is a
	// harness whose launcher was never installed, which has no Tile.
	Tile Tile
```

- [ ] **Step 4: Reshape `counted()`**

Replace the body of `counted()` (line 878-891) with this, and extend its doc comment's first line to `counted carries the working set's Attention out to the strip and the Tile.`:

```go
func (m Model) counted() Model {
	// Whether the counts have moved is the question both sinks share, and it
	// comes first: the strip's own nil check used to stand in for it, which
	// would have left a Tile told the same thing on every registry event.
	if m.shown && m.waiting == m.written {
		return m
	}
	if m.harness.Tile != nil {
		// A third copy of the same count, and the only one that fails without
		// costing you anything: the Tile retires itself, and there is nowhere
		// here that could report this without corrupting the rail.
		_ = m.harness.Tile.Badge(m.waiting)
	}
	// The strip is deliberate redundancy: everything it says is on the rail
	// already, so one that could not be written is not worth a word about. It
	// is worth trying again, though, which is why only a write that landed
	// counts as having been said.
	if m.harness.Strip != nil {
		if err := m.harness.Strip.Show(m.waiting); err != nil {
			return m
		}
	}
	m.written, m.shown = m.waiting, true
	return m
}
```

- [ ] **Step 5: Close the Tile on the way out**

In the `tea.KeyCtrlC` branch (line 1051), after the strip is blanked:

```go
		if m.harness.Tile != nil {
			_ = m.harness.Tile.Close()
		}
```

Extend the comment above it so it covers both: the strip is blanked and the Tile closed, since a count nobody is left to keep up to date will be wrong by morning.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/`
Expected: PASS — the five new tests, and every existing strip test unchanged. `TestCountsThatCouldNotBeWrittenAreTriedAgain` and `TestTheStripIsLeftAloneWhenTheCountsHaveNotChanged` are the two the reshaped gate could have broken; if either fails, the gate is wrong, not the test.

- [ ] **Step 7: Wire it into the running Dashboard**

In `cmd/ganymede/main.go`, add `tile.Default()` to the Dashboard's hands (line 222) and `"github.com/BrechtBonte/ganymede/internal/tile"` to the imports:

```go
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Focuser: harness, Strip: harness, Tile: tile.Default(), Spawner: harness, Popups: harness, Approver: harness,
		Prompter: harness, Stopper: harness, Seen: model.Seen, Tickets: tickets, Panes: harness,
	}
```

Extend the comment above `hands` to say the Tile is the one hand that reaches outside the emulator window.

- [ ] **Step 8: Run the whole suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: See it live**

Run `make refresh`, then get a Session to a permission prompt. The menu bar must read `█ 1` and the revealed Dock must show the badge, both without touching the Dashboard. Answer the prompt: both clear. Quit the Dashboard with Ctrl+C in its pane: the icon and the item go together.

- [ ] **Step 10: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go cmd/ganymede/main.go
git commit -m "Put the Blocked count on Ganymede's own tile" -m "The Dashboard already counts Attention for the strip; the Tile is the same count sent to a second sink, and the only one that survives you leaving the window. The gate on whether the counts moved now comes before both sinks — fused to the strip's nil check, it would have told the Tile the same thing on every registry event." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: The vocabulary and the docs

**Files:**
- Modify: `CONTEXT.md` — the *Structure* section, after **Popup shell**
- Modify: `docs/ARCHITECTURE.md` — the Notifications section, the data-flow diagram, *What the harness writes*
- Modify: `README.md` — Prerequisites, and the section on where attention reaches you

**Interfaces:**
- Consumes: everything Tasks 1-5 built.
- Produces: nothing code depends on.

- [ ] **Step 1: Name it in CONTEXT.md**

Add to the *Structure* section, keeping the entry shape every other term uses (bold term, colon, definition, `_Avoid_:` line):

```markdown
**Tile**:
Ganymede's own macOS Dock tile and the menu-bar item beside it, carrying the number of Blocked sessions — the harness's presence outside the emulator window. Lives exactly as long as the dashboard.
_Avoid_: dock icon, dock badge, app badge — the Dock is the tmux frame, not macOS's
```

- [ ] **Step 2: Document it in ARCHITECTURE.md**

In the **Notifications** section, after the *Missed pings* bullet, add:

```markdown
- **The Tile.** Ganymede.app's Dock tile and menu-bar item carry the standing Blocked count — `█ 2` in the menu bar, a `2` badge on the icon — which is what a notification cannot be: dismissing a banner leaves nothing outside Ghostty saying two sessions still need a decision. It is not focus-gated, unlike every banner: a badge interrupts nothing, and one that blanked itself whenever you were looking could never be trusted as a summary of where things stand. Ready is deliberately absent, since one number outside the dashboard cannot say which tier it is about.
- **Whose tile.** Not Ghostty's — macOS keeps a Dock tile private to the process that owns it, and Ghostty offers no badge of its own (its bell's `attention` bounces the icon once and counts nothing). The dashboard spawns `Ganymede.app`'s executable with `--tile` as a child holding a pipe, writes one label per change, and the child's identity, icon and name come from the bundle its executable sits in. The pipe is the lifeline: the dashboard's exit or death is EOF, at which point the tile clears both surfaces and quits, so a badge cannot outlive the dashboard that vouched for it. Quitting the tile from its own Dock menu retires the sink until the dashboard is next started.
```

In the data-flow diagram, add the Tile beside the notifier — a node inside the `ganymede` subgraph and an edge from the state model's consumer:

```
        TILE["Tile<br/>Dock badge + menu bar"]
```
```
    UI --> TILE
```

In *What the harness writes*, extend the sentence about the launcher bundle to say that `~/Applications/Ganymede.app` now holds a compiled binary and an `Info.plist` naming this checkout, both written by `make launcher`.

- [ ] **Step 3: Note the prerequisite in README.md**

Add a row to the Prerequisites table, after the Go toolchain row:

```markdown
| Xcode Command Line Tools | `make launcher` compiles the Dock tile app with `swiftc` | `xcode-select --install` |
```

And in the section that explains notifications, add a sentence: the Blocked count also sits on Ganymede's own Dock icon and in the menu bar for as long as the dashboard is up, so a dismissed banner does not take the count with it.

- [ ] **Step 4: Check the docs against the build**

Run: `grep -rn "run.sh" README.md docs/ CONTEXT.md Makefile`
Expected: no hits. The file is gone, and nothing may still point at it.

- [ ] **Step 5: Commit**

```bash
git add CONTEXT.md docs/ARCHITECTURE.md README.md
git commit -m "Record the Tile and what it is not" -m "Dock is already the tmux frame in this project's language, so the macOS surface needs its own word or the two blur in the one place the vocabulary is normative." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Done when

- `go test ./...` passes.
- A Session going Blocked puts `█ 1` in the menu bar and a badge on the revealed Dock icon, without the Dashboard being touched; answering it clears both.
- Quitting the Dashboard takes both surfaces with it; `make refresh` brings them back still counting.
- `open ~/Applications/Ganymede.app` behaves exactly as it does today.
- CONTEXT.md defines Tile, and nothing anywhere still mentions `run.sh`.
