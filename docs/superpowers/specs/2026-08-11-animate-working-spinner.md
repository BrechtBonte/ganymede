# Animate the Working spinner

## Problem

`Working`'s mark (`⠿`) and the InUse root mark (`▣`) are both static. A
Session mid-turn reads exactly like one that has stalled — the rail has no
way to say "this is still moving" short of the age counting up.

## Design

### `session.State.Frame`

A new method beside `Glyph()` and `Colour()`, in `internal/session/session.go`:

```go
// spinnerFrames is the braille cycle a Working glyph steps through — the
// classic CLI spinner, so the one state that means "your turn is running"
// is the one mark on the rail actually in motion.
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Frame is how a State reads at the given tick of the animation clock: Glyph
// for every state but Working, which instead cycles through spinnerFrames.
func (s State) Frame(tick int) string {
	if s == Working {
		return spinnerFrames[tick%len(spinnerFrames)]
	}
	return s.Glyph()
}
```

`Glyph()` itself is untouched — it stays the answer to "what does this state
look like at rest," which is still what `counts()` and the tmux attention
strip (`topology/strip.go`) want for Blocked and Ready, neither of which
animates.

### The spin clock

`dashboard.Model` gains two fields: `spinner int` (the current tick) and
`spinTicking bool` (so a rebuild that finds Working rows while a loop is
already running does not stack a second one on top of it).

```go
// Spin is the Dashboard asking to be drawn one frame further into whatever
// is spinning on the rail.
type Spin struct{}

// spinning drives the spinner clock. Unlike ticking()'s half-minute, this
// one only exists to be fast — and it stops rescheduling itself the moment
// animating() says nothing needs it, rather than running forever in the
// background of a Dashboard sitting quiet at the prompt.
func spinning() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return Spin{} })
}

// animating says whether anything on the rail is mid-spin: a Working Session
// or a root whose holder is Working.
//
// InUse alone is not enough (state.go): it fires for any live occupant —
// Idle, Ready, Blocked, Shell included — and a spinner gated on it would
// never stop for as long as a root merely had somebody sitting in it, which
// is most of the time a repo is on the rail at all. row.holderWorking is the
// narrower question the animation actually needs: not "is somebody here" but
// "is that somebody's turn running."
func (m Model) animating() bool {
	for _, r := range m.rows {
		if r.session != nil {
			if r.session.State == session.Working {
				return true
			}
			continue
		}
		if r.holderWorking {
			return true
		}
	}
	return false
}
```

#### `row.holderWorking`

A header row's `state` says whether *someone* holds the Main root; it says
nothing about what that someone is doing. `rowsOf` (`rows.go`) already
computes, per Session, whether it is the one actually holding the root
(`holdsRoot`, via `ask.checkout(running.Dir) == root`) — the header row needs
the same fact taken one step further: not just which Session holds the root,
but whether that Session is Working. A new field on the header row, set
alongside `state` in `rowsOf`:

```go
// holderWorking says the Session actually holding this root — as against
// every Session merely grouped under the repo — is Working, on a repo's
// header row. It is narrower than state == InUse, which fires for a holder
// in any state; it is what says whether the header row's own mark has
// anything worth animating.
func holderWorking(root string, group []session.Session, ask answers) bool {
	for _, s := range group {
		if ask.checkout(s.Dir) == root {
			return s.State == session.Working
		}
	}
	return false
}
```

called where `state := stateOf(root, byRoot[root], ask, claimed)` already is,
and stored as `row.holderWorking` next to `state`.

`Update` wiring:

- On `Sessions` — the only message that can actually change a Session's
  state — start `spinning()` if `m.animating() && !m.spinTicking`.
- On `Spin` — increment `m.spinner`, then either reschedule (`animating()`
  still true) or clear `spinTicking` and let the loop end.

### Rendering

- `line()`'s session glyph: `r.session.State.Glyph()` becomes
  `r.session.State.Frame(m.spinner)`.
- `repoLine()` and `selected()` both draw a repo header row's mark. Both
  route through a new helper:

  ```go
  // repoGlyph is a Main root's mark at the current frame: an InUse root
  // whose holder is Working borrows Working's own animated mark — the same
  // borrowing rootStyle already makes for its colour. An InUse root held by
  // an Idle, Ready, Blocked, or Shell Session, and every other state, stand
  // still on Glyph.
  func (m Model) repoGlyph(r row) string {
  	if r.state == repo.InUse && r.holderWorking {
  		return session.Working.Frame(m.spinner)
  	}
  	return r.state.Glyph()
  }
  ```

  Note this retires `repo.InUse.Glyph()` (`▣`) from the header row only while
  the holder is Working — the plain `▣` still shows for every other InUse
  root, exactly as today.

### Testing

- `internal/session/session_test.go`: `Frame()` cycles through all ten
  spinner frames in order and wraps around; every non-Working state's
  `Frame(tick)` equals its `Glyph()` for a handful of ticks.
- `internal/dashboard`: a synthesized `Spin{}` message advances the glyph
  drawn for a Working row and for a header row whose holder is Working;
  once nothing is left animating, `Update(Spin{})` returns a `nil` `tea.Cmd`
  rather than rescheduling.
- A new test puts an Idle Session in an InUse root (the existing
  `TestRepoHeaderMarksAMainRootASessionIsWorkingIn` and
  `TestALiveOccupantOutranksAClaimOnTheHeaderRow` already do exactly this,
  unchanged) and asserts `animating()` is false and the header row still
  reads the plain `repo.InUse.Glyph()` — an occupied-but-not-Working root
  must not spin, and must not keep the tick loop alive forever. A companion
  test swaps that Session to Working and asserts the header row now reads
  `session.Working.Frame(0)` instead.
- `internal/dashboard/roots_test.go`'s existing `repo.InUse.Glyph()`
  assertions need no change: both use an Idle occupant already
  (`session.Idle`), so `holderWorking` is false for both and the plain `▣`
  is exactly what they still see.

## Non-goals

- No new dependency — `charmbracelet/bubbles`' spinner component was
  considered and rejected in favour of a hand-rolled frame table, matching
  `Glyph()`/`Colour()`'s own hand-rolled style.
- No animation for Blocked, Ready, Idle, or Shell.
- No change to the 30s `Tick`/`ticking()` loop or to the tmux attention
  strip (`topology/strip.go`), which never draws Working at all.
