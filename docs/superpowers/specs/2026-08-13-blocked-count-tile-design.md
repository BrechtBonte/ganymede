# Ganymede's own Dock tile carries the Blocked count

## Problem

Attention beyond the Dashboard is notifications and nothing else, and a
notification is a one-shot: dismiss the Blocked banner and nothing outside
Ghostty still says that two Sessions cannot continue without your decision.
The attention strip says it, but only inside the window you have just left.
What is missing is a *standing* count — the number of Blocked Sessions,
readable from wherever you actually are, without going back to look.

Ghostty cannot carry it. Its 1.3.1 configuration surface has no badge key and
its action list no badge action, there is no macOS IPC to drive one, and macOS
keeps a Dock tile private to the process that owns it — no application can
badge another's. Ghostty's only hook for the outside world is the bell
(`bell-features = attention`), which bounces its icon once while unfocused and
counts nothing.

So the count goes on a tile of the harness's own. `Ganymede.app` already
exists for Spotlight (see `docs/designs/2026-08-12-spotlight-launcher.md`) and
does nothing after the moment it launches; this gives it the rest of the day's
work.

## Vocabulary

**Tile** — the macOS Dock tile of `Ganymede.app`, and the menu-bar item beside
it: the harness's presence outside the emulator window, carrying the Blocked
count. _Avoid_: dock icon, dock badge — **Dock** in this project is the tmux
frame filling the emulator window, and the two must not be confused.

CONTEXT.md gains this entry under *Structure*, next to Dock.

## Design

### What the surfaces say

| Blocked | Dock tile | Menu bar |
|---|---|---|
| 0 | no badge | `◑` |
| 2 | badge `2` | `█ 2` in Blocked's red |

`█` is `session.Blocked.Glyph()` and the red is `session.Blocked.Colour()`
(`#f85149`) — the same mark and colour the rail and the strip give Blocked, so
the count reads as the same fact in a third place rather than a new one. The
Dock badge keeps the system's own red; nothing is gained by fighting it.

Ready is not on either surface. It has the two it needs — the rail and its
silent, delayed notification — and a number that also moved for unread turns
would stop meaning "something needs a decision", which is the only thing this
count is for. The menu bar keeps `◑` when nothing is Blocked so that the
harness being up is still visible, and so there is something to click; the
count itself appears only when there is one, the same call the strip makes.

### One executable, two roles

`Ganymede.app/Contents/MacOS/Ganymede` becomes a compiled Swift binary in
place of today's `run.sh`, doing one of two jobs depending on how it was
started:

| Started by | Argument | Behaviour |
|---|---|---|
| Spotlight, double-click, `open -a` | none | Exactly today's launcher: spawn `<repo>/bin/ganymede up <repo>` with Homebrew's directories prepended to `PATH`, then exit. No Dock icon, no menu-bar item. |
| `ganymede dashboard`, as a child holding a pipe | `--tile` | Flip to `.regular`, own the Dock tile and the menu-bar item, and set them from labels read off stdin until EOF. |

`LSUIElement` stays `<true/>` in the plist: the launcher role keeps the
invisibility it has today, and only the tile role flips itself into the Dock
at runtime, with `NSApp.setActivationPolicy(.regular)`. The tile role does
appear in Cmd+Tab as a consequence, which is the price of a Dock tile.

`run.sh`'s `@@REPO_DIR@@` substitution moves into `Info.plist` as a
`GanymedeRepo` key, substituted by `make launcher` and read back through
`Bundle.main.infoDictionary`. The `PATH` export moves into the Swift spawn,
for the reason `run.sh` documents: a process launched by LaunchServices
inherits the login session's environment, not an interactive shell's, and
`up` shells out to `tmux` and `terminal-notifier` by bare name.

### The tile role, in full

```swift
// The tile decides nothing. What the badge says is Go's call, arriving one
// label per line; an empty line means nothing is Blocked. EOF means the
// Dashboard that was telling us is gone, and a count nobody stands behind
// any more must not stay on screen.
func read(_ line: String) {
    NSApp.dockTile.badgeLabel = line.isEmpty ? nil : line
    item.button?.attributedTitle = line.isEmpty ? plain("◑") : blocked("█ " + line)
}
```

Clicking either surface brings Ghostty forward — `NSWorkspace.shared.open` on
`/Applications/Ghostty.app`, the same activation `ghostty.Emulator.Activate`
already performs — from `applicationShouldHandleReopen` for the Dock tile and
the status item's own action for the menu bar. Not "jump to the Blocked
Session": the tile would have to be told which one, and once you are in the
window the Dashboard is one keystroke away.

There is no menu on the status item. The Dock tile's own context menu already
offers Quit for free, and a menu here would be a second Dashboard.

### `internal/tile`

The Go side owns every decision, where it can be tested.

```go
// Label is what the Tile shows: the Blocked count, and nothing at all when
// nothing is Blocked. Ready is deliberately absent — see the design.
func Label(waiting session.Attention) string {
    if waiting.Blocked == 0 {
        return ""
    }
    return strconv.Itoa(waiting.Blocked)
}

// Tile is Ganymede's own Dock tile and menu-bar item, driven down a pipe to
// the app bundle's process.
type Tile struct {
    // Start launches the tile process and returns the pipe its labels are
    // written to. Nil means the bundle could not be found, which is not an
    // error: a harness whose launcher was never installed simply has no
    // Tile.
    Start func() (io.WriteCloser, error)

    pipe    io.WriteCloser
    label   string
    started bool
    retired bool
}

// Badge shows what is waiting on you, and is a no-op when the label has not
// moved: a badge written again is a badge redrawn for nothing, and Ready
// moving is not the Tile's business.
func (t *Tile) Badge(waiting session.Attention) error
```

`Badge` starts the process on its first call, writes `Label(waiting)` and a
newline on every change, and **retires the sink on the first failure**, whether
that was the spawn or a write. That is the one place this differs from the
strip, which treats a failed write as worth trying again: a pipe to a child
process does not fail transiently — it fails because the process is gone, which
is what quitting the tile from its own Dock menu does. Retiring means that
gesture is respected until the Dashboard is next started, instead of a new tile
being spawned by the next Session that blocks.

A nil `Start` is not a failure and not an error: `Badge` returns nil and does
nothing, which is the case of a harness whose launcher was never installed.

`Default()` builds a `Tile` whose `Start` runs
`~/Applications/Ganymede.app/Contents/MacOS/Ganymede --tile` with a stdin pipe,
and leaves `Start` nil when the bundle is not there. `runDashboard` wires it in
beside the strip — `Strip: harness, Tile: tile.Default()`.

### The Dashboard's second sink

`counted()` (`internal/dashboard/dashboard.go:878`) grows a `Tile` alongside
`Strip`, behind an interface as small as `Strip`'s and nil-safe the same way:

```go
// Tile is the harness's own Dock tile and menu-bar item — the Blocked count
// where you can read it from another application entirely.
type Tile interface {
    Badge(waiting session.Attention) error
    Close() error
}
```

The function's single early return has to come apart, because today it reads
`if m.harness.Strip == nil || (m.shown && m.waiting == m.written)` — a harness
with a Tile and no strip would never reach the Tile at all:

```go
func (m Model) counted() Model {
    if m.shown && m.waiting == m.written {
        return m
    }
    if m.harness.Tile != nil {
        // Errors are the Tile's own business: it retires itself, and a count
        // the Dock could not be told is not worth a word on a Dashboard that
        // is already showing it.
        _ = m.harness.Tile.Badge(m.waiting)
    }
    if m.harness.Strip != nil {
        if err := m.harness.Strip.Show(m.waiting); err != nil {
            return m
        }
    }
    m.written, m.shown = m.waiting, true
    return m
}
```

The gate on "have the counts moved" comes first and now covers both sinks,
where today it is fused to `Strip == nil`. `m.written`/`m.shown` therefore
advance on a Strip-less harness too, which is what keeps the Tile from being
told the same thing on every registry event.

The strip's memory stays in `m.written`/`m.shown`. The Tile's lives inside the
`*tile.Tile` the interface holds, not in the `Model` — the Model is copied on
every update, and a sink that has to remember whether it spawned a process
cannot have its memory copied out from under it. That also makes the Tile's
dedupe its own: it compares labels rather than whole `Attention` values, which
is what makes Ready's movement free, and it means a strip write that failed
costs the Tile nothing when the next tick tries again.

At `dashboard.go:1051`, where Ctrl+C blanks the strip, the Tile is closed in
the same breath — so the interface is `Badge` and `Close`. EOF would take the
tile down anyway when the process ends, and has to, since `respawn-pane -k`
runs no cleanup at all; closing explicitly is what makes a Dashboard you quit
by hand drop its Dock icon at the moment you quit it rather than a beat later.

### Lifecycle is the pipe

| Event | What happens |
|---|---|
| `ganymede dashboard` starts | Spawns the tile with a pipe. Both surfaces appear, unbadged — the harness being up is itself worth showing. |
| A Session blocks or unblocks | One line down the pipe. |
| The Dashboard exits, is killed, or crashes | The pipe closes, the tile clears both surfaces and quits. |
| `make refresh` | `respawn-pane -k` kills the Dashboard and starts a new one, so the surfaces blink out and return a second later. |
| Spotlight launch while the harness is up | The launcher role runs, `up` refocuses the existing window, and the running tile is untouched. |
| Quit from the Dock tile's menu | The next write fails and the sink retires; nothing respawns until the Dashboard is next started. |

There is no socket, no reconnection, no stale-badge case to guard against and
no timer anywhere: the file descriptor *is* the liveness signal. A badge
cannot outlive the Dashboard that vouched for it, because the same kernel
event that ends the Dashboard is the one that clears the badge.

### What `make launcher` becomes

```makefile
launcher: build
	mkdir -p "$(LAUNCHER_APP)/Contents/MacOS" "$(LAUNCHER_APP)/Contents/Resources"
	sed "s|@@REPO_DIR@@|$(CURDIR)|g" macos/launcher/Info.plist > "$(LAUNCHER_APP)/Contents/Info.plist"
	swiftc -O macos/launcher/Ganymede.swift -o "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	cp macos/launcher/Ganymede.icns "$(LAUNCHER_APP)/Contents/Resources/Ganymede.icns"
	$(LSREGISTER) -f "$(LAUNCHER_APP)"
```

`macos/launcher/run.sh` is deleted, `Info.plist` becomes a template, and
`Ganymede.swift` joins them. `swiftc` ships with the Command Line Tools, which
a machine with a Go toolchain and Homebrew already has; the README's
Prerequisites table gains a row saying so, and the Install section a note that
`make launcher` must be re-run after moving the checkout — which it already
must be today.

### Verified before writing this

A throwaway prototype (30 lines of Swift in a scratch bundle, spawned as an
ordinary child process with a stdin pipe, deliberately never registered with
`lsregister`) confirmed the one assumption the whole design rests on:

```
LSDisplayName = "GanymedeProto"       the bundle's name, not the binary's
LSBundlePath  = …/GanymedeProto.app   resolved from the executable path alone
type          = "Foreground"          the runtime flip to .regular took
icon          = (128.0, 128.0)        the icns loaded
badge=2 → EOF → clearing and quitting
```

A child process gets its enclosing bundle's identity, icon and name, so the
tile does not have to be launched by LaunchServices to look like the app it is
— which is what allows the pipe, and with it the whole absence of a socket.

The badge label was set without error but could not be photographed: this
machine's Dock is set to auto-hide, which is also why the menu-bar item is
here and not deferred.

### Not doing

- **No Ready anywhere on the Tile.** Covered above.
- **No jump-to-the-Blocked-Session on click.** Activating Ghostty is the
  honest fallback the rest of the harness already leans on.
- **No login-item or always-resident tile.** The Tile lives exactly as long
  as the Dashboard, so it can never show a count nothing stands behind.
- **No focus gating.** Notifications are held back while Ghostty is frontmost
  because a banner interrupts; a badge interrupts nothing, and one that blanked
  itself whenever you were looking could never be trusted as a summary of
  where things stand. The strip already makes this call.
- **No bell.** Ghostty's `attention` bounce was considered as a cheaper
  alternative and rejected: it counts nothing, and it fires once.

### Testing

Go carries all of it. Swift gets none — there is nothing to assert there but
AppKit calls — so the acceptance list below stands in for it.

`internal/tile/tile_test.go`, in the shape of `strip_test.go`:

- `Label` over a table: nothing Blocked is the empty label even when Sessions
  are Ready; one Blocked is `"1"`; twelve is `"12"`.
- `Badge` writes the label and a newline to the pipe `Start` handed back, and
  starts the process only on the first call.
- A second `Badge` with the same Blocked count writes nothing, including when
  Ready has moved underneath it.
- A `Start` that fails, and a nil `Start`, both leave `Badge` reporting
  something a Dashboard can ignore, with nothing written.
- A write error retires the sink: the next `Badge` writes nothing and does not
  start a second process.
- Closing the `Tile` closes the pipe.

`internal/dashboard`, alongside the existing `strips` fake:

- Two Blocked Sessions reach a fake Tile as the same `Attention` the strip is
  given, in the same update.
- Counts that have not moved reach neither sink — the existing strip case,
  extended to cover the Tile now that the gate is shared.
- A nil `Tile` changes nothing about the strip or the tree — the case of a
  harness whose launcher was never installed.
- A Tile whose `Badge` fails does not stop the strip being written, and does
  not reach the tree as an error.
- Ctrl+C closes the Tile, the way it already blanks the strip.

Ready-only movement is deliberately *not* a Dashboard test: the Dashboard hands
both sinks every `Attention` that moved, and swallowing the ones where the
Blocked count did not change is the Tile's own job, tested in `internal/tile`.

Manual acceptance, once it is wired up:

- Block a Session: the menu bar reads `█ 1`, and revealing the Dock shows the
  badge on the moon icon.
- Answer it: both go back to `◑` and no badge.
- Block two in different repos: both surfaces read 2.
- Quit the Dashboard: the icon and the item disappear together.
- `make refresh`: they come back, still counting.
- Click the menu-bar item from another application, then the Dock icon:
  Ghostty comes forward both times.

### Docs

- **CONTEXT.md** — the *Tile* entry above.
- **ARCHITECTURE.md** — the Tile under Notifications (it is the same
  "attention beyond the Dashboard" idea, differing in that it stands rather
  than interrupts), a line in *What the harness writes* for the bundle now
  holding a compiled binary and a substituted `Info.plist`, and the data-flow
  diagram gaining the Tile beside the notifier.
- **README.md** — `swiftc` in Prerequisites, and the Tile in the section that
  explains where attention reaches you.
