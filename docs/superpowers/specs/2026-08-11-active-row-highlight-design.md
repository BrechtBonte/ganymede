# The Dashboard always marks the row actually on screen

## Problem

The Dashboard has exactly one highlight: `m.cursor`'s row, styled by
`selectedRowStyle()` (bright reverse while the Dashboard holds keyboard
focus, dim once it does not). Nothing on the Dashboard separately remembers
*which Session's pane the working client is actually showing* — the cursor
is made to stand in for both "what you're browsing" and "what's on screen."

Those two drift apart in two ways already possible today:

- You browse the tree with up/down after jumping somewhere — the cursor
  moves, but the working client still shows the Session you last jumped to.
  Nothing marks that row any more.
- The guard's own mismatch fallback (`jumpTo(msg.session, false)` in the
  `answered` case, §7.2) repoints the working client at a Session from a
  background event, with keyboard focus deliberately left on the Dashboard
  and the cursor wherever you had it. The pane changes; no row says so.

## Design

### New field: `Model.active`

```go
// active is the PID of the Session the working client's pane last actually
// showed, tracked apart from the cursor: the cursor is what you're
// browsing, this is what's on screen, and Enter usually but not always
// keeps them the same row. Zero means no Session has been shown yet.
active int
```

### Where it's set

Anywhere the code calls `Jumper.Jump` and it succeeds — that call is the one
thing that actually repoints the working client's pane, whether or not
keyboard focus follows it:

- **`jumpTo`** — after a successful `Jump`, unconditionally. Today `Focus`
  is only called when `moveFocus` is true; `active` is set either way, since
  the guard's async fallback (`moveFocus == false`) changes the pane just as
  much as the direct Enter gesture does.
- **`focusPane`** — same, after a successful `Jump`. Today its error is
  discarded outright (`_ = m.harness.Jumper.Jump(pid)`); this starts
  checking it so a failed Jump — the pane never actually changed — does not
  mark a row active it never showed. The error itself is still not
  surfaced, matching `focusPane`'s existing best-effort contract.

### Where it's cleared

- **`goTo`** — after a successful `Open`, `active` resets to 0. Opening a
  bare repo (Enter on a repo header with no live Session, or the picker's
  Enter) points the working client at a directory with no Session in it at
  all, so no row should keep reading as active once that happens.
- **`forget`** — clears `active` if the pid it just confirmed Gone is the
  one `active` was holding. The row is already gone from `m.rows` by then,
  so nothing visible would change either way; this is only so the field
  itself doesn't outlive the Session it named.

### Rendering

`line()`'s session branch gains a middle case, between the cursor's own
styling and the plain default:

```go
switch {
case i == m.cursor:
    return m.selectedRowStyle().Width(m.width).Render(...)
case r.session.PID == m.active:
    return blurredSelectedStyle.Width(m.width).Render(...)
default:
    return spread(...)
}
```

`blurredSelectedStyle` already exists for exactly this weight of mark; this
reframes what it means, slightly, into something more general than its
current doc comment says: bright reverse is "the cursor is here *and* your
keystrokes are actually going here right now" (cursor's row, Dashboard
focused); dim reverse is "this is what's on screen, but not what you're
doing right now" — which covers both the existing case (cursor's row, focus
moved to the working client) and the new one (a different row than the
cursor, but the one actually showing). The doc comment on
`blurredSelectedStyle` is updated to say this.

Repo header rows are untouched — `active` only ever names a Session's PID,
never a repo, so `repoLine` needs no new branch. The SELECTED detail box
stays keyed to the cursor alone, as today: it answers "what is the cursor
on," not "what is on screen."

### Not doing

- No auto-scroll to keep the active row in view when the cursor scrolls it
  off-screen. Nobody asked for that, and the tree already centers on the
  cursor.
- No change to what counts as "active" — repo header rows opened via `goTo`
  never set it, only a live Session jumped to by `jumpTo` or `focusPane`
  does.

### Testing

New cases alongside the existing `focus_test.go` / `dashboard_test.go`
fakes (`jumps`, `focuses`, `sidepanel`, `press`, `live`):

- Enter on a Session row, then Down to browse away from it: the row Enter
  landed on keeps a dim reverse; the cursor's new row gets the plain
  bright/dim-by-focus treatment; they are visibly different styles when the
  Dashboard is focused.
- The guard's mismatch fallback (`answered`, `moveFocus == false`) on a
  Session other than the one under the cursor: that Session's row picks up
  the dim reverse without moving the cursor or calling `Focus` — extending
  `TestApproveTheGuardCouldNotVerifyDoesNotMoveFocus`'s scenario to a
  multi-session dashboard.
- Enter on a repo header with no live Session (`goTo`), after a prior Enter
  had marked some Session active: the Session's row loses the dim reverse
  once the repo opens.
- A `Jump` that fails (`jumps{err: ...}`) does not mark the target row
  active — mirroring `TestAJumpThatCannotBeMadeIsReported`.
