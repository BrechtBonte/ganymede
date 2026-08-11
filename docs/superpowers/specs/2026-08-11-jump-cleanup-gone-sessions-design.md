# Clean up a Gone row on jump failure

## Problem

Brecht's words: "when trying to enter a gone thread it should check if the
thread still exists and clean it up if not." He sees this regularly, not as a
one-off.

A Session row stays on the Dashboard after its Claude process has died.
Pressing `⏎` on it tries to jump to a pane that is no longer there.
`Harness.Jump` already fails cleanly — `locate` (`internal/topology/jump.go:41`)
returns `"no tmux pane is running process %d"`, and `jumpTo`
(`internal/dashboard/dashboard.go:1169`) puts that in `m.notice` and leaves you
where you were. The failure is reported; nothing acts on it.

### Why the row is still there to jump into

The registry's own liveness filter is not the bug: `Registry.Read`
(`internal/registry/registry.go:54`) drops any record whose pid is dead on
every read, and the watch (`internal/registry/watch.go`) has re-read the
registry every 2 seconds since the file was first written — not only on
fsnotify events. A dead pid drops out of the registry's own account within
that window.

What keeps a genuinely dead row on screen for longer than that is one layer up,
in the state model's merge (`internal/state/state.go`). `reconciled()`
(state.go:289) lays the reconciler's half-minute cross-check
(`claude agents --json`) over the registry's account, and for a pid the
reconciler still lists that the *current* registry snapshot no longer has, it
appends that row back in — unchecked. Neither the reconciler
(`internal/reconciler/reconciler.go`) nor `reconciled()` ever calls a liveness
check on that path; the code says so directly: *"nothing is
liveness-checking them either, and one whose Session has ended stays up until
the next cross-check drops it."* So a dead session's row can be re-inserted as
a ghost by a stale reconciler snapshot and survive until the *next* cross-check
also stops reporting it — bounded by how promptly `claude agents --json`
itself reflects the process having ended, not by anything in this codebase.

**Scope decision, made explicit:** fixing that merge gap in `internal/state`
would be the root-cause fix and was considered, but Brecht chose to go narrow
— exactly what he asked for, and nothing in `internal/state`,
`internal/reconciler`, or `internal/registry` changes. This means rows sourced
from a reconciler ghost can still reappear on their own, on the reconciler's
cadence, if never jumped into — that is accepted, not fixed, by this change.

## Design

Two components, both self-contained: `internal/topology` learns to tell
"the process is gone" apart from "the process is alive but I can't place it in
a pane," and `internal/dashboard` acts on that distinction and keeps the
cleanup from flickering back.

### 1. `internal/topology`: distinguishing Gone from merely unplaceable

`locate` (jump.go:41) already builds a `parents` map from `ps -Ao pid=,ppid=`
to walk a pid up to the pane tmux started. Every live process on the machine
is a key in that map. Reusing it costs nothing extra — no new syscall, no
duplicating the liveness check `registry.running` already owns elsewhere —
and answers exactly the question this needs: is the pid still there at all,
independent of whether tmux can place it in a pane.

```go
// GoneError says a Jump target's process has ended — not merely that the
// harness could not place a live process in a pane.
type GoneError struct{ PID int }

func (e GoneError) Error() string {
	return fmt.Sprintf("process %d has ended", e.PID)
}

func (h Harness) locate(pid int) (string, error) {
	panes, _, err := h.panes()
	if err != nil {
		return "", err
	}
	parents, err := parents()
	if err != nil {
		return "", err
	}
	found, ok := paneOf(pid, panes, parents)
	if !ok {
		if _, alive := parents[pid]; !alive {
			return "", GoneError{PID: pid}
		}
		return "", fmt.Errorf("no tmux pane is running process %d", pid)
	}
	return found, nil
}
```

A pid present in `parents` but still unplaceable (started outside tmux, or a
`ps` snapshot inconsistency) keeps today's plain error. Only an absent pid —
the process itself is gone — is worth treating as Gone. `Jump` itself is
unchanged; it already just propagates whatever `locate` returns.

### 2. `internal/dashboard`: reacting to it, and making the cleanup stick

`jumpTo` (dashboard.go:1169), on `Jump` returning a `topology.GoneError`
(`errors.As`), does two things instead of the current plain
`m.notice = err.Error()`:

- adds the pid to a new field on `Model`, `forgotten map[int]struct{}`
  (alongside `pending`, `roots`, etc. in the struct at dashboard.go:236),
  declared and grown only here;
- sets a plain notice ("session ended") and returns, rather than leaving the
  raw locate error on screen.

Removing the row from the *current* redraw is not enough on its own: the next
`Sessions` message — the registry ticks every 2 seconds, the reconciler every
30 — can still carry that same pid if the state model hasn't caught up (the
whole reason this row was jumpable in the first place). Without remembering
the pid was confirmed dead, the row would flicker back within a couple of
seconds and undo the cleanup. So the `Sessions` case in `Update`
(dashboard.go:422, via `showing()` at dashboard.go:540) filters `sessions`
against `forgotten` before it becomes `m.set` — every subsequent snapshot,
fast or slow, keeps that pid off the tree.

`forgotten` entries are dropped the moment a real incoming snapshot itself no
longer contains that pid — once the registry or the reconciler has genuinely
caught up, there is nothing left to suppress, so the set does not grow for the
life of the Dashboard process.

This is the only place a "gone" pid is remembered. `internal/state`'s merge
semantics, `internal/reconciler`, and `internal/registry` are untouched — the
suppression is a dashboard-local reaction to a jump you actually tried, not a
change to what the state model believes.

## Testing

`internal/topology`: extend the existing `testHarness`/`tmuxOn` fixtures with
a case where a pid is absent from the `ps` snapshot (`GoneError` expected) and
a case where it's present but paneless (today's plain error, unchanged).

`internal/dashboard`: using the existing `sidepanel`/`live`/`press` helpers,
drive a jump against a stub `Jumper` whose `Jump` returns `topology.GoneError`
for a given pid; assert the row disappears immediately, and assert it stays
gone across a subsequent `Sessions` message that still carries that pid.
Assert a plain unplaceable error (not `GoneError`) leaves the row exactly as
it is today — notice set, row untouched.

## Non-goals

- **The `internal/state` merge fix.** Considered, not built. Rows can still
  reappear on their own reconciler cadence if never jumped into — see
  Problem's scope decision.
- **Killing an orphaned tmux window.** Cleanup is row-only, per CONTEXT.md's
  Gone: "the row disappears." Nothing here touches the tmux window a dead
  session may have left behind.
- **Any change to `internal/registry` or `internal/reconciler`.**
