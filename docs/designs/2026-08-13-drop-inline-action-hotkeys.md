# Drop the p/y/n/x/q inline-action hotkeys

## Why

The Dashboard's sidepanel currently lets you act on a selected Session without jumping into its pane: `p` prompts or queues, `y`/`n` approve or deny a Blocked dialog, `x` interrupts a Working turn, and `q` ends an Idle or Ready one. Each of these is redundant with jumping in (`⏎`) and acting on the Session's own pane directly. Removing them simplifies the Dashboard's keymap without losing any capability — the SELECTED box, the guarded tmux engine behind these five keys, and their footprint across three other surfaces all go with them.

## Scope

**Removed:** the `p`, `y`, `n`, `x`, `q` hotkeys, their dialogs, and every piece of code that exists only to serve them.

**Kept, unchanged:** `Stopper.End` in `internal/topology`, because Claim's **Takeover** action calls it directly to end an Idle session's occupant before claiming its root — it never went through the Dashboard's own `q` dialog. Its supporting helpers (`located`, `pasted`, `capturePaneJoined`, `exited`, `inputMarker`, `emptyInputLine`, `hasInputLine`, `bufferSeq`) stay for the same reason.

**Net effect:** a Blocked or Ready Session can no longer be acted on from the sidepanel at all. You jump in (`⏎`) and answer Claude Code directly. `w` (spawn), `c` (claim/release/takeover), `t`/`o` (ticket), and `g` (picker) are untouched.

This touches four separate surfaces: the Dashboard's own dialogs and hints, the guarded tmux engine behind them, the Dock's status-line legend, and the README.

## Changes by surface

### `internal/topology` — the guarded tmux engine

- Delete `guard.go` entirely: `Approve`, `Deny`, `answer`, `settled`, `modeHeld`, `capturePane`, `expected`, `dialogText`, `ellipsis`. Nothing else in the package uses any of it.
- Relocate the `redrawBudget`/`redrawPoll` constants (currently declared in `guard.go`) into `stop.go`, since `exited()` — which stays — still needs them.
- In `prompt.go`, delete `Send`, `sendInto`, `InterruptAndSend`, `escaped`, `interrupted`, `settledOn`. Keep `bufferSeq`, `inputMarker`, `emptyInputLine`, `hasInputLine`, `capturePaneJoined`, `located`, `pasted` — all still load-bearing for `End`.
- In `stop.go`, delete `Interrupt`. Keep `End` and `exited`.

### `internal/dashboard`

- Delete `approve.go` and `prompt.go` entirely: the `Approver`/`Prompter` interfaces, both dialogs (`prompting`, `ending`... — see below), their message types, and their view rendering.
- `stop.go` is almost entirely `x`/`q`-only: `startEnd`, `ending`, `endingKey`, `end`, `endingView`, `interrupt`, `stopped`, and the `interrupted`/`ended` message types. Delete the file, but keep the `Stopper` interface — shrunk to just `End(pid int) error` — by moving its declaration into `claim.go`, its only remaining consumer.
- In `dashboard.go`:
  - Remove the `Approver`/`Prompter` fields from the `harness` struct.
  - Remove the `case "p"`, `"y"`, `"n"`, `"x"`, `"q"` branches from the key switch.
  - Remove the `m.prompting`/`m.ending` routing from both `Update` and the SELECTED-box rendering.
  - Remove the `answered`/`sent`/`interrupted`/`ended` cases from `Update`'s message switch.
  - In `offering()`, delete the `switch r.session.State { case Blocked / Idle,Ready / Working ... }` block entirely, leaving just `⏎ jump`, `t ticket`, `o open`.

### `internal/tmuxconf` — the Dock's status-line legend

This is a separate vocabulary list from the Dashboard's own SELECTED-box hints, with its own entries and pinned tests.

- In `tmuxconf.go`, remove `"p prompt/queue"`, `"y approve"`, `"n deny"`, `"x interrupt"`, `"q end"` from the `legendKeys` slice.
- Trim the doc comment above `legendKeys` that calls out `p` as a key whose label changes with the row under it — only `c` (claim/release/takeover) is left doing that.
- In `tmuxconf_test.go`:
  - `TestTheDockStatusLineCarriesTheKeyLegend`: drop `"y approve"`, `"n deny"`, `"x interrupt"`, `"q end"` from the `want` list.
  - `TestTheDockLegendSpellsOutTheKeysThatChangeWithTheRow`: drop the `{"p", "prompt"}, {"p", "queue"}` cases, leaving only the three `c` cases.
  - The narrow-width, chord-naming, and honesty tests don't reference the removed keys directly and need no change.

### `cmd/ganymede/main.go`

Drop `Approver: harness` and `Prompter: harness` from the wiring struct literal. Keep `Stopper: harness`.

### `README.md`

- Delete the "Respond to a permission prompt inline" section and its table-of-contents entry.
- Remove the `y`/`n`, `p`, `x`, `q` rows from the Keys table.
- Add a one-line note under "See what needs you" that a Blocked or Ready Session is acted on by jumping in (`⏎`), since the sidepanel no longer scripts a response itself.
- Reword the top status line: "claim/takeover, worktree spawn, **inline actions**, and notifications all work day-to-day" — "inline actions" named this exact feature, so it comes out.

### `CONTEXT.md`

No changes. The glossary never named approve/deny/prompt/interrupt/end as terms of their own, so nothing there goes stale.

### Tests

- Delete `internal/dashboard/approve_test.go`, `internal/dashboard/prompt_test.go`, `internal/dashboard/stop_test.go`, and `internal/topology/guard_test.go` outright.
- Trim `internal/topology/prompt_test.go`: drop the `Send`/`InterruptAndSend` tests. Rewrite `TestConcurrentSendsNeverCrossPanes` to exercise `pasted`/`End` instead of `Send`, since that's what's left to race-check for buffer-naming collisions.
- Trim `internal/topology/stop_test.go`: drop the `Interrupt` tests, keep the `End` tests.
- Trim `internal/tmuxconf/tmuxconf_test.go` as described above.

## Verification

After the removal, `go build ./...` and `go test ./...` across the repo. The `Stopper` interface shrink is the one place a stray reference would fail to compile (`cmd/ganymede/main.go`, `claim.go`); everything else is inert deletion. No manual/UI check is needed beyond that, since this is pure removal with no new behavior to exercise.
