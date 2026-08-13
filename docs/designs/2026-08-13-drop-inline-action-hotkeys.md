# Drop the p/y/n/x/q inline-action hotkeys

## Why

The Dashboard's sidepanel currently lets you act on a selected Session without jumping into its pane: `p` prompts or queues, `y`/`n` approve or deny a Blocked dialog, `x` interrupts a Working turn, and `q` ends an Idle or Ready one. Each of these is redundant with jumping in (`⏎`) and acting on the Session's own pane directly. Removing them simplifies the Dashboard's keymap without losing any capability — the SELECTED box, the guarded tmux engine behind these five keys, and their footprint across three other surfaces all go with them.

## Scope

**Removed:** the `p`, `y`, `n`, `x`, `q` hotkeys, their dialogs, and every piece of code that exists only to serve them.

**Kept, unchanged:** `Stopper.End` in `internal/topology`, because Claim's **Takeover** action calls it directly to end an Idle session's occupant before claiming its root — it never went through the Dashboard's own `q` dialog. Its supporting helpers (`located`, `pasted`, `capturePaneJoined`, `exited`, `inputMarker`, `emptyInputLine`, `hasInputLine`, `bufferSeq`) stay for the same reason.

**Net effect:** a Blocked or Ready Session can no longer be acted on from the sidepanel at all. You jump in (`⏎`) and answer Claude Code directly. `w` (spawn), `c` (claim/release/takeover), `t`/`o` (ticket), and `g` (picker) are untouched.

This touches five separate surfaces: the Dashboard's own dialogs and hints, the guarded tmux engine behind them, the Dock's status-line legend, the README, and the architecture reference.

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
- Remove the "Digit keys are never scripted, because permission-dialog rows are dynamic" line — it's rationale for the permission-prompt content being deleted and has nothing to attach to afterward.
- Add a one-line note under "See what needs you" that a Blocked or Ready Session is acted on by jumping in (`⏎`), since the sidepanel no longer scripts a response itself.
- Reword the top status line: "claim/takeover, worktree spawn, **inline actions**, and notifications all work day-to-day" — "inline actions" named this exact feature, so it comes out.

### `docs/ARCHITECTURE.md`

Missed in the original pass — found while implementing, by sweeping `docs/*.md` for the same terms the reviewer checked against source. This doc documents the feature at length, not just in passing:

- **"Acting on sessions"** section: rewrite. It currently frames the whole guarded send-keys protocol — including a `#{pane_in_mode}` Frozen-pane check — as backing all five removed actions. That Frozen check is actually specific to `guard.go`'s `answer` (Approve/Deny only): `Send`/`located`/`End` never called `modeHeld` at all, confirmed by grepping for it — so the rewritten protocol is both narrower (End only) and more accurate (drops a step that was never true for End) than the original. The `PermissionRequest` hook paragraph stays as-is — it's about Blocked-reason reporting for the state model, not about the removed inline actions.
- **SELECTED detail box** bullet (Dashboard internals): "plus the inline input for prompt and confirm actions" → reword to name what's actually still there (claim, spawn, ticket entry, and the Takeover confirmation) — "prompt" specifically meant the removed `p` dialog.
- **Worktree sessions** section: "Ending a session goes through the dashboard's `q` action → graceful exit → Claude Code's own cleanup prompt" is no longer true for the general case — only Takeover still scripts an ending, and only as a side effect of claiming a root. Reword to say ending now happens the same way starting does (from inside the pane), with Takeover called out as the one path that still scripts it.
- **Build order** item 4, "Inline actions — the guarded send-keys engine, then `y`/`n`, `p`, `x`, `q`": reword to "Guarded send-keys — the engine now backing Takeover's End," dropping the dead key list while keeping the milestone.
- The data-flow mermaid diagram's `ACT["Action engine<br/>guarded send-keys"]` node and edge stay unchanged — still accurate, since guarded send-keys still exists for End.

A repo-wide sweep of `docs/*.md` and `docs/agents/*.md` for the same terms turned up nothing else. `docs/superpowers/**` is historical planning material for other, already-shipped features and is out of scope.

### `CONTEXT.md`

No changes. The glossary never named approve/deny/prompt/interrupt/end as terms of their own, so nothing there goes stale.

### `internal/session/session.go`

`ToolOf` becomes dead code once `guard.go`'s `dialogText` — its only caller anywhere in the repo — is deleted. Delete `ToolOf` too. `PermissionPrefix` stays; `internal/hooks/hooks.go` still uses it.

### Tests

Several of the files above own shared fakes and helpers that other, surviving test files depend on. These have to be relocated *before* the owning file is deleted, or `go test ./...` won't compile:

- `internal/dashboard/stop_test.go` defines the `stops`/`stopCall` fake, which `claim_test.go`'s Takeover tests also use to drive `Stopper.End` (7+ call sites). Move `stops`/`stopCall` into `claim_test.go` — the same file the `Stopper` interface itself is moving into — before deleting the rest of `stop_test.go`.
- `internal/dashboard/active_test.go` has two tests that exercise exactly the removed `y`/`p` guard-mismatch behavior via `approve_test.go`'s `approvals`/`withApprover` and `prompt_test.go`'s `prompts`/`withPrompter`: `TestTheGuardsApproveMismatchMarksItsRowEvenAfterTheCursorMovedOn` and `TestTheGuardsSendMismatchMarksItsRowEvenAfterTheCursorMovedOn`. Delete both from `active_test.go` in the same commit that deletes `approve_test.go`/`prompt_test.go`. The file's other tests (jump/focus-marking) are unrelated and stay.
- `internal/topology/guard_test.go` defines `guardable`, `shellQuoted`, `readKeylog`, and `dialogPane`, which `jump_test.go` and the surviving parts of `prompt_test.go`/`stop_test.go` also call. Relocate these four helpers (e.g. into `harness_test.go`) before deleting the rest of `guard_test.go`.

With that done:

- Delete `internal/dashboard/approve_test.go`, `internal/dashboard/prompt_test.go`, `internal/dashboard/stop_test.go`, and `internal/topology/guard_test.go`.
- Trim `internal/topology/prompt_test.go`: drop the `Send`/`InterruptAndSend` tests. Rewrite `TestConcurrentSendsNeverCrossPanes` to drive the race through `End` instead of `Send` — reusing `stop_test.go`'s existing `exitPane` helper — since `pasted` is unexported and the test lives in the external `topology_test` package.
- Trim `internal/topology/stop_test.go`: drop the `Interrupt` tests, keep the `End` tests (after relocating `exitPane` usage as above, this file's own `End` tests are otherwise untouched).
- Trim `internal/tmuxconf/tmuxconf_test.go` as described above.

## Verification

After the removal, `go build ./...` and `go test ./...` across the repo. Two places a stray reference would fail to compile: the `Stopper` interface shrink (`cmd/ganymede/main.go`, `claim.go`), and the shared test fakes/helpers called out in the Tests section above — do those relocations before deleting the files that currently own them, or the test build breaks. Everything else is inert deletion. No manual/UI check is needed beyond that, since this is pure removal with no new behavior to exercise.
