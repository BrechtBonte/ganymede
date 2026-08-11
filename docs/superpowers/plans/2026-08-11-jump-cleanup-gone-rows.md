# Clean up a Gone row on jump failure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pressing `⏎` on a Session whose process has already died should remove that row instead of just reporting a jump failure — and the row must not flicker back on the next registry or reconciler snapshot.

**Architecture:** `internal/topology.locate` reuses the `ps` process table it already builds to tell a truly dead pid apart from one merely unplaceable in a pane, returning a new typed `GoneError` only for the dead case. `internal/dashboard.jumpTo` reacts to that error by dropping the row and remembering the pid was confirmed dead in a small local set, filtered against every incoming snapshot until the state model itself catches up.

**Tech Stack:** Go 1.26.2, bubbletea, tmux (real servers in tests), no new dependencies.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-11-jump-cleanup-gone-sessions-design.md`. It is normative; this plan implements it.
- **Branch:** `fix/jump-cleanup-gone-rows`. Never commit to `main`. Already created and checked out.
- **Commits:** use the `atomic-commits` skill for every commit — never run `git commit` directly. Commit steps happen in the main session; never delegate a commit to a subagent.
- **Commit format:** free-form imperative, ≤72-char subject, body explaining *why* when non-obvious. Trailer `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`.
- **Scope is narrow, deliberately.** No changes to `internal/state`, `internal/reconciler`, or `internal/registry` — declined during brainstorming. Do not "improve" the reconciler merge while in the area.
- **Cleanup is row-only.** Do not touch tmux windows/panes left behind by a dead session.
- **House style:** every exported identifier and every non-obvious decision carries a doc comment saying *why*, in the voice of the surrounding file. This codebase's comments justify; they do not narrate.
- **No HTML view of this plan or the spec for this project** — markdown only, per standing project preference (overrides the global default).
- **Run `go test ./... -p 1` before every commit** (the plain `-p 1` avoids two known pre-existing flakes under parallel-package load — `ghostty.TestOpenReportsAnEmulatorThatFailsImmediately` and `topology.TestReopeningLandsOnTheSameHiddenSession` — do not chase them if seen). Also `gofmt -l .` — it must print nothing.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/topology/jump.go` | Modify | `GoneError`; `locate` distinguishes dead from unplaceable |
| `internal/topology/jump_test.go` | Modify | a dead pid gets `GoneError`; an alive-but-unplaceable one still doesn't |
| `internal/dashboard/dashboard.go` | Modify | `Model.forgotten`; `jumpTo` reacts to `GoneError`; `showing` filters and prunes |
| `internal/dashboard/dashboard_test.go` | Modify | row removed and stays removed; plain errors leave the row alone; pruning |

No new files. Two packages, both already `errors`/`topology`-aware on the dashboard side.

---

### Task 1: `internal/topology` — tell Gone apart from merely unplaceable

**Files:**
- Modify: `internal/topology/jump.go:1-55` (imports, new `GoneError`, `locate`)
- Test: `internal/topology/jump_test.go`

**Interfaces:**
- Consumes: existing `paneOf(pid int, panes map[int]string, parents map[int]int) (string, bool)`, `parents() (map[int]int, error)`, both already in `jump.go`.
- Produces: `type GoneError struct{ PID int }` with `func (e GoneError) Error() string`, in package `topology`. `Jump(pid int) error` is unchanged — it already just returns whatever `locate` returns, so it will now sometimes return a `GoneError`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/topology/jump_test.go` (needs a new `"errors"` import alongside the existing ones):

```go
// deadPID hands back a pid that used to be a process and now is not — the
// stand-in for a Session whose Claude process has already ended.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for the throwaway process: %v", err)
	}
	return pid
}

// A pid that no longer names any process at all is the one case worth acting
// on rather than just reporting: the Session is not merely unplaceable, it is
// Gone, and the Dashboard needs to be able to tell the two apart.
func TestJumpToAGoneProcessSaysSo(t *testing.T) {
	h := jumpable(t)
	pid := deadPID(t)

	err := h.Jump(pid)
	if err == nil {
		t.Fatal("Jump to a gone process reported success")
	}
	var gone topology.GoneError
	if !errors.As(err, &gone) || gone.PID != pid {
		t.Errorf("Jump(%d) error = %v, want a GoneError naming %d", pid, err, pid)
	}
	if session, _ := workingClientShows(t, h); session != "service-ai-assistant" {
		t.Errorf("the working client moved to %q on a jump that could not be made", session)
	}
}
```

Extend the existing `TestJumpToAProcessInNoPaneSaysSo` (still-alive, just unplaceable) so it also guards against the two cases being confused:

```go
func TestJumpToAProcessInNoPaneSaysSo(t *testing.T) {
	h := jumpable(t)

	// Our own process: alive, and running in no pane of this tmux server.
	err := h.Jump(os.Getpid())
	if err == nil {
		t.Fatal("Jump to a process in no pane reported success")
	}
	var gone topology.GoneError
	if errors.As(err, &gone) {
		t.Errorf("a live process with no pane was reported Gone: %v", err)
	}
	if session, _ := workingClientShows(t, h); session != "service-ai-assistant" {
		t.Errorf("the working client moved to %q on a jump that could not be made", session)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/topology/... -run 'TestJumpToAGoneProcessSaysSo|TestJumpToAProcessInNoPaneSaysSo' -v -p 1`
Expected: `TestJumpToAGoneProcessSaysSo` fails to compile (`topology.GoneError` does not exist yet). `TestJumpToAProcessInNoPaneSaysSo` also fails to compile for the same reason, since both are in the same package. This is the expected "red" for introducing a new exported type — fixed by Step 3.

- [ ] **Step 3: Write the minimal implementation**

In `internal/topology/jump.go`, add above `locate`:

```go
// GoneError says a Jump target's process has ended — not merely that the
// harness could not place a live process in a pane. locate tells the two
// apart using the same ps table it already builds to find the pane; the
// dashboard needs the distinction to know when a row is safe to clean up.
type GoneError struct{ PID int }

func (e GoneError) Error() string {
	return fmt.Sprintf("process %d has ended", e.PID)
}
```

Change `locate` from:

```go
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
		return "", fmt.Errorf("no tmux pane is running process %d", pid)
	}
	return found, nil
}
```

to:

```go
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
		// Every live process on the machine is a key in parents — reusing it
		// tells Gone apart from merely unplaceable at no extra cost: no new
		// syscall, no duplicating the liveness check registry.running already
		// owns elsewhere.
		if _, alive := parents[pid]; !alive {
			return "", GoneError{PID: pid}
		}
		return "", fmt.Errorf("no tmux pane is running process %d", pid)
	}
	return found, nil
}
```

`Jump` itself needs no change — it already just does `target, err := h.locate(pid); if err != nil { return err }`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/topology/... -run 'TestJumpToAGoneProcessSaysSo|TestJumpToAProcessInNoPaneSaysSo' -v -p 1`
Expected: PASS for both.

- [ ] **Step 5: Run the whole package's tests**

Run: `go test ./internal/topology/... -p 1`
Expected: PASS (`go test ./internal/topology/...` alone is fine here since this is a single package; `-p 1` matters once other packages run alongside it).

- [ ] **Step 6: Commit**

Use the `atomic-commits` skill. This is one logical unit: `GoneError` plus the `locate` change and its tests. Suggested subject: "Tell a dead process apart from an unplaceable one in Jump". Body: explain that `locate` already builds the `ps` table this reuses, and that this is what lets `internal/dashboard` clean up a row rather than just report the failure (landing in the next task).

---

### Task 2: `internal/dashboard` — clean up on Gone, and keep it cleaned up

**Files:**
- Modify: `internal/dashboard/dashboard.go` — imports (`errors`), `Model.forgotten` field (near `pending` at dashboard.go:312), `jumpTo` (dashboard.go:1169), `showing` (dashboard.go:540), new `forget`/`prune`/`withoutForgotten` helpers
- Test: `internal/dashboard/dashboard_test.go`

**Interfaces:**
- Consumes: `topology.GoneError{PID int}` from Task 1 (`errors.As`). Existing `jumps` test fake (`internal/dashboard/dashboard_test.go:19`) — its `err` field is a plain `error`, so tests can set it to a `topology.GoneError{PID: ...}` value directly with no new fake needed.
- Produces: `func (m Model) forget(pid int) Model` (unexported — internal to this package, nothing later depends on its name).

- [ ] **Step 1: Write the failing tests**

Add to `internal/dashboard/dashboard_test.go`:

```go
// A jump that finds the process itself gone — not merely unplaceable — is
// the one case worth acting on: this is the row Brecht sees linger, and
// Enter is the moment he actually notices it.
func TestJumpToAGoneSessionRemovesItsRow(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{err: topology.GoneError{PID: only.PID}}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if strings.Contains(tree(model), "ganymede-78") {
		t.Errorf("the row survived a jump that found its process gone:\n%s", tree(model))
	}
}

// Dropping the row on its own is not enough: the registry ticks every two
// seconds and the reconciler every thirty, and either can still carry the
// same pid if the state model has not caught up. A row that flickered back
// would undo the one thing Enter was just asked to do.
func TestARemovedGoneRowStaysGoneAcrossTheNextSnapshot(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{err: topology.GoneError{PID: only.PID}}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	// The same pid, reported again — a stale reconciler cross-check
	// re-asserting a Session the registry has already dropped.
	model, _ = model.Update(dashboard.Sessions{only})

	if strings.Contains(tree(model), "ganymede-78") {
		t.Errorf("a confirmed-gone row reappeared on the next snapshot:\n%s", tree(model))
	}
}

// A pid the harness forgot cannot be suppressed forever: once the source
// that reported it dead has itself moved on, there is nothing left to guard
// against, and the set must not silently keep growing for the life of the
// Dashboard process.
func TestAForgottenPidIsPrunedOnceItsSourceMovesOn(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{err: topology.GoneError{PID: only.PID}}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	// The registry catches up and stops reporting the pid at all.
	model, _ = model.Update(dashboard.Sessions{})

	// The same pid reappears as a new Session — the one case that would prove
	// the pid was still wrongly suppressed rather than pruned.
	again := live("ganymede-78", "/repos/ganymede", session.Idle)
	model, _ = model.Update(dashboard.Sessions{again})

	if !strings.Contains(tree(model), "ganymede-78") {
		t.Errorf("a pid stayed suppressed after its own source stopped reporting it gone:\n%s", tree(model))
	}
}

// A jump that fails for a reason other than the process being gone — running
// outside tmux, say — must not be mistaken for Gone: the row stays, and only
// the notice changes, exactly as before this change.
func TestAJumpThatCannotBeMadeLeavesTheRowInPlace(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if !strings.Contains(tree(model), "ganymede-78") {
		t.Errorf("a merely-unplaceable jump removed the row:\n%s", tree(model))
	}
}
```

`errors` and `topology` are already imported in this test file (see `dashboard_test.go:4` and `:11`); `dashboard` and `tea` likewise.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/... -run 'TestJumpToAGoneSessionRemovesItsRow|TestARemovedGoneRowStaysGoneAcrossTheNextSnapshot|TestAForgottenPidIsPrunedOnceItsSourceMovesOn|TestAJumpThatCannotBeMadeLeavesTheRowInPlace' -v -p 1`
Expected: `TestAJumpThatCannotBeMadeLeavesTheRowInPlace` PASSes already (today's behaviour already leaves the row alone on a plain error). The other three FAIL: the row is still drawn in the first two (nothing removes it yet) and the third never had anything to prune.

- [ ] **Step 3: Write the minimal implementation**

Add `"errors"` to the import block in `internal/dashboard/dashboard.go` (alongside `"os"`, `"path/filepath"`, etc.).

Add a field to `Model`, next to `pending` (dashboard.go:308-312):

```go
	// forgotten is which pids Jump has confirmed are Gone — the process
	// itself has ended, not merely unplaceable in a pane. Sessions is
	// filtered against it on every arrival so a row Enter just cleaned up
	// does not flicker back on the registry's next tick or a stale
	// reconciler cross-check; an entry is dropped once a real snapshot no
	// longer carries that pid, since there is nothing left to guard against.
	forgotten map[int]struct{}
```

Change `jumpTo` (dashboard.go:1169) from:

```go
func (m Model) jumpTo(s session.Session, moveFocus bool) Model {
	if m.harness.Jumper == nil {
		return m
	}
	if err := m.harness.Jumper.Jump(s.PID); err != nil {
		// A jump that could not be made left you where you were, so the
		// Session has not been seen and its badge stays.
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

to:

```go
func (m Model) jumpTo(s session.Session, moveFocus bool) Model {
	if m.harness.Jumper == nil {
		return m
	}
	if err := m.harness.Jumper.Jump(s.PID); err != nil {
		// Gone is the one jump failure worth acting on rather than just
		// reporting: the process itself has ended, so the row is cleaned up
		// instead of left for you to notice failing again.
		var gone topology.GoneError
		if errors.As(err, &gone) {
			return m.forget(s.PID)
		}
		// A jump that could not be made left you where you were, so the
		// Session has not been seen and its badge stays.
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

// forget drops a Session Jump has confirmed is Gone, and keeps it dropped
// across every snapshot until the source that reported it has moved on too
// — otherwise the very next registry tick or reconciler cross-check would
// put the row right back, undoing the one thing Enter was just asked to do.
func (m Model) forget(pid int) Model {
	if m.forgotten == nil {
		m.forgotten = map[int]struct{}{}
	}
	m.forgotten[pid] = struct{}{}
	m.set = withoutForgotten(m.set, m.forgotten)
	m.notice = "session ended — removed from the dashboard"
	return m.rebuilt()
}

// withoutForgotten drops any Session Jump has confirmed is Gone.
func withoutForgotten(sessions []session.Session, forgotten map[int]struct{}) []session.Session {
	if len(forgotten) == 0 {
		return sessions
	}
	kept := make([]session.Session, 0, len(sessions))
	for _, s := range sessions {
		if _, gone := forgotten[s.PID]; gone {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

// pruneForgotten drops a confirmed-Gone pid once the account that reported
// it Gone has itself moved on — the registry or the reconciler no longer
// naming it — so the set does not grow for the life of the Dashboard.
func pruneForgotten(forgotten map[int]struct{}, sessions []session.Session) map[int]struct{} {
	if len(forgotten) == 0 {
		return forgotten
	}
	present := make(map[int]bool, len(sessions))
	for _, s := range sessions {
		present[s.PID] = true
	}
	kept := make(map[int]struct{}, len(forgotten))
	for pid := range forgotten {
		if present[pid] {
			kept[pid] = struct{}{}
		}
	}
	return kept
}
```

Change `showing` (dashboard.go:540) from:

```go
func (m Model) showing(sessions []session.Session) Model {
	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.set = sessions
	if m.harness.Activity != nil {
```

to:

```go
func (m Model) showing(sessions []session.Session) Model {
	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.forgotten = pruneForgotten(m.forgotten, sessions)
	m.set = withoutForgotten(sessions, m.forgotten)
	if m.harness.Activity != nil {
```

(the rest of `showing` is unchanged — it already reads `sessions` for `Activity.Touch` below this point, which is fine either way since a forgotten Session touching its root's activity timestamp one last time is harmless).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/... -run 'TestJumpToAGoneSessionRemovesItsRow|TestARemovedGoneRowStaysGoneAcrossTheNextSnapshot|TestAForgottenPidIsPrunedOnceItsSourceMovesOn|TestAJumpThatCannotBeMadeLeavesTheRowInPlace' -v -p 1`
Expected: PASS for all four.

- [ ] **Step 5: Run the whole package's tests, then the whole module's**

Run: `go test ./internal/dashboard/... -p 1`
Expected: PASS — in particular `TestAJumpThatCannotBeMadeIsReported` and every other existing jump/notice test must still pass unchanged.

Run: `go test ./... -p 1`
Expected: PASS, modulo the two known pre-existing flakes named in Global Constraints.

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

Use the `atomic-commits` skill. One logical unit: the `forgotten` set, `jumpTo`'s reaction to `GoneError`, `showing`'s filtering, and their tests. Suggested subject: "Clean up a Session's row once Jump finds it Gone". Body: explain the flicker-back risk `forgotten` exists to prevent, and reference that this closes out the spec at `docs/superpowers/specs/2026-08-11-jump-cleanup-gone-sessions-design.md`.

---

## Self-Review

**Spec coverage:** §1 topology distinction → Task 1. §2 dashboard reaction + persistence → Task 2. §Testing's three points → the four tests in Task 2 plus Task 1's two. §Non-goals (state/reconciler/registry untouched, no window cleanup) → respected; nothing in either task touches those packages or tmux windows.

**Placeholder scan:** none — every step has real code, real commands, real expected output.

**Type consistency:** `topology.GoneError{PID int}` (Task 1) is the exact type `errors.As` targets in Task 2's `jumpTo`. `forgotten map[int]struct{}`, `withoutForgotten`, `pruneForgotten` are named consistently between the field declaration, `forget`, and `showing`.
