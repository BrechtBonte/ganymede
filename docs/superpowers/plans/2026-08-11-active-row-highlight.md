# Always Mark the Row the Working Client Is Showing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track which Session's pane the working client is actually showing, apart from the Dashboard's own cursor, and always mark that row on the rail — dimly, the same weight the cursor's row already wears once the Dashboard loses keyboard focus — unless the cursor is already sitting on it.

**Architecture:** A single new `Model.active int` field (a Session PID, zero for none) is set wherever the code already calls `Jumper.Jump` and it succeeds (`jumpTo`, `focusPane`), and cleared wherever the working client stops showing any Session at all (`goTo`, `forget`). `line()`'s session-row rendering gains one extra case between the cursor's own styling and the plain default.

**Tech Stack:** Go, bubbletea/lipgloss (existing `internal/dashboard` package — no new dependencies).

## Global Constraints

- "Active" only ever means a live Session's row. A repo header row opened via `goTo` (Enter on a repo with nothing running, or the picker's Enter) never sets `active`, and `repoLine` gets no new branch.
- No new visual style: reuse the existing `blurredSelectedStyle` unconditionally (not gated on `m.focused`) for the active-but-not-cursor row. The cursor's own row keeps today's `selectedRowStyle()` behavior untouched.
- The SELECTED detail box stays keyed to the cursor alone — no change to `detail()`/`selected()`.
- No auto-scroll to keep the active row in view when the cursor scrolls it off-screen.
- Tests are black-box, in the existing `dashboard_test` package, reusing the existing fakes and helpers already defined across `dashboard_test.go`, `focus_test.go`, `approve_test.go`, `prompt_test.go`, and `picker_test.go` (`jumps`, `focuses`, `approvals`, `prompts`, `opens`, `sidepanel`, `withApprover`, `withPrompter`, `dashboardOn`, `press`, `live`, `drawn`, `rawLineFor`, `styleCodeOf`, `reverseOnly`, `reverseFaint`, `remembering`, `worked`, `detail`). Do not redefine any of these.
- Commit messages: free-form imperative, one focused change per commit, with a `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>` trailer.

---

### Task 1: Track the jumped-to Session as `active` and render it

**Files:**
- Modify: `internal/dashboard/dashboard.go:242` (Model struct — add field)
- Modify: `internal/dashboard/dashboard.go:1188-1212` (`jumpTo`)
- Modify: `internal/dashboard/dashboard.go:1332-1336` (`blurredSelectedStyle` doc comment)
- Modify: `internal/dashboard/dashboard.go:1446-1468` (`line`)
- Create: `internal/dashboard/active_test.go`

**Interfaces:**
- Produces: `Model.active int` — the PID of the Session last shown in the working client's pane (zero = none). `line()`'s session branch now renders `blurredSelectedStyle` for any session row where `r.session.PID == m.active` and the row is not the cursor's.
- Consumes: existing `selectedStyle`, `blurredSelectedStyle`, `selectedRowStyle()`, `row.session.PID`, `jumpTo(s session.Session, moveFocus bool) Model`.

- [ ] **Step 1: Write the failing tests**

Create `internal/dashboard/active_test.go`:

```go
package dashboard_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// Browsing the cursor away from a Session you just jumped to must not erase
// the one mark saying what the working client is actually showing right
// now — only the cursor's own highlight should move on.
func TestTheJumpedToSessionStaysMarkedAfterBrowsingAway(t *testing.T) {
	jumper := &jumps{}
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks it active
	model = press(model, tea.KeyUp)    // back to the repo header row

	line, ok := rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for the session:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("session row = %q, want it still marked dim-reverse as the active row", line)
	}

	header, ok := rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", drawn(model))
	}
	if !strings.HasPrefix(header, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want the cursor's own plain reverse", header)
	}
}

// The guard's own mismatch fires from a background send with no idea what
// you're doing on the Dashboard right now (approve.go's respond): the
// cursor can move on before the answer comes back, and the row jumpTo
// points at must still pick up the mark even though moveFocus is false and
// the cursor never went near it.
func TestTheGuardsApproveMismatchMarksItsRowEvenAfterTheCursorMovedOn(t *testing.T) {
	approver := &approvals{err: errors.New("pane does not show the dialog it was reported waiting on")}
	jumper := &jumps{}
	a := session.Session{PID: 111, ID: "sess-a", Dir: "/repos/service-billing",
		Name: "aaa-blocked", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	b := session.Session{PID: 222, ID: "sess-b", Dir: "/repos/service-billing",
		Name: "bbb-blocked", State: session.Blocked, Reason: "permission: Bash", Since: epoch}
	model := withApprover(approver, jumper, a, b)
	model = press(model, tea.KeyDown) // onto a's row

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y on a Blocked row asked for no guarded send")
	}

	model = press(model, tea.KeyDown) // browse onto b's row before the answer lands

	model, _ = model.Update(cmd())

	line, ok := rawLineFor(model, "aaa-blocked")
	if !ok {
		t.Fatalf("no row for aaa-blocked:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("aaa-blocked row = %q, want it marked as the row the working client is actually showing", line)
	}
}

// A jump that fails outright — the harness could not reach the pane at
// all, as against Gone's "the process itself has ended" — leaves the
// working client showing whatever it already was. The row it failed on
// must not be marked active over that.
func TestAJumpThatFailsDoesNotMarkItsRowActive(t *testing.T) {
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	// Different-length names on the same dir so live()'s PID (len(name) +
	// len(dir)) does not collide between the two — a and b must stay two
	// distinct Sessions for this test to mean anything.
	a := live("aaa-idle", "/repos/ganymede", session.Idle)
	b := live("bbb-idle-2", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, a, b)

	model = press(model, tea.KeyDown)  // onto a's row
	model = press(model, tea.KeyEnter) // jump fails
	model = press(model, tea.KeyDown)  // onto b's row

	line, ok := rawLineFor(model, "aaa-idle")
	if !ok {
		t.Fatalf("no row for aaa-idle:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) || strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("aaa-idle row = %q, want no mark left behind by a jump that failed", line)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/... -run 'TestTheJumpedToSessionStaysMarkedAfterBrowsingAway|TestTheGuardsApproveMismatchMarksItsRowEvenAfterTheCursorMovedOn' -v`

Expected: both FAIL — the session row renders with the plain (non-reverse) style in both cases, since nothing yet tracks or renders an "active" row.

Run: `go test ./internal/dashboard/... -run TestAJumpThatFailsDoesNotMarkItsRowActive -v`

Expected: PASS already — there is no code path yet that could mark any row active, so this one is a true characterization test rather than a red/green pair. It stays in the suite as a guard against a later regression (e.g. an `active` write that isn't gated on the error check).

- [ ] **Step 3: Add the `active` field**

In `internal/dashboard/dashboard.go`, in the `Model` struct, change:

```go
	rows          []row
	cursor        int
	// focused says the dock's keyboard focus is on the Dashboard's own pane
```

to:

```go
	rows          []row
	cursor        int
	// active is the PID of the Session the working client's pane last
	// actually showed, tracked apart from the cursor: the cursor is what
	// you're browsing, this is what's on screen, and jumping there usually
	// but not always keeps them the same row. Zero means no Session has
	// been shown yet.
	active int
	// focused says the dock's keyboard focus is on the Dashboard's own pane
```

- [ ] **Step 4: Set `active` in `jumpTo`**

In `internal/dashboard/dashboard.go`, change `jumpTo`:

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
	// The pane really changed, whether or not keyboard focus followed it:
	// the async guard fallback (moveFocus == false) repoints the working
	// client exactly as much as the direct Enter gesture does.
	m.active = s.PID
	if m.harness.Seen != nil {
		m.harness.Seen(s.ID)
	}
	if moveFocus && m.harness.Focuser != nil {
		_ = m.harness.Focuser.Focus()
	}
	return m
}
```

- [ ] **Step 5: Render the active row and update `blurredSelectedStyle`'s doc comment**

In `internal/dashboard/dashboard.go`, change:

```go
	// The selected row is inverted and otherwise drawn plainly: a state colour
	// nested inside the inversion fights with it.
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	// blurredSelectedStyle is the same inversion, dimmed: what the cursor's row
	// wears while the dock's keyboard focus is over in the working client
	// rather than on the Dashboard.
	blurredSelectedStyle = selectedStyle.Faint(true)
```

to:

```go
	// The selected row is inverted and otherwise drawn plainly: a state colour
	// nested inside the inversion fights with it.
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	// blurredSelectedStyle is the same inversion, dimmed: the weaker of the
	// rail's two marks, for whatever is on screen but is not also where your
	// keystrokes are landing right now — the cursor's own row once the
	// dock's keyboard focus has moved to the working client, and any other
	// row the working client is actually showing while the cursor sits
	// elsewhere.
	blurredSelectedStyle = selectedStyle.Faint(true)
```

Then change `line()`:

```go
	if i == m.cursor {
		return m.selectedRowStyle().Width(m.width).Render(spread(indent+glyph+" "+mark+name, about(r.ticket)+" "+age, m.width))
	}
	return spread(indent+styleOf(r.session.State).Render(glyph)+" "+mark+name,
		ticketStyle(r.ticket).Render(about(r.ticket))+" "+quietStyle.Render(age), m.width)
}
```

to:

```go
	switch {
	case i == m.cursor:
		return m.selectedRowStyle().Width(m.width).Render(spread(indent+glyph+" "+mark+name, about(r.ticket)+" "+age, m.width))
	case r.session.PID == m.active:
		return blurredSelectedStyle.Width(m.width).Render(spread(indent+glyph+" "+mark+name, about(r.ticket)+" "+age, m.width))
	default:
		return spread(indent+styleOf(r.session.State).Render(glyph)+" "+mark+name,
			ticketStyle(r.ticket).Render(about(r.ticket))+" "+quietStyle.Render(age), m.width)
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/... -run 'TestTheJumpedToSessionStaysMarkedAfterBrowsingAway|TestTheGuardsApproveMismatchMarksItsRowEvenAfterTheCursorMovedOn|TestAJumpThatFailsDoesNotMarkItsRowActive' -v`

Expected: PASS

Then run the full package to check nothing else broke: `go test ./internal/dashboard/...`

Expected: PASS (all existing tests still pass — no other test asserts the plain style on a row that these two scenarios now mark active)

- [ ] **Step 7: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/active_test.go
git commit -m "Mark the row the working client is showing, apart from the cursor" \
  -m "The cursor stood in for both what you're browsing and what's on screen; browsing away after a jump, or the guard's async approve mismatch repointing the pane in the background, left the row actually on screen with no mark at all. Model.active now tracks it and line() renders it with the existing dim-reverse style whenever it isn't also the cursor's row." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 2: Track `focusPane`'s jump too

**Files:**
- Modify: `internal/dashboard/dashboard.go:1263-1275` (`focusPane`)
- Modify: `internal/dashboard/active_test.go`

**Interfaces:**
- Consumes: `Model.active` (Task 1), `focusPane(pid int) Model`.
- Produces: `focusPane` now sets `Model.active` on a `Jump` that succeeds.

- [ ] **Step 1: Write the failing test**

Append to `internal/dashboard/active_test.go`:

```go
// The send guard's own mismatch (prompt.go's delivering/dashboard.go's
// `sent` case) reaches the pane through focusPane, not jumpTo, and shares
// the same property: the cursor can move on before the async answer lands,
// and the row it focuses must still pick up the mark.
func TestTheGuardsSendMismatchMarksItsRowEvenAfterTheCursorMovedOn(t *testing.T) {
	prompter := &prompts{err: errors.New("pane does not show an empty input box to send into")}
	jumper := &jumps{}
	// Different-length names on the same dir so live()'s PID (len(name) +
	// len(dir)) does not collide between the two.
	a := live("aaa-idle", "/repos/ganymede", session.Idle)
	b := live("bbb-idle-2", "/repos/ganymede", session.Idle)
	model := withPrompter(prompter, jumper, a, b)
	model = press(model, tea.KeyDown) // onto a's row

	model = sendingKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = typeInto(model, "fix it")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter in the prompt dialog asked for no guarded send")
	}

	model = press(model, tea.KeyDown) // browse onto b's row before the send lands

	model, _ = model.Update(cmd())

	line, ok := rawLineFor(model, "aaa-idle")
	if !ok {
		t.Fatalf("no row for aaa-idle:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("aaa-idle row = %q, want it marked as the row the working client is actually showing", line)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/... -run TestTheGuardsSendMismatchMarksItsRowEvenAfterTheCursorMovedOn -v`

Expected: FAIL — `focusPane` calls `Jump` but discards the result, so `active` is never set and the row renders plain.

- [ ] **Step 3: Set `active` in `focusPane`**

In `internal/dashboard/dashboard.go`, change:

```go
func (m Model) focusPane(pid int) Model {
	if m.harness.Jumper != nil {
		_ = m.harness.Jumper.Jump(pid)
	}
	return m
}
```

to:

```go
func (m Model) focusPane(pid int) Model {
	if m.harness.Jumper != nil && m.harness.Jumper.Jump(pid) == nil {
		m.active = pid
	}
	return m
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/dashboard/... -run TestTheGuardsSendMismatchMarksItsRowEvenAfterTheCursorMovedOn -v`

Expected: PASS

Then run the full package: `go test ./internal/dashboard/...`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/active_test.go
git commit -m "Have focusPane mark its row active on a Jump that lands" \
  -m "focusPane is the same honest fallback jumpTo is for the send, interrupt, end and Takeover guard mismatches — it repoints the working client's pane exactly as much as jumpTo does, so it needs the same mark. The Jump error, previously discarded outright, is now checked so a Jump that fails does not mark a row that was never actually shown." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 3: Clear `active` once no Session is actually on screen

**Files:**
- Modify: `internal/dashboard/dashboard.go:1214-1226` (`forget`)
- Modify: `internal/dashboard/dashboard.go:1277-1299` (`goTo`)
- Modify: `internal/dashboard/active_test.go`

**Interfaces:**
- Consumes: `Model.active` (Task 1), `forget(pid int) Model`, `goTo(root string) Model`.
- Produces: `goTo` and `forget` now clear `Model.active` back to zero when the working client stops showing the Session it named.

- [ ] **Step 1: Write the failing tests**

Append to `internal/dashboard/active_test.go`:

```go
// Opening a bare repo (Enter on a repo header with nothing running, or the
// picker's Enter) points the working client at a directory with no Session
// in it at all, so no row should keep reading as active once that happens.
func TestOpeningABareRepoClearsTheActiveRow(t *testing.T) {
	jumper := &jumps{}
	opener := &opens{}
	state := remembering(t)
	worked(t, state, "/repos/other-repo", time.Now())
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := dashboardOn(dashboard.Harness{Jumper: jumper, Opener: opener, Activity: state}, only)

	model = press(model, tea.KeyDown)  // ganymede's header row onto its Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active
	model = press(model, tea.KeyDown)  // onto the bare other-repo's header row
	model = press(model, tea.KeyEnter) // goTo: opens it

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/other-repo" {
		t.Fatalf("opened %v, want [/repos/other-repo]", opener.dirs)
	}

	line, ok := rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for ganymede-78:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("ganymede-78 row = %q, want the active mark cleared once a bare repo is opened", line)
	}
}

// A pid Jump has confirmed Gone can be handed by the OS to an unrelated
// process later; the registry then reports that as a brand new Session
// that was never jumped to, and it must not inherit the old one's mark.
func TestForgettingAGoneSessionClearsTheActiveMarkForWhoeverReusesItsPid(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active

	jumper.err = topology.GoneError{PID: only.PID}
	model = press(model, tea.KeyEnter) // jump fails Gone: forgets and drops the row

	// The registry catches up and stops reporting the pid at all — the same
	// step TestAForgottenPidIsPrunedOnceItsSourceMovesOn uses. Without it,
	// withoutForgotten would suppress the reused Session below as a stale
	// re-report of the same dead one, and this test would never reach the
	// row it is actually about.
	model, _ = model.Update(dashboard.Sessions{})

	reused := session.Session{PID: only.PID, ID: "different-id", Dir: "/repos/ganymede",
		Name: "ganymede-new", State: session.Idle, Since: epoch}
	model, _ = model.Update(dashboard.Sessions{reused})

	line, ok := rawLineFor(model, "ganymede-new")
	if !ok {
		t.Fatalf("no row for the reused pid's Session:\n%s", drawn(model))
	}
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("ganymede-new row = %q, want a forgotten pid's old active mark not carried onto whoever reuses it", line)
	}
}
```

Add `"time"`, `"github.com/BrechtBonte/ganymede/internal/dashboard"`, and `"github.com/BrechtBonte/ganymede/internal/topology"` to `active_test.go`'s import block, which now reads:

```go
import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/... -run 'TestOpeningABareRepoClearsTheActiveRow|TestForgettingAGoneSessionClearsTheActiveMarkForWhoeverReusesItsPid' -v`

Expected: both FAIL — `goTo` and `forget` never clear `active`, so `ganymede-78`/`ganymede-new`'s row keeps the dim-reverse style in both cases.

- [ ] **Step 3: Clear `active` in `goTo`**

In `internal/dashboard/dashboard.go`, change:

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
```

to:

```go
func (m Model) goTo(root string) Model {
	if m.harness.Opener == nil {
		return m
	}
	if err := m.harness.Opener.Open(root); err != nil {
		m.notice = err.Error()
		return m
	}
	// A bare repo has no Session in it at all, so nothing on the rail
	// should keep reading as the one the working client is showing.
	m.active = 0
	if m.harness.Activity != nil {
```

- [ ] **Step 4: Clear `active` in `forget`**

In `internal/dashboard/dashboard.go`, change:

```go
func (m Model) forget(pid int) Model {
	if m.forgotten == nil {
		m.forgotten = map[int]struct{}{}
	}
	m.forgotten[pid] = struct{}{}
	m.set = withoutForgotten(m.set, m.forgotten)
	m.notice = "session ended — removed from the dashboard"
	return m.rebuilt()
}
```

to:

```go
func (m Model) forget(pid int) Model {
	if m.forgotten == nil {
		m.forgotten = map[int]struct{}{}
	}
	m.forgotten[pid] = struct{}{}
	m.set = withoutForgotten(m.set, m.forgotten)
	m.notice = "session ended — removed from the dashboard"
	if m.active == pid {
		// The pid this named is confirmed gone; if the OS ever hands it to
		// an unrelated process, the registry will report that as a new
		// Session that was never jumped to, and it must not inherit this
		// mark.
		m.active = 0
	}
	return m.rebuilt()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/... -run 'TestOpeningABareRepoClearsTheActiveRow|TestForgettingAGoneSessionClearsTheActiveMarkForWhoeverReusesItsPid' -v`

Expected: PASS

Then run the full package: `go test ./internal/dashboard/...`

Expected: PASS

Then run the whole module to make sure nothing outside the package regressed: `go build ./... && go test ./...`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/active_test.go
git commit -m "Clear the active mark once no Session is on screen to keep it" \
  -m "goTo (a bare repo) and forget (a confirmed-Gone pid) both leave the working client showing something other than the Session active last named — a bare directory, or nothing once the OS hands a forgotten pid to an unrelated process later. Both now reset active to zero rather than let the mark outlive what it described." \
  -m "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```
