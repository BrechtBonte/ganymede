# Enter Moves Focus Into The Working Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pressing Enter on the Dashboard — on a Session row, a repo row, or the picker — moves keyboard focus into the working client automatically, so you no longer need a separate alt+g afterward.

**Architecture:** Add a `Focus` primitive to `topology.Harness` that runs `select-pane` on the dock server (the same move alt+g makes). Expose it to the Dashboard as a new `Focuser` interface, sibling to the existing `Jumper`/`Opener`. Call it from the two *direct* Enter paths (`jumpTo` for Session rows, `goTo` for repo rows and the picker) but not from the async guard-fallback path that shares `jumpTo` — that one must never steal focus from whatever you're doing on the Dashboard when a background event lands.

**Tech Stack:** Go 1.26, bubbletea (TUI), tmux (via `exec.Command` subprocess calls), Go's standard `testing` package with real throwaway tmux servers for the topology package's integration tests.

## Global Constraints

- Every existing test must keep passing unchanged — this is a pure addition, not a behavior change to `Jump` or `Open` themselves.
- `Focuser`, like every other field on `dashboard.Harness`, may be absent (nil) — a Dashboard missing it does less (no auto-focus) and still draws. Guard every call with a nil check.
- Focus errors are best-effort and ignored (`_ = ...Focus()`), matching the codebase's existing treatment of `focusPane`'s own `Jump` call and popup sweeping. Never let a failed focus-move overwrite a notice a successful Jump/Open already left blank.
- `internal/topology` follows a one-action-per-file convention (`jump.go`, `open.go`); the new primitive goes in its own `focus.go`.
- Spec: `docs/superpowers/specs/2026-08-10-dashboard-enter-focus-design.md`.

---

### Task 1: `topology.Harness.Focus` — the dock-level primitive

**Files:**
- Create: `internal/topology/focus.go`
- Test: `internal/topology/focus_test.go`

**Interfaces:**
- Consumes: `Harness.dock()` (existing, unexported, in `internal/topology/harness.go`), `DockSession` constant (existing, `internal/topology/harness.go`).
- Produces: `func (h Harness) Focus() error` — later tasks call this through the `dashboard.Focuser` interface.

This package's tests spin up real throwaway tmux servers (see `testHarness`, `jumpable`, `attachEmulator`, `tmuxOn`, `settles` in `internal/topology/harness_test.go` and `jump_test.go`) rather than mocking `exec.Command`. Follow that pattern — do not introduce a mocking layer.

- [ ] **Step 1: Write the failing test**

Create `internal/topology/focus_test.go`:

```go
package topology_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// activePane is which of the dock's two panes currently has keyboard focus.
func activePane(t *testing.T, h topology.Harness) string {
	t.Helper()
	for _, line := range strings.Split(tmuxOn(t, h.DockSocket, "list-panes", "-t", "=dock",
		"-F", "#{pane_index} #{pane_active}"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "1" {
			return fields[0]
		}
	}
	return ""
}

// Focus is the dock-level move alt+g makes — Enter's own way of finishing
// what Jump or Open already started.
func TestFocusMovesKeyboardFocusToTheWorkingClient(t *testing.T) {
	h := jumpable(t)

	// Away from the working client the dock started on, so Focus has
	// something to do.
	tmuxOn(t, h.DockSocket, "select-pane", "-t", "=dock:0.0")
	if got := activePane(t, h); got != "0" {
		t.Fatalf("active dock pane is %q, want the Dashboard's own pane (0)", got)
	}

	if err := h.Focus(); err != nil {
		t.Fatalf("Focus: %v", err)
	}

	if got := activePane(t, h); got != "1" {
		t.Errorf("active dock pane is %q, want the working client's pane (1)", got)
	}
}
```

`jumpable(t)` (already defined in `internal/topology/jump_test.go`) brings the harness up, attaches a stand-in emulator, and waits for the working client to attach — exactly the precondition this test needs, with no new setup helper required.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/topology/... -run TestFocusMovesKeyboardFocusToTheWorkingClient -v`
Expected: FAIL — compile error, `h.Focus undefined (type topology.Harness has no field or method Focus)`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/topology/focus.go`:

```go
package topology

// Focus moves keyboard focus in the dock to the working client's pane — the
// same move alt+g makes, given automatically once a jump or an open has
// already put something new in front of you.
func (h Harness) Focus() error {
	return h.dock().run("select-pane", "-t", "="+DockSession+":0.1")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/topology/... -run TestFocusMovesKeyboardFocusToTheWorkingClient -v`
Expected: PASS

Then run the whole package to confirm nothing else broke: `go test ./internal/topology/...` — expect PASS (skips are fine if `tmux` is not on `PATH`, matching `testHarness`'s own skip).

- [ ] **Step 5: Commit**

```bash
git add internal/topology/focus.go internal/topology/focus_test.go
git commit -m "Add Harness.Focus, the dock-level move alt+g makes" \
  -m "Enter is about to start calling this once a jump or an open has put something in front of you — landed as its own primitive first, tested directly against a real dock, before anything in the Dashboard depends on it." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

(Use whichever commit message format `~/.claude/memory/commit_format_preference.md` records — this repo's is free-form imperative.)

---

### Task 2: Wire `Focus` into Enter's direct paths

**Files:**
- Modify: `internal/dashboard/dashboard.go` (Jumper/Opener interfaces ~line 64-75, `Harness` struct ~line 149-185, `case answered:` ~line 366-377, `jump`/`jumpTo`/`goTo` ~line 961-1027)
- Test: `internal/dashboard/dashboard_test.go` (new `focuses` fake, two new tests)
- Test: `internal/dashboard/picker_test.go` (one new test)
- Test: `internal/dashboard/approve_test.go` (one new test)

**Interfaces:**
- Consumes: `topology.Harness.Focus() error` (Task 1).
- Produces:
  - `type Focuser interface { Focus() error }` (new, `internal/dashboard/dashboard.go`)
  - `Harness.Focuser Focuser` (new field)
  - `func (m Model) jumpTo(s session.Session, moveFocus bool) Model` (signature change — was `jumpTo(s session.Session)`)
  - `func (m Model) goTo(root string) Model` (signature unchanged, new behavior)
- Task 3 consumes `Focuser` by wiring `topology.Harness` into it from `cmd/ganymede/main.go`.

This is the core behavior change. `jumpTo` is reached from two places today — the direct Enter gesture (`jump()`) and an async guard-fallback (`case answered:`, when the guard could not verify an approve/deny actually reached the pane). Only the direct gesture may move focus; the async fallback fires from a background event and must not yank your keystrokes into a different Session while you're doing something else on the Dashboard. `goTo` has no such async caller — both of its callers (`jump()`'s repo-row branch, and the picker's `m.goTo(chosen)` in `picker.go`) are direct Enter gestures, so it gets `Focus` unconditionally.

The spec also calls out `focusPane` (the fallback behind a `sent` mismatch, Takeover's `endFailed`, and Stop's own fallback) as a path that must keep not moving focus. That function's signature never touches `m.harness.Focuser` at all — there is nothing in this task's diff that could make it start, so no test asserts it. The one path worth a "must not" test is `jumpTo`'s async caller, since that's the call `moveFocus` exists to gate.

- [ ] **Step 1: Write the failing tests**

In `internal/dashboard/dashboard_test.go`, add the `focuses` fake near the existing `jumps` fake (top of file, after the `jumps` struct and before `sidepanel`):

```go
// focuses records every time the Dashboard moved keyboard focus into the
// working client, standing in for the dock's own select-pane.
type focuses struct {
	n   int
	err error
}

func (f *focuses) Focus() error {
	f.n++
	return f.err
}
```

Then add this test, right after `TestEnterJumpsToTheSelectedSession`:

```go
// Enter takes you all the way in: once the working client is pointed at the
// Session, the keyboard follows it there — no separate alt+g.
func TestEnterOnASessionRowFocusesTheWorkingClient(t *testing.T) {
	jumper := &jumps{}
	focuser := &focuses{}
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{only})

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after Enter on a Session row", focuser.n)
	}
}

// Enter on a repo row with nothing running yet makes the same promise: once
// Open has brought the repo's Session up, the keyboard follows it there too.
func TestEnterOnARepoRowFocusesTheWorkingClient(t *testing.T) {
	opener := &opens{}
	focuser := &focuses{}
	model := onARepo(t, dashboard.Harness{Opener: opener, Focuser: focuser}, "/repos/service-billing")

	model = press(model, tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/service-billing" {
		t.Fatalf("opened %v, want /repos/service-billing", opener.dirs)
	}
	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after Enter on a repo row", focuser.n)
	}
}
```

In `internal/dashboard/picker_test.go`, add this test right after `TestPickingARepoOpensItAndPutsItOnTheDashboard`:

```go
// Picking a repo is Enter too: the same Focus that follows a jump follows a
// pick, so choosing a repo out of the picker lands you in it as well.
func TestPickingARepoFocusesTheWorkingClient(t *testing.T) {
	focuser := &focuses{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    &opens{},
		Focuser:   focuser,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model = press(typing(model, "gany"), tea.KeyEnter)

	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after picking a repo", focuser.n)
	}
}
```

In `internal/dashboard/approve_test.go`, add this test right after `TestApproveTheGuardCouldNotVerifyFocusesThePane`:

```go
// jumpTo is shared with the direct Enter gesture, but the guard's own
// mismatch fires from a background send with no idea what you're doing on
// the Dashboard right now — it must show the pane without also stealing
// your keyboard away from it.
func TestApproveTheGuardCouldNotVerifyDoesNotMoveFocus(t *testing.T) {
	approver := &approvals{err: errors.New("pane does not show the dialog it was reported waiting on")}
	jumper := &jumps{}
	focuser := &focuses{}
	s := session.Session{PID: 4242, ID: "sess-1", Dir: "/repos/service-billing",
		Name: "FIRE-2841-paging", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Approver: approver, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{s})
	model = press(model, tea.KeyDown)

	model = answering(model, "y")

	if len(jumper.pids) != 1 {
		t.Fatalf("jumped to %v, want the guard's own mismatch to still focus the pane", jumper.pids)
	}
	if focuser.n != 0 {
		t.Errorf("Focus called %d times, want the async fallback to leave keyboard focus alone", focuser.n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/... -run 'FocusesTheWorkingClient|DoesNotMoveFocus' -v`
Expected: FAIL — compile errors: `unknown field Focuser in struct literal of type dashboard.Harness` (in each of the three call sites above).

- [ ] **Step 3: Write the minimal implementation**

In `internal/dashboard/dashboard.go`, add the `Focuser` interface right after `Opener`:

```go
// Opener puts a repo in front of you: the working client moves to that repo's
// Session, brought up if nothing is running there yet. It is what Enter on a
// repo's row does, and what the picker does with the repo you chose.
type Opener interface {
	Open(dir string) error
}

// Focuser moves keyboard focus to the working client's pane — the dock's own
// alt+g, given automatically once Enter has already put something in front
// of you.
type Focuser interface {
	Focus() error
}
```

Add the `Focuser` field to the `Harness` struct, next to `Jumper`/`Opener`:

```go
type Harness struct {
	// Jumper puts a Session in front of you, and Opener a repo.
	Jumper Jumper
	Opener Opener
	// Focuser moves keyboard focus into the working client once Jumper or
	// Opener has already put something new there — the alt+g Enter used to
	// still leave you needing.
	Focuser Focuser
	// Strip carries the Attention counts to the working client's status line.
	Strip Strip
```

Change `jumpTo`'s signature and body:

```go
// jumpTo puts s in front of you: the pane it is running in, and the moment
// it counts as seen. It is jump's own work over the selected row, factored
// out so the guard's asynchronous fallback (§7.2) can focus the exact
// Session it tried to answer, by the Session itself rather than whichever
// row the cursor has since moved to.
//
// moveFocus is true only for the direct Enter gesture in jump(): the async
// fallback shares this same call but must never steal keyboard focus from
// whatever you're doing on the Dashboard when it fires.
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

Update `jump()`'s call site to pass `true`:

```go
func (m Model) jump() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	selected := m.rows[m.cursor]
	if selected.session == nil {
		return m.goTo(selected.root)
	}
	return m.jumpTo(*selected.session, true)
}
```

Update the async fallback's call site (inside `Update`'s `case answered:`) to pass `false`:

```go
	case answered:
		delete(m.pending, msg.session.PID)
		if msg.err != nil {
			// The guard's own mismatch: the gate passed but tmux could not verify
			// the send. Said before the fallback jump, which is silent on success
			// and would otherwise overwrite it with nothing — a y or n that did
			// not go through has to say why, not just leave you looking at the
			// pane wondering.
			m.notice = msg.err.Error()
			return m.jumpTo(msg.session, false), nil
		}
		return m, nil
```

Finally, add the `Focus` call to `goTo`:

```go
func (m Model) goTo(root string) Model {
	if m.harness.Opener == nil {
		return m
	}
	if err := m.harness.Opener.Open(root); err != nil {
		m.notice = err.Error()
		return m
	}
	if m.harness.Activity != nil {
		if err := m.harness.Activity.Touch(root, time.Now()); err != nil {
			m.notice = err.Error()
		}
	}
	if m.harness.Focuser != nil {
		_ = m.harness.Focuser.Focus()
	}
	return m.rebuilt().selecting(root)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/... -v`
Expected: PASS — every test in the package, including the four new ones and every pre-existing one (`jumpTo`'s two call sites and `goTo`'s behavior are the only production changes, and both are covered).

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go \
  internal/dashboard/picker_test.go internal/dashboard/approve_test.go
git commit -m "Move keyboard focus into the working client on Enter" \
  -m "Enter already pointed the working client at the right Session or repo; it left you typing into the Dashboard until a separate alt+g. Session rows, repo rows, and the picker all gain the same automatic focus move — but jumpTo's async guard-fallback caller keeps its hands off, since it fires from a background event that must never steal your keystrokes out from under you." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 3: Wire the real `Focuser` into the running harness

**Files:**
- Modify: `cmd/ganymede/main.go` (the `hands := dashboard.Harness{...}` literal)

**Interfaces:**
- Consumes: `topology.Harness.Focus` (Task 1, already satisfies `dashboard.Focuser` structurally — the same way it already satisfies `Jumper`/`Opener`), `dashboard.Harness.Focuser` field (Task 2).
- Produces: nothing further downstream — this is the last wire.

`cmd/ganymede` has no test file today (confirmed: no `main_test.go`), so this task's verification is a successful build rather than a unit test.

- [ ] **Step 1: Add `Focuser` to the harness wiring**

In `cmd/ganymede/main.go`, find:

```go
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Strip: harness, Spawner: harness, Popups: harness, Approver: harness,
		Prompter: harness, Stopper: harness, Seen: model.Seen, Tickets: tickets,
	}
```

Change to:

```go
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Focuser: harness, Strip: harness, Spawner: harness, Popups: harness, Approver: harness,
		Prompter: harness, Stopper: harness, Seen: model.Seen, Tickets: tickets,
	}
```

- [ ] **Step 2: Verify the build**

Run: `go build ./...`
Expected: exits 0, no errors. `topology.Harness` already has a `Focus() error` method from Task 1, so it satisfies `dashboard.Focuser` with no further code needed.

Run the full test suite once more as a final check: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/ganymede/main.go
git commit -m "Wire the harness's Focus into the running Dashboard" \
  -m "The last step: Focuser was defined and consumed inside the dashboard package since Task 2, but nothing supplied a real implementation until now — this is what makes Enter's auto-focus actually happen outside of tests." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```
