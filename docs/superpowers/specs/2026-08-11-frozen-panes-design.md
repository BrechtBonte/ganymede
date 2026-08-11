# Surface a Frozen pane

## Problem

A tmux pane in copy-mode shows a held view. The program underneath keeps
running and keeps writing to its screen; the client keeps rendering the
snapshot the mode was entered on, and every keystroke goes to the mode's own
key table instead of to the program. The harness says nothing about any of
this.

It happened for real. `ganymede-repo:0.0` sat in copy-mode while the Session
in it went from `⏱ 11m` / 6% context / mid-turn to `⏱ 19m` / 8% / finished —
the rail showed the Session moving through those states correctly the whole
time, because the registry never stopped being right. What the rail could not
say is that the pane in front of you had stopped showing any of it. The
screen read as a hang; nothing was hung.

`#{pane_in_mode}` was 1 on exactly one of the ten panes the harness had. The
harness never asks.

Two things follow from never asking:

- **The rail cannot say why a Session looks dead.** Every state it draws is
  about the Claude process. None of them is about whether the pane is still
  showing you that process.
- **The guard sends keys into the mode.** `answer` (`internal/topology/guard.go`)
  captures the pane, checks it shows the dialog, and sends. `capture-pane`
  returns the live screen, not the mode's held view, so a Frozen pane passes
  the check — then the keystroke lands in the copy-mode key table, the dialog
  never moves, and `settled` fails half a second later reporting that the pane
  "still shows the dialog after Y was sent". True, and useless: the pane never
  got the Y.

## Design

### The term

CONTEXT.md gains **Frozen**, in a new `### Pane view` subsection placed after
`### Session states` — deliberately *not* inside it. A sixth entry beside
Working, Blocked, Ready, Idle and Shell would say Frozen is a state a Session
can be in instead of those, and it is not: the incident above was a Frozen
pane over a Working Session, and both facts were true at once.

```markdown
### Pane view

**Frozen**:
The Session's pane is showing a held tmux view rather than the live Session —
your keys reach the mode, not Claude. Orthogonal to every Session state: a
Frozen pane can sit over a Working one, which is exactly when it reads as a
hang.
_Avoid_: stuck, hung, copy-mode, scrolled
```

Frozen is a property of a Session's **pane**, so it belongs to Session rows
only. A repo's header row has no pane of its own — the `⏵` that can appear
there is about a Popup shell over the Main root directory, which has no mode
to be in.

`#{pane_in_mode}` is 1 for view-mode as well as copy-mode. Both hold the view
still and both take the keyboard, so one term covers both without qualifying
which mode it was.

### Detection

One primitive, two readers, on the socket the Sessions live on.

#### `panes()` grows a field

`internal/topology/jump.go`'s `panes()` already runs one `list-panes -a` over
every Session pane and maps the process tmux started to its pane id. It reads
one more field:

```go
// panes maps the process tmux started in each pane to that pane's id, and
// says which of those panes is holding a mode over the live view.
func (h Harness) panes() (map[int]string, map[string]bool, error)
```

`-F "#{pane_pid} #{pane_id} #{pane_in_mode}"`. `locate` ignores the second
map; `Jump` is unchanged. No new tmux call is added to any path that already
walks this one.

#### `Harness.Frozen`

```go
// Frozen says which of pids are running in a pane holding a mode over the
// live view. It resolves each pid through the same parent walk locate makes:
// tmux knows the process it started in the pane, and a Session is that
// process's descendant.
//
// A pid the harness cannot place in a pane at all is absent rather than
// false — the two answers differ, and a Session started outside tmux has no
// pane whose view could be held.
func (h Harness) Frozen(pids []int) (map[int]bool, error)
```

#### The guard asks before it sends

In `answer`, between `locate` and the first `capturePane`:

```go
held, err := h.modeHeld(target)
if err != nil {
	return err
}
if held {
	return fmt.Errorf("pane %s is frozen: it is showing a held view, not the live Session", target)
}
```

`modeHeld` is a per-target `display-message -p -t <target> '#{pane_in_mode}'`
rather than a reuse of `Frozen`: the guard already has the pane id, and asking
about the one pane it is about to send to is both cheaper and less racy than
asking about every pane and indexing back in. It is named apart from `Frozen`
rather than case-shifted from it, so the two are told apart at every call
site.

This is step 2 of the guarded send-keys protocol arriving before the capture
rather than a new step of its own — the pane must be showing the dialog *and*
be able to receive the key, and the second question is the cheaper of the two.

Refusal behaves like every other guard mismatch: nothing is sent, and the
Dashboard's existing fallback focuses the pane. You land on it and decide.
The harness deliberately does **not** clear the mode: a pane scrolled back on
purpose to read something is in exactly the same state as one frozen by
accident, and the harness cannot tell them apart. Taking a scrollback
position away from someone who set it deliberately is the worse of the two
mistakes.

### Edges and the cross-check

The same shape the state model already uses: hooks give the edges, a slow
timer cross-checks.

**Edge.** `internal/tmuxconf` gains a fragment beside `seenHook`:

```
set-hook -g pane-mode-changed 'run-shell -b "#{q:@ganymede-seen} frozen #{pane_pid} #{pane_in_mode}"'
```

`pane-mode-changed` fires on entering *and* leaving a mode, so the mark
appears the moment the view is held and clears the moment it is released.
`-b` for the same reason `seenHook` uses it: tmux is free the moment the
command has started.

This makes a second global tmux hook the harness owns, with the same
consequence already documented for `pane-focus-in` — a `pane-mode-changed`
hook of the user's own would be replaced by it. ARCHITECTURE.md's "What the
harness writes" currently names one; it says two after this.

**CLI.** `ganymede frozen <pane-pid> <0|1>`, modelled directly on `seen`:
read the registry, resolve which Sessions run inside the pane process with
`topology.Under`, and forward one event per Session. Like `seen` and `hook`
it is run by something that must not be held up, so it reports nothing it
could not do.

**Events.** `internal/hooks` gains two Kinds, `Froze` and `Thawed`.
`hooks.Event` needs no shape change: it is already keyed by `Session` (the
Claude session id), which is what the `frozen` command has after resolving
the pane.

**Cross-check.** The existing half-minute `Tick` — the one that re-reads
cautions and sweeps popups — also calls `Frozen` over the live Session pids
and sends a `FrozenPanes` message. This is what covers a mode entered while
the Dashboard was down, a hook that never fired, and a fragment not yet
sourced into a running server.

A sweep that *failed* sends no message at all, leaving the last answer
standing rather than blanking a mark — the same reasoning already written
into the popup sweep, and for the same reason: a marker that blinks out while
tmux is being asked again is a marker you stop trusting.

#### Keying

The Dashboard keys Frozen by **Claude session id**, not pid.

The event path is the primary source and already speaks session ids with no
change to `Event`. The cross-check converts pid to id where it builds its
message, which it can do because it is holding the Session list it just asked
about. Keying by pid instead would leave the primary path converting on every
edge, and `Event` carries no pid to convert from.

`rows.go` documents why *rows* are keyed by pid — the pid is checked, the id
comes from an undocumented file — and that reasoning is untouched: rows keep
their pid key and look Frozen up by `r.session.ID`. A Session whose id the
harness never learned simply never carries the mark, which is the same
degradation the Ready badge already has.

### Dashboard

A one-column mark beside the two that exist:

```go
// frozen is the mark a row carries while its pane is holding a mode over the
// live Session: what you are looking at in that pane is a held view, and the
// keys you type into it are going to the mode.
const frozen = "❄"
```

- `Model` gains `frozen map[string]bool`, laid over rather than cleared —
  like `cautions` and `popups`, for the reason both already document.
- `FrozenPanes map[string]bool` joins `Cautions` and `PopupStatuses` as a
  message type, plus `Froze`/`Thawed` handling on the event path.
- `row` gains `frozen bool`; `answers` gains `frozen func(id string) bool`,
  set in `rowsOf` alongside `ticket` and `popup`.
- `busyMark` becomes `marks`, returning the Frozen mark ahead of the popup
  one where both apply. Frozen goes first: whether the row you are reading is
  even live is read before what a popup under it is doing.

```
  ⠿ ❄ ganymede-1e           no ticket 1m
  ⠿ ❄ ⏵ ganymede-1e        no ticket 1m
```

- The SELECTED box appends `· frozen` to the state line:
  `⠿ Working · 1m · frozen`.

**What it does not touch.** Not `Attention`, not `tier`/`moreUrgent`, not the
tmux attention strip, not the notifier. Frozen is your own doing, not the
Session asking something of you; a Blocked Session behind a Frozen pane is
still Blocked, still sorts as Blocked, and still pings. The mark says what is
between you and the Session, and changes nothing about what the Session
wants.

### Documentation

- **CONTEXT.md** — the `### Pane view` subsection above.
- **ARCHITECTURE.md** — `ganymede frozen` in the CLI entry-points table; the
  mode check in the guarded send-keys protocol list; the second harness-owned
  tmux hook in "What the harness writes"; a line in Dashboard internals for
  the mark.

## Testing

`internal/topology`'s tests already stand real tmux servers up on temporary
sockets, so this gets tested against tmux rather than against a mock of it.

**The mechanism was probed before this spec was signed off**, on the grounds
that tmux accepting a hook name is not the same as tmux firing it — the lesson
of the spawn that looked like a dead Enter key and was a window created for a
command that died instantly. On tmux 3.7b, against a throwaway server:

| Action | Event |
|---|---|
| `copy-mode` | `pid=98002 in_mode=1 pane=%0` |
| `send-keys -X cancel` | `pid=98002 in_mode=0 pane=%0` |
| `copy-mode` | `pid=98002 in_mode=1 pane=%0` |
| `send-keys q` | `pid=98002 in_mode=0 pane=%0` |

So it fires on entering *and* leaving, by the scripted exit and by the real
key, and `#{pane_pid}`, `#{pane_id}` and `#{pane_in_mode}` all expand inside
the hook body — including `in_mode=0` on the leaving edge, which is what lets
one command handle both directions.

One quirk found in the probe, and corrected while implementing: an unfiltered
`show-hooks -g` does not list `pane-mode-changed` at all, even while it is set
and firing, and neither does `show-options -g`. Asked **by name** —
`show-hooks -g pane-mode-changed` — it reports normally, which is how
`internal/tmuxconf` already checks `pane-focus-in`. So the hook is testable
against a live server after all, and is tested that way rather than by
matching the generated fragment text.

The related trap is worth keeping: an *unset* hook asked for by name is
reported as its bare name rather than as nothing, so `hook != ""` is not the
way to assert one was never installed. What says so is that it runs no
harness.

- `internal/topology`: a pane put into `copy-mode` is reported by `Frozen`
  and its neighbours are not; `send-keys -X cancel` clears it. A pid in no
  pane is absent from the map rather than false.
- `internal/topology`: `Approve` against a Frozen pane returns the refusal
  **and the pane is unchanged** — asserting nothing was sent, not merely that
  an error came back, since the failure this fixes is a key that went out and
  landed in the wrong key table.
- `internal/tmuxconf`: the fragment carries the `pane-mode-changed` hook, and
  re-sourcing it leaves one hook rather than two (the property `seenHook`
  already holds by setting rather than appending).
- `internal/dashboard`: a Frozen row draws the mark, a Frozen *and* popup-busy
  row draws both in order, a row that is neither costs the layout nothing; a
  `Froze` event marks and a `Thawed` event clears; a failed sweep leaves the
  previous answer standing; `Attention`, tier order and the strip are
  unchanged by a Frozen row.
`cmd/ganymede` gets no test of its own. `frozen` is a transcription of `seen`
— registry read, `topology.Under`, forward per Session — and `seen` is
untested at that layer too, because the package is `main` and everything in
the command that is worth asserting already lives behind `topology.Under` and
`hooks.Forward`, both tested where they are. Adding the first test file to
`cmd/ganymede` to cover a copy of an untested neighbour is not what this
change should be spending its restructuring on.

## Non-goals

- **The trap itself.** `tmuxconf.findHook` binds `C-]` to `copy-mode` followed
  by a search prompt, and nothing in it ever leaves copy-mode — dismissing the
  prompt strands the pane. That is the cause; this spec is only about seeing
  it. Follow-up.
- **A key to clear it.** No Dashboard action that sends `send-keys -X cancel`.
  Same follow-up.
- **No new session state, no change to Attention**, per above.
- **The `@ganymede-seen` option name**, which now names the binary for three
  bindings rather than one and reads oddly for all but the first. Renaming it
  touches the popup binding too and belongs in its own change.
