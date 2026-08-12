# Dashboard: bring the sidepanel back in line with the validated mock

## Problem Statement

The Dashboard is structurally the mock's variant D already — condensed repo tree
on the left, working client on the right, SELECTED box underneath — but running
it against a real working set shows that most of its 40 columns go to text that
says nothing.

Reading the Dashboard as it stands today:

```
GANYMEDE
────────────────────────────────────────
ganymede                               ▣
  ○ ganymede-51            no ticket now
focus-service-ai-credit-usage ⚠ FIRE-… ▣
  ○ focus-service-ai-credi FIRE-2923 15m
plans                          ⚠ dirty ▣
  ○ plans-bf                 no ticket 2d
api-internal                           ○
focus-frontend ⚠ FIRE-2910-followup-s… ○
focus-service-ai-assistant ⚠ FIRE-291… ○
focus-service-authorization ⚠ FIRE-29… ○
payments-backend ⚠ FIRE-2923/focus-ai… ○
service-auth-frontend ⚠ FIRE-2935/foc… ○
settle-probe                           ○
────────────────────────────────────────
SELECTED
focus-service-ai-credit-usage
▣ root: In use by agent
⚠ off default: FIRE-2923/account-allowa…
…leadercrm/focus-service-ai-credit-usage
⏎ go to repo · w spawn · c takeover

           (twenty blank lines)
```

Six separate things are wrong with that picture.

1. **Session rows are named after their repo.** All three live Sessions above
   are working in their Main root, and Claude Code auto-names such a Session
   `<repo>-<two characters>` — `ganymede-51`, `plans-bf`,
   `focus-service-ai-credit-usage-c9`. The row therefore spends its widest
   column repeating the repo header immediately above it, and repeating it
   truncated: `focus-service-ai-credi` is the same repo as the header, cut off
   before the suffix that was the only new information in it. Nothing on the
   row answers the question the Dashboard exists to answer — whether that
   Session is holding the Main root or sitting in a Worktree session.

2. **The git caution and the repo name fight over one line.** `focus-frontend
   ⚠ FIRE-2910-followup-s… ○` gives up the branch after eighteen characters,
   and `focus-service-ai-credit-usage ⚠ FIRE-… ▣` gives up both the branch and
   the repo name. Eight of the eleven repos on show carry a caution, so this is
   the normal case, not the edge. You can only find out which branch a root is
   parked on by moving the cursor onto it and reading the SELECTED box — one
   repo at a time, when the question ("where can I check out a PR?") is asked
   about the whole working set at once.

3. **`no ticket` costs nine columns to say nothing**, on the rows that have the
   least to spare, and the tickets that are there are spelled in full when the
   project prefix is the same `FIRE-` on every row of every repo.

4. **The SELECTED box floats.** It should sit at the Dashboard's foot; instead
   it ends wherever the tree happens to end, so it moves up and down the panel
   as Sessions come and go, and the twenty lines below it are dead. There is
   nowhere on the panel your eye can learn to look for detail.

5. **`GANYMEDE` and `SELECTED` are drawn identically** — both plain bold — so
   the panel's name and one of its section labels carry the same weight, and
   neither reads as the mock's blue brand.

6. **The Dock says nothing about itself.** There is no clock (Ghostty runs
   fullscreen, so the menu bar's is hidden) and no list of what the Dashboard's
   keys are, so every gesture beyond `↑↓` and `⏎` has to be remembered or
   rediscovered by moving the cursor onto a row that offers it. Meanwhile the
   working client's status line is stock tmux green, which is the one loud thing
   in an otherwise dark Dock.

## Solution

Spend the freed columns on what the mock puts there.

```
GANYMEDE                          14:32
────────────────────────────────────────
ganymede                               ▣
  ○ main                              now
focus-service-ai-credit-usage          ▣
 ⚠ FIRE-2923/account-allowances · dirty
  ○ main                       F-2923 15m
plans                                  ▣
 ⚠ dirty
  ○ main                             2d
api-internal                           ○
focus-frontend                         ○
 ⚠ FIRE-2910-followup-sorting
focus-service-ai-assistant             ○
 ⚠ FIRE-2914/assistant-streaming
settle-probe                           ○
────────────────────────────────────────
SELECTED
▣ root: In use by agent
focus-service-ai-credit-usage
⚠ off default: FIRE-2923/account-allowa…
…leadercrm/focus-service-ai-credit-usage
⏎ go to repo · w spawn · c takeover
```

with the SELECTED box pinned to the foot whatever the tree does, and a key
legend along the bottom of the whole Dock.

Each Session row now says which checkout it has its hands on rather than what
Claude Code happened to call it: `main` when it is holding the Main root,
`wt·<name>` when it is a Worktree session. Every repo's caution gets a line of
its own, so the branch is readable and the repo name never gives up a column
for it. The ticket is abbreviated to the digits that differ, and a Session about
no ticket says so in the SELECTED box rather than on the row.

None of this is new design. SPEC.md §5 already reads "Layout per the validated
mock (variant D)", already asks for an "abbreviated ticket ID", and already
places the SELECTED box "at the rail's foot". This is the Dashboard catching up
to it.

## User Stories

1. As a developer running agents across ten repos, I want each Session row to
   name the checkout it is working in, so that I can tell a Main root occupant
   from a Worktree session without moving the cursor.
2. As a developer with a Session in a repo's Main root, I want its row to read
   `main`, so that the row stops repeating the repo header directly above it.
3. As a developer with a Worktree session running, I want its row to read
   `wt·<worktree name>`, so that I can see at a glance that the Main root is
   still free for a PR.
4. As a developer whose worktree is named after its ticket, I want the row's
   checkout label to leave the ticket to the ticket column, so that
   `FIRE-2841-paging` reads as `wt·paging` and the part I can actually tell
   apart is the part that survives truncation.
5. As a developer running two Sessions in one repo — one in the Main root, one
   in a worktree — I want their rows to differ in the first word, so that I can
   tell which is which while both are Working on the same ticket.
6. As a developer checking whether I can review a PR, I want every repo's git
   caution on a line of its own, so that I can scan the whole working set's
   branches at once instead of one cursor position at a time.
7. As a developer whose repo is parked on a long branch name, I want the caution
   line to show the whole branch, so that `FIRE-2910-followup-sorting` is
   readable rather than cut to `FIRE-2910-followup-s…`.
8. As a developer with a long repo name, I want the repo header to keep its full
   width, so that `focus-service-ai-credit-usage` is never truncated to make
   room for a branch.
9. As a developer scanning the Main-root marks down the panel, I want them to
   stay in one column on every repo header, so that the column reads as a list
   of roots I can and cannot have.
10. As a developer whose root is dirty but on the default branch, I want the
    caution line to say just `dirty`, so that the line says only what is true.
11. As a developer whose root is detached, I want the caution line to say
    `detached`, so that a commit checked out by hash is never mistaken for a
    branch.
12. As a developer reading a Session row, I want the ticket abbreviated to its
    project initial and number, so that `F-2923` leaves the checkout label room
    to be read.
13. As a developer who needs the exact key, I want the full ticket ID in the
    SELECTED box, so that abbreviating on the row costs me nothing.
14. As a developer with Sessions that have no ticket, I want their rows to leave
    that column empty, so that nine columns are not spent telling me nothing on
    the rows with the least to spare.
15. As a developer who wants to know whether the harness has worked a ticket
    out, I want the SELECTED box to say `no ticket` for a Session about none, so
    that an empty row column is never ambiguous and never a placeholder key.
16. As a developer reading detail, I want the SELECTED box pinned to the
    Dashboard's foot, so that my eye learns one place to look for it.
17. As a developer whose working set grows and shrinks all day, I want the tree
    to absorb the slack above the SELECTED box, so that the box does not walk up
    and down the panel as Sessions start and end.
18. As a developer with more repos than fit, I want the tree to scroll around
    the cursor as it does today, so that pinning the box costs me no rows.
19. As a developer on a Session row, I want the SELECTED box to name the repo,
    so that the row saying `main` still leaves me knowing which repo's main it
    is.
20. As a developer glancing at the Dashboard, I want `GANYMEDE` in the mock's
    blue, so that the panel's name reads as the harness's own mark rather than
    as another bold row.
21. As a developer reading the panel's sections, I want `SELECTED` drawn quietly,
    so that a section label does not carry the same weight as the content under
    it.
22. As a developer working fullscreen in Ghostty with the menu bar hidden, I
    want the time in the Dashboard header, so that I can tell how long I have
    been at this without leaving the Dock.
23. As a developer reading that clock, I want it to change on the minute, so
    that I never act on a time that is half a minute stale.
24. As a developer with Sessions waiting on me, I want the Attention counts to
    keep their place beside the clock, so that adding a clock costs the counts
    nothing.
25. As a developer with a quiet working set, I want the Attention counts to stay
    absent, so that a count I always see is not a count I stop reading.
26. As a developer learning the harness, I want a key legend along the bottom of
    the Dock, so that every gesture is discoverable without hunting for the row
    that offers it.
27. As a developer reading that legend, I want it to name the keys that are
    actually bound, so that it never offers me `!` for a Popup shell that opens
    on `⌃\``.
28. As a developer who rebinds the focus or Popup key, I want the legend to
    follow the binding, so that it cannot drift into telling me something false.
29. As a developer on a specific row, I want the SELECTED box to keep offering
    only the keys that apply to that row, so that the legend's completeness does
    not cost me the box's precision.
30. As a developer scanning either list of keys, I want the key character itself
    to stand out from its label, so that I can find the key without reading the
    sentence.
31. As a developer looking at the Dock, I want the working client's status line
    in the harness's own colours, so that stock tmux green is not the loudest
    thing on a dark screen.
32. As a developer with several Ghostty windows open, I want the working
    client's status line to sign itself `ganymede`, so that I can tell a harness
    window from a plain terminal.
33. As a developer with nothing waiting on me, I want that status line to show
    the signature without a dangling separator, so that an empty Attention strip
    leaves no punctuation behind.
34. As a developer whose pane is Frozen or whose Popup shell is busy, I want
    those marks to keep their place on the row, so that a row's layout change
    does not cost me the marks I rely on.
35. As a developer moving the cursor, I want a caution line to never be
    selectable, so that `↑↓` still steps repo to repo and Session to Session.
36. As a developer with the cursor on a repo header, I want the header row alone
    to be highlighted, so that the inversion marks the row I am on rather than
    two lines.
37. As a developer whose Dashboard has lost focus to the working client, I want
    the dimmed selection to behave exactly as it does today, so that this change
    costs me nothing I already read.
38. As a developer with a Session outside every scan root, I want its row to
    still get a checkout label, so that a Session the working set never adopted
    is not the one row that reads differently.

## Implementation Decisions

### Modules touched

- **`internal/dashboard`** — every tree, header and SELECTED-box change. `View`,
  `header`, `tree`, `line`, `repoLine`, `carrying`, `selected`, `offering`,
  `repoOffering`, and the style block.
- **`internal/tmuxconf`** — the Dock's key legend (`dockBody`, written by
  `WriteDockConf`) and the working client's status-line styling (the `strip`
  const in the Sessions-server fragment).

No new packages, no new interfaces on `dashboard.Harness`, no state-model or
registry changes. Everything here is presentation over data the Dashboard
already holds.

### The checkout label on a Session row

The row's widest column becomes the checkout the Session has its hands on,
derived in this order:

1. The Session's checkout is its repo's Main root → `main`.
2. Otherwise → `wt·` + the base name of the **checkout** directory.
3. In case 2, when that base name begins with the ticket key the row is already
   showing, followed by a separator, drop the key and the separator — provided
   something is left. Worktree `FIRE-2841-paging` showing ticket `FIRE-2841`
   reads `wt·paging`; worktree `FIRE-2841` with nothing after it keeps
   `wt·FIRE-2841`.
4. Too long for the column → elided keeping the head, as names are today.

Derive from the **checkout**, not from the Session's directory: a Session whose
cwd is a subdirectory of a worktree would otherwise be labelled after that
subdirectory. `row.holdsRoot` already answers step 1 and `answers.checkout`
already supplies the directory for step 2 — both exist for Takeover and need no
change.

The Session's own name leaves the row entirely. For a Main root Session it is
Claude Code's auto-generated `<repo>-<xx>`, which is the redundancy this change
is about; for a Worktree session it is the worktree name, which is what step 2
draws anyway. Two Sessions sharing one checkout therefore read identically on
the row — the ticket and age columns are what tell them apart, and that is
accepted.

### The git caution line

A repo header carrying any caution gets one continuation line beneath it,
indented one column, in the existing amber:

```
focus-frontend                         ○
 ⚠ FIRE-2910-followup-sorting · dirty
```

Always its own line whenever there is a caution, never inline and never
adaptively — a header that reflowed between one and two lines as git changed
underneath it would be a header you cannot scan. `⚠ dirty` alone therefore also
takes a line, which is the price of the tree never reflowing.

Keep the existing graceful degradation (whole → elided branch → marks without
the branch → the bare mark), just against ~39 columns instead of the ~10 the
inline version had, so nearly every branch now fits whole.

The continuation line is **not a row**: `m.rows` still holds one entry per repo
header and per Session, `↑↓` still steps between them, and the inversion on a
selected header covers the header line only.

**Consequence worth stating plainly:** the tree's viewport arithmetic currently
assumes one line per row — both the `space` budget and the centre-on-the-cursor
calculation in `tree`. Once a row can occupy two lines, both must count lines
while the cursor keeps counting rows. Getting this wrong shows up as the
selection drifting out of view on a working set with cautions in it.

### The ticket on a Session row

Abbreviate to the project key's first letter, the hyphen, and the number:
`FIRE-2923` → `F-2923`. The SELECTED box keeps the full key, and `o` still opens
it. Two projects sharing an initial collide (`FIRE-1` and `FOCUS-1` both read
`F-1`) — accepted, since the full key is on the same screen in the box.

A Session about no ticket draws nothing in that column. SPEC.md §10's "render a
dim `no ticket` — never a placeholder key" is honoured in the SELECTED box,
which is where it goes on saying `no ticket` and where `t` is offered.

### SELECTED pinned to the foot

Pad the tree's block to exactly its line budget so the rule, the `SELECTED`
label and the detail always land on the panel's last lines. The existing rule
that the tree gives way before the detail does, when the panel is too short for
both, stays as it is.

On a Session row the box's identity line becomes the **repo** where it used to
be the Session's name — a straight substitution, so the box is no longer than
today:

```
█ Blocked · 4m
focus-service-ai-credit-usage
FIRE-2923
permission: Bash(git push)
Fix ready on fix/…, 42 files, asking to push
~/Projects/teamleadercrm/…/worktrees/paging
⏎ jump · y approve · n deny · t ticket
```

The Session's own name is dropped rather than given a line: it is either the
auto-generated `<repo>-<xx>` or the worktree name the row already shows, and the
directory line's tail identifies the checkout either way.

The repo-header form of the box is unchanged apart from the styles below.

### Header, styles and the clock

- `GANYMEDE` is bold in the mock's brand blue `#58a6ff`. Declare it as its own
  style with its own literal hex — do **not** derive it from
  `session.Working.Colour()`. The two happen to be the same triplet today; the
  brand is not a state and must be free to move without dragging Working with
  it. This follows how `ticketColour` is already handled.
- `SELECTED` drops to the panel's quiet style. It is a section label, not
  content.
- The header carries `HH:MM`, 24-hour, quiet, at the far end — after the
  Attention counts, as the mock has it: `GANYMEDE   █1 ●1 14:32`. The counts
  stay absent when nothing is waiting, and their absence must not leave a double
  space before the clock.
- The clock needs a redraw of its own, scheduled to the **next minute
  boundary** rather than riding the existing half-minute tick — that tick fires
  30 seconds after it last fired, so a clock hung on it can read up to half a
  minute late. This is a third clock alongside `ticking()` and `spinning()`, and
  it runs whether or not anything is animating.

### Key hints

In both offerings — a Session row's and a repo header's — the key character is
drawn in the panel's normal foreground and its label stays quiet, so the key is
findable without reading the phrase. Labels themselves are unchanged: `⏎ go to
repo` on a header row is doing honest work that `⏎ jump` would blur.

### The Dock's key legend

Turn the Dock server's status line on — it is `status off` today — and give it
the legend, full width along the bottom of the whole Dock. The Dock is the frame
holding both panes, which makes its own status line the only full-width row
there is; the mock's legend spans exactly that.

It is static text in `dockBody`, so it costs no harness write and no round trip.
Order it by what matters most, because tmux truncates a status line from the
right on a narrow window.

Two rules on its content:

- **Build the chords from the key constants.** `FocusKey` and
  `PopupToggleKey` already exist; a legend with its own copy of `M-g` would
  outlive a rebinding. Render them as a Mac user presses them (`⌥g`, `⌃\``)
  rather than in tmux's notation.
- **Correct the mock's legend against §7.3.** The prototype's bar is shared
  boilerplate across all four variants and is partly fiction: `!` is not the
  Popup shell key, `x` is interrupt rather than Takeover, and `q` ends a Session
  rather than quitting the Dashboard, which answers to no quit key at all.

A static legend necessarily lists keys that do nothing on the row you are
standing on, which cuts against the principle `offering` follows ("offering a
key that would silently do nothing is worse than not offering it"). That
tension is accepted deliberately, and the division of labour is explicit: the
legend is the **complete vocabulary**, for learning; the SELECTED box is the
**applicable subset** for the row you are on, and remains the authority on what
will actually fire.

### The working client's status line

The Sessions-server fragment sets only `status on` and `status-right` today, so
everything else is stock tmux — including the default green, which is the one
loud thing in the Dock. Style it in the harness's own palette, and sign the
right-hand end:

```
status-right "…attention… · ganymede"
```

The separator appears only when there is a count to separate, via a tmux
conditional on the attention option, so a quiet working set shows `ganymede`
with no dangling `·`.

The Dashboard goes on owning the whole of the attention strip's text, marks and
colours, exactly as it does now.

## Testing Decisions

A good test here asserts on **what the panel shows** — the rendered lines with
the styling stripped, read as the eye reads them — and never on how a line was
assembled. Both surfaces already have a seam for that, and this change adds
none.

### Seam 1: `dashboard.Model.View()`

Every tree, header and SELECTED-box change is tested through the bubbletea model
at the sidepanel's own width, using the helpers already in
`internal/dashboard/dashboard_test.go`:

```go
model := sidepanel(&jumps{}, blocked, working)
if !strings.Contains(tree(model), "wt·paging") { ... }
```

`sidepanel` builds a Dashboard at `topology.SidepanelWidth`, `drawn` strips the
ANSI so a test reads the panel as the eye does, and `tree` isolates the tree
from the detail box so a test about one is not satisfied by the other. Prior art
for exactly this shape: `active_test.go`, `roots_test.go`, `spin_test.go`,
`frozen_test.go`.

Colour and weight are the exception to reading stripped output — the brand blue
and the quiet `SELECTED` have to be asserted on the styled `View()`, the way
`active_test.go` already inspects the styled line for the selection's inversion.

What to cover:

- A Main root Session reads `main`; a Worktree session reads `wt·<name>`.
- A worktree named after its ticket drops the key from the label but keeps it
  when nothing would be left.
- A repo carrying a caution draws it on its own line, with the header's name and
  Main-root mark intact at full width; a clean repo draws no such line.
- A long branch that used to be cut now fits.
- `↑↓` steps over caution lines, and the inversion covers the header line only.
- A working set with cautions in it keeps the cursor in view — the line-versus-
  row arithmetic.
- The ticket abbreviates on the row and stays whole in the box; a Session about
  no ticket draws nothing on the row and `no ticket` in the box.
- The rule, `SELECTED` and the detail land on the panel's last lines for a tree
  of one row and a tree of forty.
- A Session row's box names the repo.
- The header shows `HH:MM` with the counts, and with the counts absent.
- Frozen and busy-Popup marks keep their place on a relabelled row.

### Seam 2: the installed tmux configuration

The Dock legend and the status-line styling are tested where the rest of the
configuration already is, in `internal/tmuxconf/tmuxconf_test.go`, against a
**real throwaway tmux server** loading the written file — `tmuxWithConf` plus
`show-options`. Prior art: `TestTheInstalledConfigKeepsTheStatusLineForTheAttentionStrip`,
`TestInstalledConfigBindsThePopupToggleKeysAtTheRootTable`.

Note that `WriteDockConf` and `dockBody` have **no test at all** today, so the
legend brings the first one. Model it on the Sessions-server fragment tests
above rather than inventing a shape.

What to cover:

- The Dock's status line is on and carries the legend.
- The legend names the chords the constants are actually bound to — change
  `FocusKey` in the test's expectation and the assertion is what fails.
- The working client's status line carries the harness's styling.
- `status-right` shows the signature alone when the attention option is empty,
  and the count, separator and signature when it is set.

## Out of Scope

- **A state word on Session rows.** The mock draws `█ Blocked`; the row keeps
  the bare glyph. §5's own text says "state glyph", the word costs about eight
  of forty columns, the glyph plus its colour is what the eye runs down, and the
  SELECTED box already spells the state out for the row you are on.
- **The `main` / `wt·` vocabulary in the working client's window list.** The mock
  shows `[service-ai-assistant] 1:main 2:wt·max-paging*`, but Claude Code
  renames its own tmux windows as it runs (documented in `topology.Spawn`), so a
  format over `#{window_name}` cannot hold. Doing it properly means the harness
  owning a per-window option, which is its own change with its own tension
  against §6's "window name = worktree name".
- **Ordering the quiet repos by recency.** §5 says "Attention tier first, then
  recency", and repos with no Session are alphabetical today. A real divergence,
  visible in the screenshot above, deliberately left for its own issue.
- **The sidepanel's width.** Forty columns stays; this change is about spending
  them better.
- **Anything in the working client's pane** beyond its status line's styling.
- **Attention ordering, the notifier, and the picker.** Untouched.

## Further Notes

- The mock is `~/Projects/plans/Ganymede-harness/prototype-dashboard-mock.html`,
  variant D (`#variant=D`). It is a throwaway prototype: its layout is
  normative per §5, its key legend is not, and its fake fleet dodges the
  ticket-in-worktree-name case the real one hits constantly.
- Vocabulary: CONTEXT.md lists "rail" among the words to avoid for **Dashboard**.
  The code's own comments use "the rail" for the repo tree throughout. This spec
  says Dashboard and tree; existing comments are not in scope to rewrite, but new
  ones should not add more.
- The freed columns net out in the Session row's favour. Today the checkout has
  22 columns after the indent, glyph, `no ticket` and age; after this it has 26,
  and it is spending them on something worth reading.
- Eight of eleven repos in the screenshot carry a caution, so the tree grows by
  roughly eight lines on a real working set. It still fits a 45-line panel, and
  the existing centre-on-the-cursor scrolling handles the case where it does
  not — which is exactly why the line-versus-row arithmetic above has to be
  right.
