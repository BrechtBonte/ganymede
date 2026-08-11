# Frozen panes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a tmux pane that is holding a mode over its live view visible on the dashboard, and stop the guard sending keys into one.

**Architecture:** `topology` learns to ask tmux `#{pane_in_mode}` — once per sweep for the rail, once per target for the guard. A `pane-mode-changed` tmux hook gives the edges through `ganymede frozen` and the existing event socket; the half-minute clock cross-checks. The dashboard holds the answer in its own map, keyed by Claude session id, exactly as it already holds cautions and popup statuses. `session.State` gains nothing.

**Tech Stack:** Go 1.26.2, bubbletea/lipgloss, tmux 3.7b, no new dependencies.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-11-frozen-panes-design.md`. It is normative; this plan implements it.
- **Branch:** `surface-frozen-panes`. Never commit to `main`.
- **Commit format:** free-form imperative, ≤72-char subject, body explaining *why* when non-obvious. Trailer `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- **Term:** the concept is **Frozen**, the mark is `❄` (U+2744). Never "stuck", "hung", "copy-mode", or "scrolled" in user-facing text or doc comments.
- **`session.State` gains no sixth value.** Frozen is orthogonal and must coexist with all five.
- **Frozen never affects** `Attention`, `tier`, `moreUrgent`, the tmux attention strip, or the notifier.
- **House style:** every exported identifier and every non-obvious decision carries a doc comment saying *why*, in the voice of the surrounding file. This codebase's comments justify; they do not narrate.
- **Run `go test ./...` before every commit.** Also `gofmt -l .` — it must print nothing.

---

## Deviation from the spec, decided while planning

The spec says the edge arrives as a `hooks.Event` and the dashboard holds the map. It does not say how the event *reaches* the dashboard. Reading the wiring settled it:

`hooks.Listen` produces one channel, consumed by `state.Model.Watch`. A Go channel has one reader — `cmd/ganymede/main.go` already says so where it calls `fanned` for exactly this reason. So the event stream is fanned in two: one tap to the state model (which ignores the new kinds — `state.apply` has `default: return`), one tap to a goroutine that translates into bubbletea messages via `program.Send`.

`program.Send` rather than a second channel argument to `dashboard.New`: `New(sessions, harness)` has ~15 test call sites passing `nil`, and a third parameter would be pure churn in files this change has no business touching.

Frozen deliberately does **not** go through `state.Model`. That package merges three sources under rules about which one wins on *what Claude is doing*; a tmux fact has no place in that adjudication.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/topology/jump.go` | Modify | `panes()` also reports which panes hold a mode; new `Frozen(pids)` |
| `internal/topology/guard.go` | Modify | `modeHeld(target)`; refuse before sending |
| `internal/topology/jump_test.go` | Modify | `Frozen` against a real tmux server |
| `internal/topology/guard_test.go` | Modify | the guard refuses and sends nothing |
| `internal/tmuxconf/tmuxconf.go` | Modify | `frozenHook` fragment |
| `internal/tmuxconf/tmuxconf_test.go` | Modify | fragment carries the hook |
| `internal/hooks/hooks.go` | Modify | `Froze`/`Thawed` kinds, `FrozenPayload`, parse |
| `internal/hooks/hooks_test.go` | Modify | payload round-trip |
| `internal/dashboard/dashboard.go` | Modify | mark, map, messages, sweep, detail line |
| `internal/dashboard/rows.go` | Modify | `row.frozen`, `answers.frozen` |
| `internal/dashboard/frozen_test.go` | Create | every dashboard-side behaviour |
| `cmd/ganymede/main.go` | Modify | `frozen` subcommand, generic `fanned`, wiring |
| `CONTEXT.md` | Modify | the term |
| `docs/ARCHITECTURE.md` | Modify | entry point, guard step, owned hooks, rail |

---

### Task 1: `panes()` reports which panes hold a mode

**Files:**
- Modify: `internal/topology/jump.go:131-152` (`panes`), `:41-55` (`locate`)
- Test: `internal/topology/jump_test.go`

**Interfaces:**
- Consumes: existing `paneOf(pid int, panes map[int]string, parents map[int]int) (string, bool)`, `parents() (map[int]int, error)`, `Harness.sessions() server`.
- Produces: `func (h Harness) panes() (map[int]string, map[string]bool, error)` — pane-pid→pane-id, and pane-id→holding-a-mode. `func (h Harness) Frozen(pids []int) (map[int]bool, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/topology/jump_test.go`:

```go
// idlePane starts a tmux session doing nothing, standing in for a Session's
// own pane with no mode held over it.
func idlePane(t *testing.T, h topology.Harness, name string) int {
	t.Helper()
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", name, "sleep", "300")
	return panePIDInSession(t, h.Socket, name)
}

// A pane in copy-mode is showing a held view: the program underneath goes on
// writing to its screen, and none of it reaches the client. That is what the
// rail has to be able to say, and tmux answers it with pane_in_mode.
func TestFrozenReportsOnlyThePaneHoldingAMode(t *testing.T) {
	h := jumpable(t)
	held := idlePane(t, h, "held")
	live := idlePane(t, h, "live")

	tmuxOn(t, h.Socket, "copy-mode", "-t", "held")

	frozen, err := h.Frozen([]int{held, live})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if !frozen[held] {
		t.Errorf("the pane in copy-mode is not reported frozen: %v", frozen)
	}
	if frozen[live] {
		t.Errorf("a pane showing its live view is reported frozen: %v", frozen)
	}
}

// Leaving the mode is as much of an answer as entering it: a mark that only
// ever went on would be worse than no mark at all.
func TestFrozenClearsWhenTheModeIsCancelled(t *testing.T) {
	h := jumpable(t)
	pid := idlePane(t, h, "held")
	tmuxOn(t, h.Socket, "copy-mode", "-t", "held")
	tmuxOn(t, h.Socket, "send-keys", "-X", "-t", "held", "cancel")

	frozen, err := h.Frozen([]int{pid})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if frozen[pid] {
		t.Error("a pane whose mode was cancelled is still reported frozen")
	}
}

// A Session in no pane at all — started outside tmux, or gone since the
// registry was read — is absent rather than false. The two are different
// answers, and only one of them is true.
func TestFrozenLeavesOutAProcessInNoPane(t *testing.T) {
	h := jumpable(t)
	frozen, err := h.Frozen([]int{os.Getpid()})
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if _, answered := frozen[os.Getpid()]; answered {
		t.Errorf("a process in no pane was given an answer: %v", frozen)
	}
}
```

If `jumpable` does not exist in `jump_test.go`, use whatever helper that file already uses to build a `topology.Harness` on a throwaway socket (the same one its existing `Jump` tests call). Do not add a second one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/topology/ -run TestFrozen -v`
Expected: FAIL — `h.Frozen undefined (type topology.Harness has no field or method Frozen)`.

- [ ] **Step 3: Widen `panes()`**

In `internal/topology/jump.go`, replace `panes()`:

```go
// panes maps the process tmux started in each pane to that pane's id, and
// says which of those panes is holding a mode over its live view.
//
// The two answers come off one list-panes because they are asked of the same
// panes at the same moment: a pane whose mode was entered between two calls
// would otherwise be located by the first and judged by the second.
func (h Harness) panes() (map[int]string, map[string]bool, error) {
	out, err := exec.Command("tmux", h.sessions().args("list-panes", "-a",
		"-F", "#{pane_pid} #{pane_id} #{pane_in_mode}")...).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list the panes: %w", err)
	}

	panes := map[int]string{}
	held := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		panes[pid] = fields[1]
		held[fields[1]] = fields[2] == "1"
	}
	return panes, held, nil
}
```

And in `locate`, take the extra return:

```go
	panes, _, err := h.panes()
```

- [ ] **Step 4: Add `Frozen`**

Append to `internal/topology/jump.go`:

```go
// Frozen says which of pids are running in a pane holding a mode over its
// live view — a pane showing a held picture of the Session rather than the
// Session, and sending every keystroke to the mode instead of to Claude.
//
// It resolves each pid the same way locate does, and for the same reason:
// tmux knows only the process it started in the pane, and a Session is that
// process's descendant however many shells down.
//
// A pid the harness cannot place in a pane is left out of the map rather than
// answered false. A Session running outside tmux has no pane whose view could
// be held, and saying "not frozen" would be claiming to have looked.
func (h Harness) Frozen(pids []int) (map[int]bool, error) {
	panes, held, err := h.panes()
	if err != nil {
		return nil, err
	}
	parents, err := parents()
	if err != nil {
		return nil, err
	}

	frozen := make(map[int]bool, len(pids))
	for _, pid := range pids {
		pane, ok := paneOf(pid, panes, parents)
		if !ok {
			continue
		}
		frozen[pid] = held[pane]
	}
	return frozen, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/topology/ -run TestFrozen -v`
Expected: PASS, all three.

Then `go test ./...` — `locate`'s callers (`Jump`, `answer`) must still compile and pass.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go test ./...
git add internal/topology/jump.go internal/topology/jump_test.go
git commit -m "Ask tmux which panes are holding a mode" -m "A pane in a mode shows a held view and sends keys to the mode's key table, and the harness had no way to know. panes() already lists every Session pane for locate; reading pane_in_mode off the same call answers both questions about the same panes at the same moment." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The guard refuses a Frozen pane

**Files:**
- Modify: `internal/topology/guard.go:58-79` (`answer`)
- Test: `internal/topology/guard_test.go`

**Interfaces:**
- Consumes: `Harness.locate`, `server.output(args ...string) (string, error)` (returns tmux's stdout **untrimmed**).
- Produces: `func (h Harness) modeHeld(target string) (bool, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/topology/guard_test.go`:

```go
// A pane holding a mode takes every keystroke into the mode's own key table,
// so a guarded send lands nowhere near the dialog. capture-pane does not say
// so — it returns the live screen, which still shows the dialog — so the pane
// passes the content check and the key goes out anyway, to be reported half a
// second later as a dialog that did not move. The guard asks first instead.
func TestApproveRefusesAFrozenPaneAndSendsNothing(t *testing.T) {
	h := guardable(t)
	pid, keylog := dialogPane(t, h, true)
	// dialogPane names its session after the test; freeze that pane.
	tmuxOn(t, h.Socket, "copy-mode", "-t", "dialog-"+strings.ReplaceAll(t.Name(), "/", "-"))

	err := h.Approve(pid, "permission: Bash")
	if err == nil {
		t.Fatal("Approve reported success against a frozen pane")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("Approve refused with %q, which does not say the pane is frozen", err)
	}
	if _, err := os.Stat(keylog); err == nil {
		t.Error("Approve sent a key into a frozen pane")
	}
}
```

The `os.Stat` assertion is the point of this test. An error return alone would also come back from a guard that sent the key and then failed the settle check — the failure being fixed is a key that *went out*.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/topology/ -run TestApproveRefusesAFrozen -v`
Expected: FAIL — the key *is* logged, and the error (if any) is about the dialog still being there rather than about a frozen pane.

- [ ] **Step 3: Add `modeHeld` and the check**

Append to `internal/topology/guard.go`:

```go
// modeHeld says the pane is holding a mode over its live view, which is what
// makes a send-keys land in the mode's own key table instead of in the
// Session.
//
// It asks about the one pane rather than reusing Frozen, which asks about
// every pane at once: the guard already has the pane id, and the narrower
// question is both cheaper and less racy. It is named apart from Frozen
// rather than case-shifted from it, so the two are told apart at every call
// site.
func (h Harness) modeHeld(target string) (bool, error) {
	out, err := h.sessions().output("display-message", "-p", "-t", target, "#{pane_in_mode}")
	if err != nil {
		return false, fmt.Errorf("ask whether the Session's pane is frozen: %w", err)
	}
	return strings.TrimSpace(out) == "1", nil
}
```

In `answer`, immediately after `locate` and **before** the first `capturePane`:

```go
	target, err := h.locate(pid)
	if err != nil {
		return err
	}
	// Before the content check, not after it: a frozen pane still shows the
	// dialog to capture-pane, so the content check passes and the key goes
	// into the mode instead. This is the cheaper of the two questions and the
	// one that decides whether the other is worth asking.
	held, err := h.modeHeld(target)
	if err != nil {
		return err
	}
	if held {
		return fmt.Errorf("pane %s is frozen: it is showing a held view, not the live Session", target)
	}
	before, err := h.capturePane(target)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/topology/ -v`
Expected: PASS, including the existing `TestApproveSendsYAndTheDialogClears` and `TestDenySendsEscape` — an unfrozen pane must be unaffected.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/topology/guard.go internal/topology/guard_test.go
git commit -m "Refuse to send keys into a frozen pane" -m "capture-pane returns the live screen, not the held view, so a frozen pane passed the guard's content check and the keystroke went into the mode's key table. The guard then reported that the dialog had not moved, which was true and useless: the dialog never got the key." -m "The check goes before the capture because it is the cheaper question and it decides whether the other is worth asking. Refusal behaves like every other guard mismatch: nothing is sent, and the Dashboard falls back to focusing the pane." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The tmux fragment carries the `pane-mode-changed` hook

**Files:**
- Modify: `internal/tmuxconf/tmuxconf.go:55-58` (near `seenHook`), `:147-152` (`fragment`)
- Test: `internal/tmuxconf/tmuxconf_test.go`

**Interfaces:**
- Consumes: `Layout.Command`, `quoteForOption(path string) string`, the `@ganymede-seen` option set by `seenHook`.
- Produces: `const frozenHook` (a `%s`-free string appended by `fragment`).

**Note before you start:** `pane-mode-changed` is missing from an *unfiltered* `show-hooks -g` even while set and firing, but `show-hooks -g pane-mode-changed` reports it normally — which is how this package already checks `pane-focus-in`. Test it against a live server. Beware only that an *unset* hook asked for by name comes back as its bare name, not as an empty string.

- [ ] **Step 1: Write the failing test**

Add to `internal/tmuxconf/tmuxconf_test.go`:

```go
// The edge that makes the Frozen mark quick. It reads the harness's path out
// of the same option seenHook sets, so the fragment names its binary once.
func TestTheFragmentReportsAPaneEnteringAndLeavingAMode(t *testing.T) {
	body := fragmentFor(t, "/opt/ganymede/bin/ganymede")

	if !strings.Contains(body, "set-hook -g pane-mode-changed") {
		t.Errorf("the fragment installs no pane-mode-changed hook:\n%s", body)
	}
	if !strings.Contains(body, `#{q:@ganymede-seen} frozen #{pane_pid} #{pane_in_mode}`) {
		t.Errorf("the hook does not report the pane and its mode:\n%s", body)
	}
}

// A Layout that cannot say where the harness lives installs no hook that runs
// it — the same trade seenHook already makes.
func TestAFragmentWithNoCommandReportsNoModeChanges(t *testing.T) {
	body := fragmentFor(t, "")

	if strings.Contains(body, "pane-mode-changed") {
		t.Errorf("a fragment with no command still installs the hook:\n%s", body)
	}
}
```

`fragmentFor` is whatever this file already uses to render a fragment for a `Layout` (it may be an inline `Install` + `os.ReadFile`). Reuse it; do not add a second helper. If none exists, write the two tests against `Install` into `t.TempDir()` and read the fragment back.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tmuxconf/ -run TestTheFragmentReports -v` and `-run TestAFragmentWithNoCommand -v`
Expected: the first FAILs (no such hook), the second PASSes vacuously.

- [ ] **Step 3: Add the fragment**

Add to `internal/tmuxconf/tmuxconf.go`, directly after `seenHook`:

```go
// frozenHook is what tells the harness a pane has started or stopped holding
// a mode over its live view, which is what puts the Frozen mark on the rail
// and takes it off again.
//
// pane-mode-changed fires on entering and on leaving, and #{pane_in_mode}
// reads 0 on the leaving edge — so one command covers both directions and the
// mark clears the moment you press q, rather than waiting for the half-minute
// cross-check to notice.
//
// Like seenHook it reads the harness's path out of @ganymede-seen rather than
// carrying a second copy, leaves #{pane_pid} literal so it names the pane the
// mode changed in rather than whichever pane this config was loaded from, and
// takes -b so tmux is free the moment the command has started.
//
// It is set rather than appended, so re-sourcing this fragment onto a server
// that already had it leaves one hook rather than two. That makes it the
// second global tmux hook the harness owns: a pane-mode-changed hook of your
// own would be replaced by it.
const frozenHook = `
set-hook -g pane-mode-changed 'run-shell -b "#{q:@ganymede-seen} frozen #{pane_pid} #{pane_in_mode}"'
`
```

And in `fragment`, append it to the branch that has a command — it is useless without one:

```go
func fragment(l Layout) string {
	if l.Command == "" {
		return settings + strip + findHook
	}
	return settings + strip + findHook + fmt.Sprintf(seenHook, quoteForOption(l.Command)) + popupHook + frozenHook
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tmuxconf/ -v`
Expected: PASS. Existing fragment tests may assert exact content — update them to expect the new line if they do.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/tmuxconf/tmuxconf.go internal/tmuxconf/tmuxconf_test.go
git commit -m "Report a pane entering or leaving a tmux mode" -m "pane-mode-changed fires on both edges and pane_in_mode reads 0 on the leaving one, so a single hook both raises the Frozen mark and clears it. Without the edge the mark would wait on the half-minute cross-check, which is a long time to stare at a screen that looks dead." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `hooks` learns Froze and Thawed

**Files:**
- Modify: `internal/hooks/hooks.go:28-50` (kinds), `:82-84` (event names), `:151` (payloads), and `Parse`
- Test: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `payload{Event, SessionID}`, `seenEvent`, `Parse`.
- Produces: `hooks.Froze`, `hooks.Thawed` (`Kind`); `func FrozenPayload(id string, frozen bool) []byte`.

`Event` needs no new field: the *event name* carries the direction, so nothing has to encode a bool.

- [ ] **Step 1: Write the failing test**

Add to `internal/hooks/hooks_test.go`:

```go
// The harness's own edges, in the same envelope Claude Code's hooks use so
// the receiver has one way in. The direction rides in the event name rather
// than in a field, so the payload shape is untouched.
func TestAFrozenPayloadArrivesAsFrozeOrThawed(t *testing.T) {
	for _, c := range []struct {
		frozen bool
		want   hooks.Kind
	}{
		{true, hooks.Froze},
		{false, hooks.Thawed},
	} {
		event, ok := hooks.Parse(hooks.FrozenPayload("abc123", c.frozen))
		if !ok {
			t.Fatalf("a frozen=%v payload said nothing the harness acts on", c.frozen)
		}
		if event.Kind != c.want {
			t.Errorf("frozen=%v parsed as %q, want %q", c.frozen, event.Kind, c.want)
		}
		if event.Session != "abc123" {
			t.Errorf("parsed session %q, want abc123", event.Session)
		}
	}
}
```

Check `Parse`'s real signature in `hooks.go` before writing this — if it takes an `io.Reader` or returns `(Event, bool)` in another order, match it. The assertions do not change.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/hooks/ -run TestAFrozenPayload -v`
Expected: FAIL — `undefined: hooks.Froze`.

- [ ] **Step 3: Add the kinds, names and payload**

In the `Kind` const block, after `Seen`:

```go
	// Froze: the Session's pane has started holding a mode over its live
	// view. It says nothing about what the Session is doing — a Frozen pane
	// sits over a Session in any state, which is exactly when it reads as a
	// hang.
	Froze Kind = "Froze"
	// Thawed: the pane is showing the live Session again.
	Thawed Kind = "Thawed"
```

Beside `seenEvent`:

```go
// frozeEvent and thawedEvent name the harness's own mode edges on the wire.
// Like seenEvent they are deliberately not names Claude Code could grow into.
// The direction is the name rather than a field, so the payload keeps the one
// shape the receiver already reads.
const (
	frozeEvent  = "GanymedeFroze"
	thawedEvent = "GanymedeThawed"
)
```

Beside `SeenPayload`:

```go
// FrozenPayload is what the pane-mode-changed hook sends the Dashboard about
// one Session behind the pane whose mode just changed.
func FrozenPayload(id string, frozen bool) []byte {
	event := thawedEvent
	if frozen {
		event = frozeEvent
	}
	body, err := json.Marshal(payload{Event: event, SessionID: id})
	if err != nil {
		// payload is two strings; encoding/json cannot fail on it.
		return nil
	}
	return body
}
```

In `Parse`, beside the case that maps `seenEvent` to `Seen`, add:

```go
	case frozeEvent:
		event.Kind = Froze
	case thawedEvent:
		event.Kind = Thawed
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/hooks/ ./internal/state/ -v`
Expected: PASS. `state.apply` ends in `default: return`, so the new kinds pass through the state model untouched — that is deliberate and must stay true.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git commit -m "Carry a pane freezing and thawing as harness events" -m "The direction rides in the event name rather than a new payload field, so the envelope the receiver already reads is unchanged. The state model ignores both kinds by its existing default, which is right: whether a pane holds a mode is not a claim about what Claude is doing, and has no business in a three-source merge about that." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `ganymede frozen`

**Files:**
- Modify: `cmd/ganymede/main.go` (dispatch switch, new `frozen` func, `usage`)

**Interfaces:**
- Consumes: `registry.Default()`, `topology.Under(root int, pids []int) ([]int, error)`, `hooks.DefaultSocket()`, `hooks.Forward(socket string, body []byte) error`, `hooks.FrozenPayload(id string, frozen bool) []byte`.
- Produces: `func frozen(args []string) error`.

No test file. `frozen` is a transcription of `seen`, which is likewise untested at this layer; everything in it worth asserting lives behind `topology.Under` and `hooks.Forward`, both tested where they are. Adding the first test file to `package main` to cover a copy of an untested neighbour is not what this change should spend its restructuring on.

- [ ] **Step 1: Add the subcommand**

In the dispatch switch, after `case "seen":`:

```go
	case "frozen":
		return frozen(args[1:])
```

Add beside `seen`:

```go
// frozen reports every Session running inside a pane whose mode just changed
// as being behind a held view, or back in front of a live one.
//
// It is seen's shape for the same reason: tmux can only name the process it
// started in the pane, and the Sessions are that process's descendants. Like
// seen and the hook command it is run by something that must not be held up,
// so it says nothing about what it could not do.
func frozen(args []string) error {
	if len(args) != 2 {
		return errors.New("frozen takes the pid of the pane and whether it is in a mode")
	}
	pane, err := strconv.Atoi(args[0])
	if err != nil {
		return nil
	}
	held := args[1] == "1"

	sessions, err := registry.Default()
	if err != nil {
		return nil
	}
	running, err := sessions.Read()
	if err != nil {
		return nil
	}
	pids := make([]int, len(running))
	for i, s := range running {
		pids[i] = s.PID
	}
	inside, err := topology.Under(pane, pids)
	if err != nil || len(inside) == 0 {
		return nil
	}

	socket, err := hooks.DefaultSocket()
	if err != nil {
		return nil
	}
	within := map[int]bool{}
	for _, pid := range inside {
		within[pid] = true
	}
	for _, s := range running {
		if within[s.PID] {
			_ = hooks.Forward(socket, hooks.FrozenPayload(s.ID, held))
		}
	}
	return nil
}
```

- [ ] **Step 2: Add it to `usage`**

Find the `usage` const and add a line matching the existing formatting, e.g.:

```
  frozen <pane-pid> <0|1>   Report that a pane has started or stopped holding a mode (run by tmux)
```

- [ ] **Step 3: Verify it builds and runs**

```bash
go build ./... && go run ./cmd/ganymede frozen
```
Expected: exits non-zero with `frozen takes the pid of the pane and whether it is in a mode`.

```bash
go run ./cmd/ganymede frozen notanumber 1; echo "exit=$?"
```
Expected: `exit=0`, no output — a malformed argument from a tmux hook must never make noise in a pane.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go test ./...
git add cmd/ganymede/main.go
git commit -m "Add the frozen entry point tmux reports mode changes to" -m "Modelled on seen, and for the same reason: tmux names only the process it started in the pane, so the Sessions behind a mode change have to be resolved out of it. Like seen it is run inside a tmux hook and stays silent about anything it could not do." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `fanned` splits any stream

**Files:**
- Modify: `cmd/ganymede/main.go` (`fanned`)

**Interfaces:**
- Produces: `func fanned[T any](ctx context.Context, in <-chan T) (a, b <-chan T)`.

Mechanical only. No behaviour change — its one existing caller must be untouched at the call site. Committed separately so the wiring commit that follows is readable.

- [ ] **Step 1: Make it generic**

```go
// fanned splits one stream into two, so two watchers can each take it on their
// own goroutine without racing each other for values meant for both. The
// working sets go to the Dashboard and the notifier; the reported events go to
// the state model and the Dashboard.
func fanned[T any](ctx context.Context, in <-chan T) (a, b <-chan T) {
	toA := make(chan T)
	toB := make(chan T)
	go func() {
		defer close(toA)
		defer close(toB)
		for {
			select {
			case <-ctx.Done():
				return
			case value, ok := <-in:
				if !ok {
					return
				}
				for _, out := range [](chan<- T){toA, toB} {
					select {
					case out <- value:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return toA, toB
}
```

- [ ] **Step 2: Verify nothing else changed**

Run: `go build ./... && go test ./...`
Expected: PASS. `fanned(ctx, merged)` still infers `T = []session.Session`; the call site is unchanged.

- [ ] **Step 3: Commit**

```bash
gofmt -l . && go test ./...
git add cmd/ganymede/main.go
git commit -m "Let fanned split a stream of any type" -m "The reported-events channel needs the same two-reader split the working sets already get, and a channel has one reader. Type parameter only — the existing call site and its behaviour are unchanged." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: The rail marks a Frozen row

**Files:**
- Modify: `internal/dashboard/dashboard.go` (mark const near `:28-35`, `Model` fields near `:222-230`, message types near `:39-58`, `Update` near `:404-417`, `rebuilt`'s `answers` near `:539-546`, `busyMark` at `:1265-1274`, `repoLine` at `:1285`)
- Modify: `internal/dashboard/rows.go` (`row`, `answers`, `rowsOf`)
- Create: `internal/dashboard/frozen_test.go`

**Interfaces:**
- Consumes: `row`, `answers`, `rowsOf`, `Model.rebuilt()`, `popup.Status`.
- Produces: `type FrozenPanes map[string]bool`; `row.frozen bool`; `answers.frozen func(id string) bool`; `func marks(r row) string` (replaces `busyMark`); `Model.frozenOf(id string) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/dashboard/frozen_test.go`. Follow the construction idiom the other dashboard tests use — `dashboard.New(nil, dashboard.Harness{...})`, feed a `dashboard.Sessions{...}`, then the message under test, then read `model.View()`.

```go
package dashboard_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// frozenMark is the Frozen mark from the spec: one column, on the row whose
// pane is showing a held view rather than the live Session.
const frozenMark = "❄"

// A Frozen pane is orthogonal to the Session's state: the Session goes on
// Working while the pane in front of you shows a picture of it from whenever
// the mode was entered. Both facts belong on the row.
func TestARowWhosePaneIsFrozenCarriesTheMark(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})

	if strings.Contains(model.View(), frozenMark) {
		t.Fatal("a row nothing has said anything about already carries the Frozen mark")
	}

	model, _ = model.Update(dashboard.FrozenPanes{"abc123": true})

	if !strings.Contains(model.View(), frozenMark) {
		t.Errorf("the row does not carry the Frozen mark:\n%s", model.View())
	}
	if !strings.Contains(model.View(), string(session.Working)) {
		t.Errorf("the Session stopped reading as Working once its pane froze:\n%s", model.View())
	}
}

// Frozen is not attention. It is your own doing, not the Session asking
// something of you, so it must not touch the counts, the order, or the strip.
func TestAFrozenRowIsNotAttention(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Idle,
	}})
	before := model.View()

	model, _ = model.Update(dashboard.FrozenPanes{"abc123": true})
	after := model.View()

	if strings.Count(before, "●") != strings.Count(after, "●") ||
		strings.Count(before, "█") != strings.Count(after, "█") {
		t.Errorf("freezing a pane changed the attention marks:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
```

Check the exact `session.Session` field names against `internal/session/session.go` before writing — `PID`, `ID`, `Dir`, `Name`, `State`, `Since` — and give `Dir` a real temp path so the row groups under a root.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestARowWhosePaneIsFrozen|TestAFrozenRowIsNotAttention' -v`
Expected: FAIL — `undefined: dashboard.FrozenPanes`.

- [ ] **Step 3: Add the mark, the map and the message**

In `internal/dashboard/dashboard.go`, beside `caution` and `popupBusy`:

```go
// frozen is the mark a row carries while its pane is holding a mode over the
// live Session: what that pane is showing you is a held view, and the keys
// you type into it are going to the mode rather than to Claude.
const frozen = "❄"
```

Beside `Cautions` and `PopupStatuses`:

```go
// FrozenPanes is which Sessions are behind a pane holding a mode over the
// live view, by Claude session id, as the half-minute cross-check last found
// them.
//
// Like Cautions it arrives as a message rather than being read where it is
// drawn: asking tmux is a round trip, and a View that made one would make it
// several times a second.
type FrozenPanes map[string]bool
```

On `Model`, beside `cautions` and `popups`:

```go
	// frozen is which Sessions the harness last heard were behind a pane
	// holding a mode over the live view, by Claude session id. Never cleared,
	// only laid over by the next answer — for the reason cautions already
	// documents: a mark that blinked out while tmux was being asked again is
	// a mark you stop reading.
	frozen map[string]bool
```

In `Update`, beside the `PopupStatuses` case:

```go
	case FrozenPanes:
		m.frozen = msg
		m = m.rebuilt()
		return m, nil
```

And the lookup, beside `cautionOf`/`popupOf`:

```go
// frozenOf says whether the Session with this id is behind a pane holding a
// mode over the live view. A Session nothing has been said about is not
// frozen, which is the right default: the mark claims something, and its
// absence claims nothing.
func (m Model) frozenOf(id string) bool { return m.frozen[id] }
```

- [ ] **Step 4: Carry it on the row**

In `internal/dashboard/rows.go`, on `row`, after `popup`:

```go
	// frozen says the Session's own pane is holding a mode over the live
	// view, so what that pane shows is a held picture of the Session rather
	// than the Session. It is orthogonal to the Session's State — a Frozen
	// pane sits over one in any state, which is exactly when it reads as a
	// hang — and it is empty on a repo's header row, which has no pane of its
	// own to hold anything.
	frozen bool
```

On `answers`, after `popup`:

```go
	// frozen is whether a Session's own pane is holding a mode over the live
	// view.
	frozen func(id string) bool
```

In `rowsOf`, on the session-row append only:

```go
			rows = append(rows, row{
				root: root, session: running, ticket: ask.ticket(running.Dir, root), popup: ask.popup(running.Dir),
				holdsRoot: ask.checkout(running.Dir) == root, frozen: ask.frozen(running.ID),
			})
```

And in `rebuilt`'s `answers` literal in `dashboard.go`:

```go
		frozen:   m.frozenOf,
```

- [ ] **Step 5: Draw it**

Replace `busyMark` in `dashboard.go`:

```go
// marks are what you have done to a row, as against what its Session is
// doing: that its pane is frozen, and that its hidden Popup shell is running
// something (§8). They come with the trailing space that separates them from
// whatever follows, and a row carrying neither costs the layout nothing.
//
// Frozen comes first. Whether the pane is still showing you the live Session
// changes what the rest of the row means; what a popup underneath it is doing
// is a footnote to that.
func marks(r row) string {
	var said []string
	if r.frozen {
		said = append(said, frozen)
	}
	if r.popup.Busy {
		said = append(said, popupBusy)
	}
	if len(said) == 0 {
		return ""
	}
	return strings.Join(said, " ") + " "
}
```

Update both call sites: `line()` — `mark := marks(r)`; `repoLine()` — `mark := strings.TrimRight(marks(r), " ")`. A header row's `frozen` is always false, so `repoLine` is unchanged in behaviour.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS, including every existing row-rendering test — a row with no Frozen mark must render byte-identically to before.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go test ./...
git add internal/dashboard/dashboard.go internal/dashboard/rows.go internal/dashboard/frozen_test.go
git commit -m "Mark a row whose pane is showing a held view" -m "A pane holding a mode shows a picture of the Session from whenever the mode was entered, while the Session itself carries on — so the rail read normal while the screen read dead. The mark goes in the column that already carries the busy-popup mark, which is where the things you are doing to a row live, as against what its Session is doing." -m "Frozen is not attention: it changes no count, no order, and nothing in the strip." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The mark appears and clears on the edge

**Files:**
- Modify: `internal/dashboard/dashboard.go` (message types, `Update`)
- Modify: `internal/dashboard/frozen_test.go`

**Interfaces:**
- Consumes: `Model.frozen`, `Model.rebuilt()`.
- Produces: `type Froze string`, `type Thawed string`, `func (m Model) freezing(id string, held bool) Model`.

- [ ] **Step 1: Write the failing test**

Append to `internal/dashboard/frozen_test.go`:

```go
// The edge off the pane-mode-changed hook, which is what makes the mark
// quick — and, on the leaving edge, what makes it go away the moment you
// press q rather than up to half a minute later.
func TestTheMarkFollowsTheModeEdges(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})

	model, _ = model.Update(dashboard.Froze("abc123"))
	if !strings.Contains(model.View(), frozenMark) {
		t.Errorf("Froze did not put the mark on the row:\n%s", model.View())
	}

	model, _ = model.Update(dashboard.Thawed("abc123"))
	if strings.Contains(model.View(), frozenMark) {
		t.Errorf("Thawed did not take the mark off the row:\n%s", model.View())
	}
}

// An edge must not write through the map the last cross-check handed over:
// a message's own value belongs to whoever sent it.
func TestAnEdgeDoesNotWriteThroughTheSweptMap(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})

	swept := dashboard.FrozenPanes{"abc123": false}
	model, _ = model.Update(swept)
	model, _ = model.Update(dashboard.Froze("abc123"))

	if swept["abc123"] {
		t.Error("an edge wrote through the map the cross-check sent")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestTheMarkFollows|TestAnEdgeDoesNot' -v`
Expected: FAIL — `undefined: dashboard.Froze`.

- [ ] **Step 3: Add the messages and the handler**

Beside `FrozenPanes`:

```go
// Froze says a Session's pane has started holding a mode over the live view,
// and Thawed that it has stopped. Both name the Session by Claude's own id,
// which is what the pane-mode-changed hook resolves the pane into.
//
// They are the quick half of the answer: the half-minute FrozenPanes sweep is
// what makes it true again after a Dashboard restart, a fragment not yet
// sourced, or an edge that never arrived.
type (
	Froze  string
	Thawed string
)
```

In `Update`:

```go
	case Froze:
		return m.freezing(string(msg), true), nil
	case Thawed:
		return m.freezing(string(msg), false), nil
```

And:

```go
// freezing records one mode edge against the Session it was about.
//
// The map is copied rather than written through: the value standing in
// m.frozen came in as a FrozenPanes message and belongs to whoever sent it,
// and a Model that edited it would be reaching back into a message it has
// already handled.
func (m Model) freezing(id string, held bool) Model {
	next := make(map[string]bool, len(m.frozen)+1)
	// Not `for session, frozen := range` — this package imports session,
	// frozen is the mark's own const, and held is this function's parameter.
	for behind, was := range m.frozen {
		next[behind] = was
	}
	next[id] = held
	m.frozen = next
	return m.rebuilt()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/dashboard/dashboard.go internal/dashboard/frozen_test.go
git commit -m "Put the Frozen mark up and take it down on the edge" -m "The half-minute sweep alone would leave the mark up to thirty seconds late appearing and the same again clearing, on a mark whose whole job is explaining a screen that looks dead right now. The edges come off the pane-mode-changed hook, which fires in both directions." -m "The edge copies the map rather than writing through it: what stands in m.frozen arrived as a message and belongs to its sender." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: The half-minute cross-check

**Files:**
- Modify: `internal/dashboard/dashboard.go` (`Harness` struct and a new interface, `Tick` case, new command)
- Modify: `internal/dashboard/frozen_test.go`

**Interfaces:**
- Consumes: `topology.Harness.Frozen(pids []int) (map[int]bool, error)` (Task 1), `Model.set`, `ticking()`, `m.sweepingPopups()`.
- Produces: `type Panes interface { Frozen(pids []int) (map[int]bool, error) }`; `Harness.Panes` field; `func (m Model) sweepingFrozen() tea.Cmd`.

- [ ] **Step 1: Write the failing test**

Append to `internal/dashboard/frozen_test.go`:

```go
// panes is a stand-in for the harness's hand on tmux.
type panes struct {
	frozen map[int]bool
	err    error
	asked  [][]int
}

func (p *panes) Frozen(pids []int) (map[int]bool, error) {
	p.asked = append(p.asked, pids)
	return p.frozen, p.err
}

// The cross-check under the hook: what catches a mode entered while the
// Dashboard was down, or an edge that never arrived.
func TestTheTickSweepsForFrozenPanes(t *testing.T) {
	swept := &panes{frozen: map[int]bool{4242: true}}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Panes: swept})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})

	_, cmd := model.Update(dashboard.Tick{})
	if cmd == nil {
		t.Fatal("the tick asked for nothing")
	}
	// Run the batch and feed back whatever the sweep produced.
	model = drain(t, model, cmd)

	if !strings.Contains(model.View(), frozenMark) {
		t.Errorf("the sweep did not mark the frozen row:\n%s", model.View())
	}
}

// A sweep that failed says nothing about any pane, and leaves the last answer
// standing rather than blanking a mark tmux was simply slow to answer about.
func TestAFailedSweepLeavesTheLastAnswerStanding(t *testing.T) {
	swept := &panes{err: errors.New("tmux is not there")}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Panes: swept})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})
	model, _ = model.Update(dashboard.FrozenPanes{"abc123": true})

	_, cmd := model.Update(dashboard.Tick{})
	model = drain(t, model, cmd)

	if !strings.Contains(model.View(), frozenMark) {
		t.Errorf("a failed sweep blanked the mark:\n%s", model.View())
	}
}
```

`drain` runs a `tea.Cmd` (unwrapping `tea.Batch` via `tea.BatchMsg`) and feeds each non-nil message back through `Update`. If the dashboard tests already have such a helper — the popup-sweep tests need the same thing — reuse it rather than writing a second. Check `popups_test.go` first.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestTheTickSweeps|TestAFailedSweep' -v`
Expected: FAIL — `unknown field Panes in struct literal`.

- [ ] **Step 3: Add the interface and the sweep**

Beside the other harness interfaces:

```go
// Panes is what the harness can say about the tmux panes the Sessions are
// running in.
type Panes interface {
	// Frozen says which of pids are running in a pane holding a mode over
	// its live view. A pid it cannot place in a pane is left out.
	Frozen(pids []int) (map[int]bool, error)
}
```

On `Harness`, beside `Popups`:

```go
	// Panes is what tmux says about the panes the Sessions run in.
	Panes Panes
```

The command:

```go
// sweepingFrozen asks tmux which Sessions are behind a pane holding a mode.
//
// It is the cross-check under the pane-mode-changed hook, on the same
// half-minute clock the cautions are re-read on: the hook is what makes the
// mark quick, and this is what makes it true after a Dashboard restart, a
// fragment not yet sourced into a running server, or an edge that never
// arrived.
//
// tmux is asked in pids, which is the only name it could match a Session by,
// and answers in them; the ids are put back on here, where the working set
// this was asked about is still in hand.
func (m Model) sweepingFrozen() tea.Cmd {
	if m.harness.Panes == nil || len(m.set) == 0 {
		return nil
	}
	ids := make(map[int]string, len(m.set))
	pids := make([]int, 0, len(m.set))
	for _, s := range m.set {
		ids[s.PID] = s.ID
		pids = append(pids, s.PID)
	}
	panes := m.harness.Panes
	return func() tea.Msg {
		held, err := panes.Frozen(pids)
		if err != nil {
			// A sweep that failed said nothing about any pane, and reporting
			// no message at all leaves the last answer standing — unlike a
			// FrozenPanes of nothing, which would take every mark off the
			// rail over a tmux that was briefly not there.
			return nil
		}
		frozen := make(FrozenPanes, len(held))
		for pid, held := range held {
			if id := ids[pid]; id != "" {
				frozen[id] = held
			}
		}
		return frozen
	}
}
```

In the `Tick` case, add it to the batch:

```go
		return m, tea.Batch(ticking(), m.reading(), m.sweepingPopups(), m.sweepingFrozen())
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS. Existing `Tick` tests must still pass — a `Harness` with no `Panes` returns a nil `tea.Cmd`, which `tea.Batch` drops.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/dashboard/dashboard.go internal/dashboard/frozen_test.go
git commit -m "Cross-check the Frozen marks every half minute" -m "The pane-mode-changed hook only covers edges a running Dashboard was there to hear. A mode entered while it was down, a fragment not yet sourced into a running server, or an edge that never arrived would otherwise leave the rail confidently wrong until the next one." -m "A failed sweep reports no message rather than an empty one, so tmux being briefly unavailable leaves the last answer standing instead of clearing every mark." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: The SELECTED box says it in words

**Files:**
- Modify: `internal/dashboard/dashboard.go:1491-1500` (the session branch of `selected`)
- Modify: `internal/dashboard/frozen_test.go`

**Interfaces:**
- Consumes: `row.frozen`, the `standing` string built from `r.session.State` and `ageOf`.

- [ ] **Step 1: Write the failing test**

Append to `internal/dashboard/frozen_test.go`:

```go
// The rail draws the mark; the box spells it. A one-column glyph is a
// reminder for someone who already knows what it means, and this is where
// the first person to see one finds out.
func TestTheSelectedBoxSaysTheRowIsFrozen(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{})
	model, _ = model.Update(dashboard.Sessions{{
		PID: 4242, ID: "abc123", Dir: t.TempDir(),
		Name: "ganymede-1e", State: session.Working,
	}})
	model, _ = model.Update(dashboard.FrozenPanes{"abc123": true})

	if !strings.Contains(model.View(), "· frozen") {
		t.Errorf("the box does not say the pane is frozen:\n%s", model.View())
	}
}
```

The cursor starts on row 0, which is the repo header. Move it onto the session row first with whatever key the other dashboard tests use (`tea.KeyMsg` for `down`/`j`) — check `dashboard_test.go` for the idiom and match it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestTheSelectedBoxSays -v`
Expected: FAIL — no `· frozen` in the view.

- [ ] **Step 3: Add the words**

In `selected`, after the age is appended to `standing`:

```go
	state := styleOf(r.session.State)
	standing := string(r.session.State)
	if age := ageOf(*r.session); age != "" {
		standing += " · " + age
	}
	if r.frozen {
		// Last, and alongside the state rather than instead of it: the
		// Session is still doing whatever it is doing, and the pane not
		// showing you that is a separate fact about the same row.
		standing += " · frozen"
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go test ./...
git add internal/dashboard/dashboard.go internal/dashboard/frozen_test.go
git commit -m "Spell out a Frozen pane in the SELECTED box" -m "A one-column glyph is a reminder for someone who already knows what it means. It reads alongside the state rather than instead of it, because the Session is still doing whatever it is doing and the pane not showing you that is a separate fact about the same row." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Wire it together

**Files:**
- Modify: `cmd/ganymede/main.go` (`runDashboard`)

**Interfaces:**
- Consumes: `fanned` (Task 6), `hooks.Froze`/`hooks.Thawed` (Task 4), `dashboard.Froze`/`dashboard.Thawed` (Task 8), `dashboard.Harness.Panes` (Task 9), `topology.Harness.Frozen` (Task 1).

- [ ] **Step 1: Fan the reported events**

In `runDashboard`, immediately after `reported, err := hooks.Listen(ctx, socket)` and its error check:

```go
	// The state model and the Dashboard both want the reported events, and a
	// channel has one reader. The state model ignores the mode edges by its
	// own default; the Dashboard is the only thing that acts on them, and it
	// keeps them out of the three-source merge, where a fact about a tmux
	// pane has no business being adjudicated.
	stateEvents, paneEvents := fanned(ctx, reported)
```

Change the `model.Watch` call to take `stateEvents`:

```go
	merged := model.Watch(ctx, watch, checked, stateEvents)
```

- [ ] **Step 2: Give the Dashboard the harness's hand on panes**

In the `dashboard.Harness{...}` literal, add `Panes: harness` to the field list:

```go
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Focuser: harness, Strip: harness, Spawner: harness, Popups: harness, Approver: harness,
		Prompter: harness, Stopper: harness, Seen: model.Seen, Tickets: tickets, Panes: harness,
	}
```

- [ ] **Step 3: Translate the edges into messages**

Replace the final `tea.NewProgram(...).Run()` with:

```go
	program := tea.NewProgram(dashboard.New(dashboardSessions, hands), tea.WithAltScreen())
	// The mode edges reach the Dashboard as messages rather than down a
	// channel of its own: New already takes the one stream it is built
	// around, and a second parameter would be threaded through every caller
	// for a message two lines can send.
	go func() {
		for event := range paneEvents {
			switch event.Kind {
			case hooks.Froze:
				program.Send(dashboard.Froze(event.Session))
			case hooks.Thawed:
				program.Send(dashboard.Thawed(event.Session))
			}
		}
	}()
	_, err = program.Run()
	return err
```

- [ ] **Step 4: Verify it builds and the suite passes**

Run: `gofmt -l . && go build ./... && go test ./...`
Expected: no output from gofmt, PASS throughout.

- [ ] **Step 5: Verify it end to end, by hand**

```bash
go build -o bin/ganymede ./cmd/ganymede
```

Then, against the live harness:

1. `./bin/ganymede install` (or `up`) so the fragment carrying `pane-mode-changed` is written, and re-source it into the running sessions server: `tmux source-file ~/.config/ganymede/tmux.conf`.
2. Restart the dashboard so it has the `Panes` hand.
3. Freeze a Session's pane: `tmux copy-mode -t <session>:0.0`.
4. **Expected:** `❄` appears on that Session's row within a second, and the SELECTED box reads `· frozen`.
5. `tmux send-keys -X -t <session>:0.0 cancel`.
6. **Expected:** the mark clears within a second.
7. With a Session Blocked on a permission dialog and its pane frozen, press `y` on the dashboard. **Expected:** it refuses saying the pane is frozen, focuses the pane, and the dialog is untouched.

Record what actually happened. If any step differs, stop and report rather than adjusting the expectation.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go test ./...
git add cmd/ganymede/main.go
git commit -m "Wire the mode edges through to the Dashboard" -m "The reported events are fanned so the state model and the Dashboard each get their own tap, and the Dashboard's tap is translated into messages with program.Send rather than a second channel argument to New, which has fifteen call sites passing nil for the first one." -m "Frozen stays out of the state model on purpose: that package merges three sources under rules about which one is right about what Claude is doing, and whether a tmux pane is holding a mode is not that kind of claim." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: The documentation

**Files:**
- Modify: `CONTEXT.md` (after the `### Session states` block, before `### Main-root states`)
- Modify: `docs/ARCHITECTURE.md` (CLI entry points table; guarded send-keys list; Dashboard internals; "What the harness writes")

- [ ] **Step 1: Add the term to CONTEXT.md**

Insert between the `**Attention**` entry and `### Main-root states`:

```markdown
### Pane view

**Frozen**:
The Session's pane is showing a held tmux view rather than the live Session —
your keys reach the mode, not Claude. Orthogonal to every Session state: a
Frozen pane can sit over a Working one, which is exactly when it reads as a
hang.
_Avoid_: stuck, hung, copy-mode, scrolled
```

It goes in its own subsection rather than among the Session states on purpose: a sixth entry beside Working, Blocked, Ready, Idle and Shell would say Frozen is a state a Session is in *instead of* those, and it is not.

- [ ] **Step 2: Add the CLI entry point**

In the "CLI entry points other tools invoke" table, after the `ganymede seen <pid>` row:

```markdown
| `ganymede frozen <pane-pid> <0\|1>` | tmux's `pane-mode-changed` hook | Reports that a pane has started or stopped holding a mode over its live view |
```

- [ ] **Step 3: Add the guard step**

In the **Guarded send-keys** numbered list, insert a new step 2 and renumber the rest:

```markdown
2. Ask `#{pane_in_mode}` — a pane holding a mode takes the keystroke into the mode's own key table, and `capture-pane` cannot tell you so, since it returns the live screen the mode is holding a view over.
```

- [ ] **Step 4: Record the second owned hook**

Replace the paragraph beginning "Two things in that fragment are the harness's to own":

```markdown
Three things in that fragment are the harness's to own: tmux's global `pane-focus-in` hook, which is how seeing a session clears its Ready badge; its global `pane-mode-changed` hook, which is how a pane holding a mode over its live view earns the Frozen mark and loses it again; and the `@ganymede-seen` option both read. A `pane-focus-in` or `pane-mode-changed` hook of your own in `tmux.conf` would be replaced by them.
```

- [ ] **Step 5: Mention the mark on the rail**

In "Dashboard internals", extend the **Repo tree** bullet so the session-row description ends:

```markdown
... indented session rows with state glyph, the marks for a Frozen pane (`❄`) and a busy popup (`⏵`), session/worktree name, abbreviated ticket ID, and wait age.
```

- [ ] **Step 6: Check the docs against what was built**

Re-read the four ARCHITECTURE.md edits against the code as committed. Every claim must be true of the merged branch — particularly the guard step's position in the list, which Task 2 put *before* the capture.

- [ ] **Step 7: Commit**

```bash
git add CONTEXT.md docs/ARCHITECTURE.md
git commit -m "Record Frozen in the vocabulary and the architecture" -m "Frozen gets its own subsection rather than a sixth entry among the Session states: putting it there would say it is a state a Session is in instead of Working or Blocked, when the whole point is that it sits over one of those." -m "ARCHITECTURE.md's owned-hooks paragraph promised the harness owned exactly one global tmux hook, which is no longer true." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| The term, `### Pane view`, `_Avoid_` list | 12 |
| Session rows only, not header rows | 7 (`rowsOf` sets `frozen` on the session append only) |
| `panes()` grows `#{pane_in_mode}` | 1 |
| `Harness.Frozen`, absent ≠ false | 1 |
| Guard asks before sending, `modeHeld` named apart | 2 |
| Refuse + focus, never clear the mode | 2 (refusal); focus is the Dashboard's existing fallback, unchanged |
| `pane-mode-changed` fragment, `-b`, set-not-append | 3 |
| `ganymede frozen`, modelled on `seen` | 5 |
| `Froze`/`Thawed` kinds, no `Event` shape change | 4 |
| Half-minute cross-check; failed sweep says nothing | 9 |
| Keying by session id | 7, 9 |
| `❄` mark, Frozen before popup mark | 7 |
| SELECTED `· frozen` | 10 |
| Touches nothing in Attention/tier/strip/notifier | 7 (test), and no other task touches them |
| ARCHITECTURE.md edits | 12 |
| Testing: real tmux, "sent nothing" assertion | 1, 2 |
| `cmd/ganymede` untested, with reason | 5 |
| `show-hooks` trap | 3 (note before Step 1) |

No gaps.

**Placeholder scan:** none — every code step carries the code, every test step the test, every verification step the command and its expected output. The three "check the existing idiom first" notes (Tasks 1, 7, 9, 10) name the exact file to look in and what to match; they are not deferred decisions.

**Type consistency checked:**
- `Frozen(pids []int) (map[int]bool, error)` — Task 1 defines, Task 9's `Panes` interface and its `panes` stub match, Task 11 satisfies it with `topology.Harness`.
- `frozen` is used as three distinct identifiers by design: the `const frozen = "❄"` (dashboard), the `row.frozen` field, and the `answers.frozen` func. Go scoping keeps them apart, and `marks` reads `r.frozen` against the const `frozen` in one expression — verify it compiles as written; if the shadowing reads badly in review, rename the const to `frozenMark` in Task 7 and update the test's own `frozenMark` reference.
- `modeHeld` (unexported, per-target) vs `Frozen` (exported, per-pid-set) — deliberately not case variants of one name.
- `FrozenPanes` (sweep, `map[string]bool`) vs `Froze`/`Thawed` (edges, `string`) — all keyed by Claude session id.
