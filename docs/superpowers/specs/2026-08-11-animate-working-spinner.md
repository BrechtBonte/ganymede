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
// or an InUse root.
func (m Model) animating() bool {
	for _, r := range m.rows {
		if r.session != nil {
			if r.session.State == session.Working {
				return true
			}
			continue
		}
		if r.state == repo.InUse {
			return true
		}
	}
	return false
}
```

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
  // repoGlyph is a Main root's mark at the current frame: InUse borrows
  // Working's own animated mark, the same borrowing rootStyle already makes
  // for its colour; everything else stands still on its Glyph.
  func (m Model) repoGlyph(state repo.State) string {
  	if state == repo.InUse {
  		return session.Working.Frame(m.spinner)
  	}
  	return state.Glyph()
  }
  ```

### Testing

- `internal/session/session_test.go`: `Frame()` cycles through all ten
  spinner frames in order and wraps around; every non-Working state's
  `Frame(tick)` equals its `Glyph()` for a handful of ticks.
- `internal/dashboard`: a synthesized `Spin{}` message advances the glyph
  drawn for a Working row and for an InUse repo header row; once nothing is
  left animating, `Update(Spin{})` returns a `nil` `tea.Cmd` rather than
  rescheduling.
- `internal/dashboard/roots_test.go`'s two `repo.InUse.Glyph()` assertions
  become `session.Working.Frame(0)` — deterministic, since these test
  helpers construct a `Model` via `Update` and never execute the returned
  `tea.Cmd`, so `spinner` never leaves its zero value.

## Non-goals

- No new dependency — `charmbracelet/bubbles`' spinner component was
  considered and rejected in favour of a hand-rolled frame table, matching
  `Glyph()`/`Colour()`'s own hand-rolled style.
- No animation for Blocked, Ready, Idle, or Shell.
- No change to the 30s `Tick`/`ticking()` loop or to the tmux attention
  strip (`topology/strip.go`), which never draws Working at all.
