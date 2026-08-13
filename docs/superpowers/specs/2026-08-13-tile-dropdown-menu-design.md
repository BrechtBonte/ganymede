# The Tile's menu-bar item gets a dropdown

## Problem

The Tile (`docs/superpowers/specs/2026-08-13-blocked-count-tile-design.md`)
carries the Blocked count and nothing else: the menu-bar item reads `◑` or
`█ N`, and clicking it — like the Dock icon — does one thing, jump to
Ghostty. That leaves Ready and Working invisible outside Ghostty's own
window, and the menu-bar item itself offers nothing to look at beyond the
Blocked digit.

Clicking the item should open a menu: an explicit "Open Ganymede" action
(since a click no longer jumps there by itself), and the full Blocked / Ready
/ Working breakdown, each tier in its own colour once there is one of it.

## Design

### What the surfaces say

The Dock badge and the menu-bar title are unchanged — still Blocked alone,
exactly as `docs/superpowers/specs/2026-08-13-blocked-count-tile-design.md`
left them:

| Blocked | Dock tile | Menu bar |
|---|---|---|
| 0 | no badge | `◑` |
| 2 | badge `2` | `█ 2` in Blocked's red |

Clicking the menu-bar item now opens a menu instead of jumping straight to
Ghostty:

```
Open Ganymede
─────────────
█ 2 Blocked      ← red, session.Blocked.Colour()
● 1 Ready        ← green, session.Ready.Colour()
⠿ 4 Working      ← blue, session.Working.Colour()
```

All three rows are always present, even at zero — a quiet working set reads:

```
Open Ganymede
─────────────
█ 0 Blocked      ← ordinary menu text colour
● 0 Ready        ← ordinary menu text colour
⠿ 0 Working      ← ordinary menu text colour
```

A row is only in its tier's colour once its count is nonzero; at zero it is
just the menu's own text colour, the same call the strip and the Dock badge
already make about a tier that has nothing in it. Glyphs (`█` `●` `⠿`) and
colours are the same ones `session.State.Glyph()`/`.Colour()` give each tier
on the rail and the strip — this is a fourth surface for a vocabulary that
already exists in three places, not a new one.

The Dock icon's own reopen gesture (`applicationShouldHandleReopen`) is
untouched: it still jumps straight to Ghostty. Only the status-bar item's
click grows a menu.

### Wire protocol

One line per update, as today, but three space-separated integers —
`blocked ready working`, e.g. `2 1 4`, or `0 0 0` for a quiet working set —
instead of a single Blocked-or-empty string. There is no more empty-line
case: the harness always has a number for all three tiers, so the app's
stdin reader is a plain three-field split rather than an empty/non-empty
branch.

### `internal/tile`

`Counts` replaces `Label`'s use of `session.Attention` as what the Tile is
told, since the dropdown needs a tier `session.Attention` deliberately
doesn't carry:

```go
// Counts is what the Tile shows, in full: every tier the working set is in,
// not only the one the Dock badge counts. The dropdown reads all three; the
// Dock badge and the menu-bar title still read Blocked alone, exactly as
// before.
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
```

`Badge` moves from a Blocked-only string dedupe to comparing whole `Counts`
values, and writes all three down the pipe:

```go
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
```

`t.label string` becomes `t.counts Counts`. `Label` is gone — nothing derives
a display string from `session.Attention` inside the tile package any more;
`Counts`/`CountsIn` are the whole of what it decides.

### `internal/dashboard`'s second sink

`counted()` gates both sinks today on one shared question — "has `Attention`
moved" — because until now that also answered "has what the Tile shows
moved", since the Tile only ever showed a subset of Attention. That stops
being true here: the dropdown needs Working, which `Attention` doesn't carry,
so a Working-only change must still reach the Tile even though the strip has
nothing new to say. The gate splits: the Tile is asked on every pass and
answers for itself (`Badge` already no-ops when nothing has changed), while
the Strip keeps its existing Attention-only gate exactly as it is.

```go
func (m Model) counted() Model {
    if m.harness.Tile != nil {
        _ = m.harness.Tile.Badge(tile.CountsIn(m.set))
    }
    if m.shown && m.waiting == m.written {
        return m
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

The `Tile` interface `dashboard.go` declares grows the same way:

```go
type Tile interface {
    Badge(counts tile.Counts) error
    Close() error
}
```

No new `Model` field: `tile.CountsIn(m.set)` is computed fresh each pass from
the working set that is already there, the same way `session.AttentionIn(m.set)`
already is. `Badge`'s own dedupe is what makes a repeat call cheap, so `Model`
does not need a second copy of "what was last sent" alongside `m.written`.

### A behaviour this flips

`TestNeitherSinkHearsCountsThatHaveNotChanged` (dashboard_test.go) and the
"Ready moving is not the Tile's business" comment in tile.go both encode
today's invariant: the Tile only cares about Blocked. Both go — the Tile now
cares about all three tiers the dropdown shows. The rewritten test asserts
the new shape of the same idea: a Working-only change reaches the Tile, still
doesn't reach the strip, since Working carries no Attention.

### `macos/launcher/Ganymede.swift`

New constants beside `blockedRed`/`blockedGlyph`, mirroring
`session.Ready`/`session.Working`'s colour and glyph the same way `blockedRed`
already mirrors `session.Blocked`:

```swift
let readyGreen = NSColor(srgbRed: 0x3F / 255, green: 0xB9 / 255, blue: 0x50 / 255, alpha: 1)
let workingBlue = NSColor(srgbRed: 0x58 / 255, green: 0xA6 / 255, blue: 0xFF / 255, alpha: 1)
let readyGlyph = "●"
let workingGlyph = "⠿"
```

`TileDelegate` builds one `NSMenu` at launch and keeps references to the
three count rows so later updates restyle them in place rather than
rebuilding the menu on every line read off stdin:

```swift
private var blockedItem, readyItem, workingItem: NSMenuItem!

private func buildMenu() -> NSMenu {
    let menu = NSMenu()
    let open = menu.addItem(withTitle: "Open Ganymede", action: #selector(clicked), keyEquivalent: "")
    open.target = self
    menu.addItem(.separator())
    blockedItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
    readyItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
    workingItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
    return menu
}
```

`item.menu = buildMenu()` replaces `item.button?.target`/`.action`: an
`NSStatusItem` with a `menu` assigned shows it on any click by itself, so
`clicked()` moves from being the button's own action to "Open Ganymede"'s.

`show(_ line:)` parses three integers instead of one label, leaves the Dock
badge and menu-bar title exactly as they read today, and restyles the three
rows:

```swift
private func show(_ line: String) {
    let counts = line.split(separator: " ").compactMap { Int($0) }
    guard counts.count == 3 else { return }
    let (blocked, ready, working) = (counts[0], counts[1], counts[2])

    NSApp.dockTile.badgeLabel = blocked == 0 ? nil : String(blocked)
    item?.button?.attributedTitle = blocked == 0
        ? NSAttributedString(string: idleGlyph, attributes: [.font: NSFont.menuBarFont(ofSize: 0)])
        : NSAttributedString(string: blockedGlyph + " " + String(blocked), attributes: [
            .font: NSFont.menuBarFont(ofSize: 0), .foregroundColor: blockedRed,
          ])

    row(blockedItem, blockedGlyph, blocked, "Blocked", blockedRed)
    row(readyItem, readyGlyph, ready, "Ready", readyGreen)
    row(workingItem, workingGlyph, working, "Working", workingBlue)
}

// row sets one dropdown line: the tier's mark, count and name, in the tier's
// own colour once there is one of it and the menu's ordinary text colour
// otherwise — a zero row still names the tier, it is just not something to
// look twice at.
private func row(_ item: NSMenuItem, _ glyph: String, _ count: Int, _ name: String, _ colour: NSColor) {
    item.attributedTitle = NSAttributedString(string: "\(glyph) \(count) \(name)", attributes: [
        .foregroundColor: count == 0 ? NSColor.labelColor : colour,
    ])
}
```

EOF handling is unchanged: it still clears the Dock badge and terminates the
tile process.

### Docs

CONTEXT.md's **Tile** entry gains a sentence: clicking it opens a menu with
the Blocked/Ready/Working breakdown and an "Open Ganymede" action, rather
than jumping straight there.

### Testing

`internal/tile/tile_test.go`:

- `CountsIn` over a table: an empty set, one of each state, several of one
  state, and states the Tile doesn't count at all (Idle, Shell) contributing
  to none of the three fields.
- `Badge` writes all three counts as one line (`"2 1 4\n"`), starts the
  process only on the first call, and a repeat with the exact same `Counts`
  writes nothing — including when only Working has moved, since any of the
  three fields changing must write, and only a `Counts` identical in all
  three must not.
- The Start-fails / write-fails / nil-Start / Close cases carry over
  structurally unchanged, just typed on `Counts` instead of
  `session.Attention`.

`internal/dashboard`:

- `TestTheTileIsToldTheSameCountsAsTheStrip` is rewritten to compare the
  Blocked/Ready portion of what the Tile got against what the strip got
  (still the same update), with Working recorded on the Tile's side alone.
- `TestNeitherSinkHearsCountsThatHaveNotChanged` is replaced by a test
  asserting the new invariant: a Working-only change reaches the Tile, not
  the strip.
- The rest — nil Tile, a Tile that cannot be reached, Ctrl+C closing the
  Tile — carry over unchanged; none of them depend on what shape `Counts` is.

Swift has no test target (`swiftc` compiles the file directly, no Xcode
project) — manual acceptance once `make launcher` is rerun:

- Click the icon with a quiet working set: the dropdown reads "0 Blocked / 0
  Ready / 0 Working" in the ordinary menu text colour, above a separator
  under "Open Ganymede".
- Block a Session: the dropdown's Blocked row turns red with the right count;
  the menu-bar title and Dock badge behave exactly as they do today.
- Start a Session working with nothing Blocked or Ready: the dropdown's
  Working row turns blue; the menu-bar title and Dock badge are untouched
  (still `◑`), since Working carries no Attention.
- Click "Open Ganymede" from another application: Ghostty comes forward, the
  same activation clicking the icon performs today.
- Quit the Dashboard: the icon and its dropdown disappear together, same as
  today.

### Not doing

- No jump-to-a-specific-Session from any dropdown row — same reasoning as
  "Open Ganymede" itself: the Tile isn't told which Session, and once you're
  in the window the Dashboard is a keystroke away.
- No Quit item on the dropdown — the Dock tile's own context menu already
  offers one; this menu is additive, not a replacement.
- No live-updating menu while it is actually open on screen — AppKit doesn't
  require it for correctness (the counts are restyled the moment the next
  line arrives, same as today), and it would be new complexity for a window
  of time measured in how long it takes to read three numbers.
- No change to the Dock tile itself or to the tmux attention strip — both are
  exactly as they are today.
