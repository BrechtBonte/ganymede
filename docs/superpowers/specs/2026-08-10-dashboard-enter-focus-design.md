# Dashboard Enter moves focus into the working client

## Problem

Pressing Enter on the Dashboard already puts the right thing in front of you —
`Jump` or `Open` points the working client at the chosen Session or repo — but
your keystrokes stay on the Dashboard until you separately press alt+g. Enter
should take you all the way in: point the working client *and* move keyboard
focus to it, in one gesture.

## Why this gap exists

The dock is two tmux panes side by side: the Dashboard (pane 0) and the
working client (pane 1), itself a tmux client attached to a separate server
holding the repo Sessions. Two different tmux servers own the two things Enter
needs to do:

- **What the working client shows** — `switch-client` on the Sessions server.
  This is what `Jump` and `Open` already do.
- **Which dock pane has keyboard focus** — `select-pane` on the dock server.
  This is what alt+g does, and what Enter has never touched.

## Design

### New primitive: `topology.Harness.Focus`

```go
// Focus moves keyboard focus in the dock to the working client's pane — the
// same move alt+g makes, given automatically once a jump or an open has
// already put something new in front of you.
func (h Harness) Focus() error {
	return h.dock().run("select-pane", "-t", "="+DockSession+":0.1")
}
```

Lives in a new `internal/topology/focus.go`, alongside `jump.go` and
`open.go`'s one-action-per-file shape. Targets `=dock:0.1` — the same pane id
`ensureDock` already selects when the dock is first built — since the dock's
layout is fixed by construction: pane 0 is always the Dashboard, pane 1 always
the working client.

### New interface: `dashboard.Focuser`

```go
// Focuser moves keyboard focus to the working client's pane — the dock's own
// alt+g, given automatically once Enter has already put something in front
// of you.
type Focuser interface {
	Focus() error
}
```

Sibling to `Jumper` and `Opener`. Added as a `Focuser Focuser` field on
`dashboard.Harness`, following the same "any of them may be absent" contract
as the rest of that struct. Wired in `cmd/ganymede/main.go`'s
`dashboard.Harness{...}` literal as `Focuser: harness` — `topology.Harness`
satisfies the interface structurally, same as it already does for `Jumper`
and `Opener`.

### Call sites

Enter reaches two paths in `dashboard.go`, and both get Focus:

- **`jump()`'s session branch** (`m.jumpTo(*selected.session)`) — the direct,
  synchronous Enter-on-a-session-row gesture.
- **`goTo()`** (`m.harness.Opener.Open(root)`) — reached both from Enter on a
  repo row with no live Session yet, and from the picker's Enter
  (`picker.go`'s `m.goTo(chosen)`). Both are direct user gestures, so `goTo`
  calls Focus unconditionally after a successful Open.

`jumpTo` is not only reached from `jump()`. `dashboard.go`'s `answered`
message case — the guard's async fallback when it could not verify an
approve/deny actually reached the pane — also calls `jumpTo`, from a
background event that may land while you're mid-typing something else
entirely on the Dashboard. Stealing dock focus there would yank your
keystrokes into a different Session out from under you. So `jumpTo` gains a
`moveFocus bool` parameter:

```go
func (m Model) jumpTo(s session.Session, moveFocus bool) Model {
	if m.harness.Jumper == nil {
		return m
	}
	if err := m.harness.Jumper.Jump(s.PID); err != nil {
		m.notice = err.Error()
		return m
	}
	if m.harness.Seen != nil {
		m.harness.Seen(s.ID)
	}
	if moveFocus && m.harness.Focuser != nil {
		_ = m.harness.Focuser.Focus()
	}
	return m
}
```

- `jump()` calls `m.jumpTo(*selected.session, true)`.
- The `answered` case calls `m.jumpTo(msg.session, false)`.

`focusPane` — the shared fallback behind the `sent` mismatch, the Takeover
`endFailed` fallback, and Stop's own fallback — is left untouched entirely.
It already exists specifically to show a pane without counting it as seen;
the same reasoning that keeps it from touching `Seen` keeps it from touching
dock focus.

### Error handling

`Focus` errors are ignored (`_ = ...Focus()`), the same best-effort treatment
`focusPane`'s own `Jump` call and popup sweeping already get elsewhere in this
codebase. A failed focus-move has nothing actionable to tell the user, and
must not overwrite the notice a successful Jump or Open already left blank.

### Testing

A `focuses` fake (`Focus() error`, recording calls, returning a settable
error) mirrors the existing `jumps` and `opens` fakes. New assertions:

- Enter on a session row calls `Jump` then `Focus`.
- Enter on a repo-only row, and the picker's Enter, call `Open` then `Focus`.
- The `answered`-mismatch fallback calls `Jump` (via `jumpTo`) but not `Focus`.
- `focusPane`'s callers (`sent` mismatch, Takeover's `endFailed`, Stop's
  fallback) call `Jump` but not `Focus` — unchanged from today.
