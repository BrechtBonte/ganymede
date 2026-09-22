# Pulls: your open pull requests at the foot of the Dashboard

## Problem

The Dashboard claims to be the one place that says what needs you, and it is
not. It knows every Session in the working set and nothing at all about the
pull requests those Sessions exist to produce. A review someone asked you for
three days ago is invisible here; so is your own PR going red, going behind
its base, or becoming the one press that lands it.

`ARCHITECTURE.md` ruled this out permanently, and the reasoning was sound at
the time: "PR state lives in GitHub/`gh`; the harness only answers 'is the main
root free'." That ruling stood against a **build-health board** — a wall of
green and red checks, which is a different product and still out of scope.

What it also excluded, by accident, is the census: which of your pull requests
is waiting on a decision only you can make. That half is the same question the
rail already answers for Sessions, asked about the other half of the same
work. This design narrows the boundary rather than deleting it, and says so in
`ARCHITECTURE.md` rather than quietly contradicting it.

Every number in this document was measured on this account between 2026-09-18
and 2026-09-22, against the live GitHub API and real tmux. Where a decision
overrules the request that produced it, the measurement that overruled it is
given.

## Vocabulary

Eight terms landed in `CONTEXT.md` as of `ac6ba0b`. They are restated here
because the rest of this document is written in them; `CONTEXT.md` is the
source of truth, and it holds the glossary alone — no implementation detail.

**Pulls** — a section at the foot of the Dashboard: your open pull requests
from every repo on GitHub, in two lists, AUTHORED and REQUESTED. A row is a
**Pull**.

**Your move** — the Pulls whose state is yours to act on: `Rework`,
`Conflicted`, `Behind` or `Landable` in AUTHORED, and `Yours` in REQUESTED.

**Attention** keeps its existing definition and is now explicit that it covers
**Sessions only**. The map opened on the premise that "a PR assigned to you is
Attention"; that premise did not survive the state model, because three of the
nine states mean the move is explicitly *not* yours.

| AUTHORED | REQUESTED |
|---|---|
| `Draft` › `Rework` › `Conflicted` › `Behind` › `Landable` › `Sent` | `Draft` › `Approved` › `Yours` |

**`panel` is retired as a word.** It was never in the glossary, but the Go
comments use it throughout to mean the Dashboard — "the SELECTED box is the one
thing on the panel that is always in the same place" — and so does the
`panelLines` helper. Reclaiming it for this section would turn every one of
those into a trap, so it is on the `_Avoid_` line for both the Dashboard and
Pulls.

**The implementation is expected to sweep them**; that is execution, not a
decision, and a reviewer should not have to discover it. The scope, counted
fresh: `grep -roE '\bpanels?\b' --include='*.go' .` reports **68**, plus **13**
references to `panelLines`. (The naming ticket recorded 84; that figure does not
reproduce under any obvious pattern, and 68 + 13 is what the tree holds today.
The decision does not turn on the number.)

## Where it lives

**A section of the Dashboard, at the foot, in the place the SELECTED box
occupies. It is not a Dock pane.**

A third pane was the obvious shape and is arithmetically dead. Measured on
tmux 3.7b with real splits — a horizontal split spends a column on the border,
so the survivor is never what the arithmetic suggests:

| | 80-col window | 200-col window |
|---|---|---|
| *Today, two panes* | Dashboard 40 · working **39** | Dashboard 40 · working **159** |
| A — third pane right of the working client | 40 · **19** · 19 | 40 · 114 · 44 |
| **B — a section inside the Dashboard** *(chosen)* | working **39** untouched; section gets the sidepanel's **40** | working **159** untouched; section **40** |
| C — section takes the working client's place | section 39, Session hidden | section 159, Session hidden |
| D — `display-popup` over the whole frame | working 39; overlay 60 | working 159; overlay 150 |

At 80 columns option A leaves the working client on 19 columns, and there is
no width to ask for that fixes it — tmux halves the survivor.

**`reattachClients` changes nothing.** The Dock stays exactly two panes, so
`CONTEXT.md`'s two-pane definition holds and `a8e6c4f`'s reconciliation — which
deliberately clears any pane past the second, after a stray third shipped a bug
that survived restarts — is untouched. There is no section pane for the
reconciler to tell from a stray one.

> Recorded because the question was asked and the answer was cheap: a pane user
> option (`set -p -t <pane> @ganymede-role panel`) **does** survive
> `respawn-pane -k` and reads back in a format string, verified on tmux 3.7b.
> That is the identity `a8e6c4f` lacked when it had only position to go on.
> This design does not need it.

### The foot's dispatch

`selected()` (`internal/dashboard/dashboard.go:1910`) is already a priority
chain, and the row detail is only its last case:

```
spawning   → spawningView()      (w)
claiming   → claimingView()      (c — claim / release)
takingOver → takingOverView()    (c — Takeover confirmation)
setting    → ticket input        (t)
otherwise  → the cursor's row detail
```

The foot is not a detail display that happens to host inputs; it is a modal
surface whose default state is the detail, and four keys already displace it.

**Pulls becomes a fifth case in that same chain, below the four input flows and
above the row detail.** Pressing `c`, `w` or `t` while Pulls is open shows that
input in the foot exactly as it does today, and Pulls returns when the input
closes. No new mechanism, and the invariant the code states outright — "the box
is the one thing on the panel that is always in the same place" — survives.

**The cost, accepted:** while Pulls is open you cannot see the detail of the
row you are standing on. Pulls displaces precisely what the cursor is for.

### The space contract

At the default height of 45, chrome is 4 lines (5 with the update notice),
leaving **41 usable**. Today the foot takes 4–7 and the tree gets 34–37
(`space = m.height - chrome - len(detail)`, `dashboard.go:1435`).

**Pulls takes what it needs, capped at half the usable height, and scrolls
inside its budget when the cap bites.** At height 45 that is a cap of 20,
leaving the tree no fewer than **21 rows**.

Two reasons for a cap over letting the tree absorb Pulls the way it absorbs
`detail` today. It is count-agnostic, so it survives however many Pulls there
turn out to be. And the scrolling is not new machinery: `shown()`
(`dashboard.go:1579`) already solves "keep the cursor visible inside a line
budget" for any budget down to a single line, which is also why a tree squeezed
by Pulls scrolls harder rather than breaking.

### The label

`View()` renders the literal `"SELECTED"` at `dashboard.go:1451`, **outside**
the dispatch chain — so today the label already lies during `c`, `w` and `t`.
Pulls makes it dispatch-dependent and reads `PULLS`, quiet and bare, in the
same weight as `SELECTED`, for the reason the code already gives at
`dashboard.go:1449`: "a section label weighted like its own content is one more
bold row for the eye to read past."

**The label costs no rows.** `chrome := 4` covers the header, both rules and
the label, and `space = m.height - chrome - len(detail)` — the label sits in
chrome, not in the foot's content.

**The other four labels keep lying, and that is stated rather than hidden.**
Making the label dispatch-dependent for one case of five while four keep
hardcoding `SELECTED` is an odd shape, and fixing all four was offered during
naming and declined: the lie predates this work and does not belong to it.

## What it shows

**Two sets in two sections — `review-requested:@me` and `author:@me` — from any
repo on GitHub, with no repo filter at all.**

### The sets

`assignee:@me` is **not** a third set. Measured: it returns 7 PRs, and they are
the same 7 that `author:@me` returns, every one authored by `BrechtBonte`.
GitHub's PR assignee carries no independent meaning in this workflow, so it is
dropped rather than deduplicated.

That leaves the reviewer set — someone waiting on your decision — and the
author set, which is waiting on you the moment a review bounces back or the
branch conflicts. Without the author set the Dashboard could never tell you
your own PR went red, and nothing else in the harness would.

The two roles are structurally disjoint: GitHub will not request a review from
a PR's own author, so there is no dedupe rule to write.

### The qualifier is `review-requested:@me`, never `user-review-requested:@me`

This inverts the obvious guess, and getting it wrong would have been invisible.
Measured:

| Qualifier | Count | What came back |
|---|---|---|
| `review-requested:@me` | 8 | 3 human (`teamleadercrm/core` #48032–48034) + 5 dependabot |
| `user-review-requested:@me` | 5 | **all five dependabot** |

Every human review request on this account arrives as a **team** request, which
only the broad qualifier matches. The narrower-looking qualifier builds a
section that hides every person asking you for something and shows only version
bumps.

### Sections by role, never by repo

**Two lists, AUTHORED then REQUESTED, each carrying its own count. The repo
rides on the row. There are no repo group headers.** Rows sort by repo, then by
number inside each list, which keeps a stack contiguous.

| Shape | Lines on the measuring day |
|---|---|
| **Two role sections** | 2 headings + 17 rows = **19** |
| Repo group headers | 7 headings + 17 rows = 24 — **over the 20-line cap** |

Role sections also match the meaning: one list is a decision you owe someone,
the other a decision owed to you. Those are different urgencies and do not
belong in one ordering.

The names are `AUTHORED` and `REQUESTED` — 8 and 9 characters inside 40 — and
they name the two GitHub qualifiers behind them, so the heading and the query
agree. The earlier `MINE`/`REVIEW` pair was dropped because **MINE speaks as
you while every other line the harness draws speaks to you** ("waiting on
you", "asked of you", "Your move"), and because the pair was parallel in
neither voice nor part of speech.

**19 of 20 lines are spent before the section ships**, so scrolling is
load-bearing from the first day rather than a safety net.

### Which repos: all of them

**GitHub decides membership. The harness never narrows by repo.**

A working-set filter was measured and rejected on the numbers: it drops **5 of
15 rows** — two of them your own PRs awaiting review, plus three review
requests in repos you had not opened in over a week. The working set was 15
repos at the time; the inventory is 103 checkouts. A section that hid a review
request because you had not touched that repo recently would break the one
claim this design is built on.

Two consequences:

- **Repos are named from GitHub's `nameWithOwner`**, not from the harness's
  label — it has to be, for a repo it holds no path for.
- **A repo with no checkout needs no special rule.** Acting on a PR is out of
  scope; opening it in a browser behaves identically with or without a clone.

### Bots, drafts, and what `is:open` does not guarantee

- **Dependabot PRs are rows like any other** — 5 of 8 review requests on the
  measuring day. Filtering them would make the count disagree with GitHub's own
  and bury a dependency PR you actually need to merge. Pulls reports your
  census; it does not curate it.
- **Drafts are included and marked.** A draft PR is not waiting on you, but its
  row is what keeps a live Session on a draft branch from having no Pull.
- **Open only** (`is:open`) — with a caveat measured the hard way.

**GitHub's search serves merged PRs as open.** Two PRs in `teamleadercrm/prqa`
were merged at 14:32:22Z and 14:32:32Z and kept coming back for
`is:pr is:open author:@me` for roughly **two hours**. This was first read as a
lag in indexing new PRs and is the opposite. **No state in this design claims
to have verified that the PR still exists in the state the row reports**, and
none should: Pulls is a viewer whose only action is opening a browser, so a
stale row costs a wasted click. There is no verification pass.

### The query

Both lists ride in **one HTTP request for 1 rate-limit point**, as two aliased
`search` fields:

```graphql
query($review: String!, $mine: String!) {
  rateLimit { cost remaining }
  review: search(query: $review, type: ISSUE, first: 20) {
    issueCount
    nodes { ... on PullRequest {
      number title url isDraft baseRefName headRefName updatedAt
      mergeable mergeStateStatus reviewDecision
      author { login }
      repository { nameWithOwner }
      statusCheckRollup { state }
    } }
  }
  mine: search(query: $mine, type: ISSUE, first: 20) { … same shape … }
}
```

with `review="is:pr is:open review-requested:@me"` and
`mine="is:pr is:open author:@me"`.

**Measured, 3 runs:** `cost=1`, **4.4–5.6 s** for 17 PRs carrying every field
including `mergeStateStatus`. That is half the ~10.5 s gateway ceiling the
research hit on a ten-repo query, and the reason is this decision: scoping by
*you* keeps the result set small, where ten `repo:` qualifiers did not. Page at
`first: 20`.

`headRefName` is in the field set and is **free — measured `cost: 1`
unchanged**. It pays for two things: the stack parent, and matching a Pull to a
Session.

Every field-level finding behind this query — including that `reviewDecision`
has no REST counterpart at all, which alone settles GraphQL over REST — is in
`docs/research/github-pr-fields.md` on branch `research/github-pr-fields`.

## The state model

**One state per row, answering whose move it is, plus marks that are orthogonal
to it. The two lists carry different vocabularies.**

### Why the three requested states are not the model

The original request named ready-to-merge, merge-conflicts and
changes-requested. Measured against the real population:

| Requested state | Instances |
|---|---|
| Ready to merge | **0 of 15** |
| Merge conflicts | **0 of 15** — every PR `MERGEABLE` |
| Changes requested | **0 of 15** |

What is actually there: **13 of 15 at `mergeStateStatus: BLOCKED`** and **11 of
15 at `statusCheckRollup: PENDING`**. Both are the resting state of a healthy
PR under this org's branch protection, so neither discriminates anything. A
model built on the three named states would have rendered the whole section as
one undifferentiated block and still had no word for the one real collision in
the data — a PR **approved with checks failing**.

### AUTHORED — six states, first match wins

| State | Meaning | Primary signal |
|---|---|---|
| **Draft** | Nothing is asked of anyone; you marked it unfinished | `isDraft` |
| **Rework** | A reviewer has asked for changes — your move | `reviewDecision: CHANGES_REQUESTED` |
| **Conflicted** | Cannot merge mechanically — your move | `mergeable: CONFLICTING` (`mergeStateStatus: DIRTY`) |
| **Behind** | The base has moved under it — your move | `mergeStateStatus: BEHIND` |
| **Landable** | You can press merge now — your move | `mergeStateStatus: CLEAN` or `HAS_HOOKS`, **and** the base is the default branch |
| **Sent** | Nobody's move but the reviewer's | fallback |

**Priority is the table's own order, top to bottom.**

- **Draft outranks everything.** A draft is in nobody's queue, so nothing it
  carries is urgent — you will resolve the conflict before marking it ready.
  The alternative nags you about work you have explicitly declared unfinished.
- **Rework outranks Conflicted.** When a reviewer has asked for work you are
  rewriting the code anyway; the rebase comes afterwards, so the social block
  names your next move. The two genuinely co-occur — `Conflicted` reads
  `mergeable` while `Behind` reads `mergeStateStatus` — and the chain is what
  settles it rather than an assumption that they are exclusive.

### REQUESTED — three states

| State | Meaning | Primary signal |
|---|---|---|
| **Draft** | Converted to draft while its request stands | `isDraft` |
| **Approved** | Someone else has approved; yours is a second opinion | `reviewDecision: APPROVED` |
| **Yours** | You owe this review | fallback |

Membership in `review-requested:@me` already asserts that GitHub thinks you owe
a review, so the fallback needs no signal of its own. `reviewDecision: null` —
which the research warns must be read as "nobody has asked for a review" rather
than as missing data — cannot occur here for that reason.

> **`Yours` and `Your move` share a word on purpose.** `CONTEXT.md` defines the
> category as the union of its five members, one of which is `Yours`, so the
> repetition teaches the category. The snag is written down rather than dodged:
> in AUTHORED every Pull is yours by authorship, so a reader can take `Yours`
> for ownership before they take it for the move.

Two of the nine words were forced by collisions with existing Session states.
`ready to merge` became **Landable** because `Ready` already means "the turn
finished and you have not seen the output yet". `in review` became **Sent**
because the word has to mean "not your move" across all three of its
sub-cases — nobody has looked, a reviewer is looking, approved with CI still
running — and both `in review` and `Theirs` assert a holder, so they lie in the
first and third. `Sent` claims nothing about who holds it: you did your part.
`Blocked`'s own `_Avoid_` line rules out *waiting* and *pending*.

### Marks — orthogonal, never states

| Mark | Source |
|---|---|
| `✗` | `statusCheckRollup.state` is FAILURE or ERROR |
| `✓` | `reviewDecision: APPROVED`, in AUTHORED only |

**The check marks draw only a failure**, which amends the original model. Drawn
literally — `✓ ✗ ◷` for SUCCESS, FAILURE and PENDING — that is a mark on **13
of 17 rows**, 6 of one day's 11 being `◷` alone. PENDING and SUCCESS are both
resting states, and a mark two rows in three carry is a mark the eye stops
reading. Drawing only `✗` leaves 4 marks instead of 13, **and it frees `✓` to
mean exactly one thing: the approval a person gave.**

That is what resolves the one real collision without a priority fight. A PR
approved with checks failing is not a contest between "approved" and "failing"
— it is `Sent` carrying a `✓` and a `✗`.

**One tension, accepted:** a PR of yours with failing checks reads `Sent`, not
as your move. That follows the boundary — a red check is your PR's problem, not
a decision waiting on you. The `✗` is there to be seen; it does not promote the
row.

### Stacked Pulls are a mark, not a weight

The original request asked that a PR based on something other than the default
branch be "slightly less visible". **Rejected on measurement.** The only
non-default-base PRs were a true four-deep stack from a colleague — and the
*only* three rows in the whole section with a person waiting behind them.
Dimming them would have dimmed exactly those and left five dependabot bumps at
full weight.

Instead:

- **The row carries a bare `↳`.** It does not name the parent — see below.
- **`Landable` is withheld on a non-default base.** `CLEAN` there means
  "mergeable into an unmerged parent", which is not the action you want; the
  row falls through to `Sent`. This is the reason stacked Pulls are genuinely
  different, and the request had not named it.
- **Stacking is not an edge case: 4 of 17 rows.**
- **The parent resolves for free**, by matching each row's `baseRefName`
  against the other rows' `headRefName` — a local join, no extra request.

**Why the mark stays bare**, amending the original model's "the row says what
it is stacked on": `↳#48032` is 7 columns, and because the identity column is
measured against the widest tail in the section (below), those 7 columns come
off **every row**, not off the 4 that are stacked. What they buy is small — the
lists sort by repo then number, so a stacked Pull's parent is the row directly
above it in **3 of the 4 cases**, and in the fourth the parent is not in the
section at all and its branch name is 30 columns. One column says everything
the row can honestly say; `o` says the rest.

### `mergeStateStatus` is kept

It is the only source for `Behind` and the only exact source for `Landable`.
The research clocked it at roughly triple the wall-clock on 40 PRs across 10
repos, but at this section's real scale the measurement is **4.4–5.6 s with
every field included** — about two seconds on a thirty-minute poll. Dropping it
would reduce `Landable` to a proxy (`APPROVED` + `SUCCESS` + `MERGEABLE`, which
cannot know whether branch protection is satisfied) and delete `Behind`
outright. Both states survive because the field is affordable *here*, not
because it is cheap in general.

## The row

**Identity on the left, state right-aligned at the far end — the row every
other row on the Dashboard already is.** Nothing here is new machinery; it is
`line()`'s own shape with a different tail.

```
 …us-service-bookkeeping#1005  Approved ✗
 ↑ ↑                     ↑     ↑        ↑
 │ the repo, tail kept   #num  state    marks
 one column of indent
```

The section as the mock draws it, on the 11 Pulls this account had open at
08:12 on 2026-09-22:

```
PULLS                              08:12
AUTHORED 1
 …us-service-bookkeeping#1010      Draft
REQUESTED 10
 api-internal#1564                 Yours
 focus-frontend#7430            Approved
 …s-service-ai-assistant#1066      Yours
 …s-service-ai-assistant#1104      Yours
 …ervice-ai-credit-usage#279       Yours
 …us-service-bookkeeping#979     Yours ✗
 …us-service-bookkeeping#1005 Approved ✗
 …eloper-portal-frontend#953     Yours ✗
 …eloper-portal-frontend#956  Approved ✗
 …e-insights-datasources#946       Yours

⏎ jump · o open · r refresh · esc close
```

### There is no title

| | rows the section holds | title |
|---|---|---|
| **A — identity left, state right** *(chosen)* | **17** | none |
| B — state column on the left | **17** | none |
| C — two lines per Pull | **8** | 37 columns |

40 columns hold the repo, the number, the state and the marks with 22 left for
the repo name. A title needs 37, so it costs a second line per Pull and the
section drops from 17 Pulls to 8 — scrolling from the first row rather than
from the 18th.

**And the title fails exactly where it would be needed.** The three stack rows
read:

```
 core#48032                       ↳ Sent
   [PHX-4335] Decode the search term f…
 core#48033                       ↳ Sent
   [PHX-4335] Decode the search term f…
 core#48034                     ↳ Sent ✓
   [PHX-4335] Decode the search term f…
```

Three identical lines. Those titles, like their branches, differ only past
column 37 — which is the one case where you would be reading a title to tell
rows apart. Paying 9 of 17 rows for that is the whole argument.

### The repo keeps its tail, and the column is measured over the section

**`elide()` is the obvious helper and the wrong one.** It keeps a name's head,
which is right for a worktree carrying its ticket and wrong for this org's
repositories: `focus-service-ai-assistant` and `focus-service-ai-credit-usage`
both come out as `focus-service-ai-…`, and both are on the section today.
Keeping the **tail** — `tail()`, the helper the Dashboard already has for paths
— reads `…-service-ai-assistant#1066` against `…rvice-ai-credit-usage#279`.

The cost is a leading `…` on 8 of 11 rows, noise on the very edge the eye runs
down. It is the price of rows that name different things differently.

**The identity column is the width of the widest tail in the section, measured
once — not per row.** Elided against whatever each row's own tail left over,
`#979` and `#1005` in one repository came out as `focus-service-bookkeep…` and
`focus-service-bookkeeping`: the same repo reading as two different ones.

### A row with no state draws `—`

A row still `UNKNOWN` after the second pass draws `—` where the word goes, not
a gap:

```
 …us-service-bookkeeping#1010          —
```

An empty column reads as a rendering that failed; the dash reads as an answer
the harness does not have. **It is not a tenth word** — it is drawn, not said,
and it appears in no vocabulary.

### Scrolling, and the two highlights

The 20th row is the key legend at 39 of 40 columns, so there is nowhere *in*
the section to put a scroll marker. It rides the chrome line beside the fetch
time, where it costs no row and is never itself scrolled away:

```
PULLS                           ▴2 ▾9 14:31
```

The list heading the window opens inside is redrawn at the top, and every
heading carries its list's count — `REQUESTED 11` — so the count and the window
together say what is out of sight.

**The two highlights are the rail's own pair**: `selectedStyle` for the
cursor's row, `blurredSelectedStyle` — the same inversion, faint — for the Pull
the working pane's Session is sitting on. Those are the two marks `line()`
already draws for `m.cursor` and `m.active`, so the section and the tree say
"here you are" and "here you were" the one way. No rendering is invented, and
the cursor case draws both at once.

## Fetching

**One `gh api graphql` subprocess, a two-pass cycle every thirty minutes,
nothing remembered across restarts, and silence ruled out as a rendering.**

### `gh` as a subprocess, and a new hard dependency

`gh api graphql` — not `net/http` with a token of the harness's own, and not
`gh auth token` feeding an HTTP client.

The deciding argument is the failure modes, not the convenience. Three
outcomes have to be told apart, and they are `gh`'s own diagnosis:

| Situation | Exit | Distinguished by |
|---|---|---|
| Never logged in | `4` | exit code alone |
| Token expired or revoked | `1` | `HTTP 401` on stderr |
| Network | `1` | absence of `HTTP 401` |

A raw 401 from an HTTP client cannot tell a revoked token from a changed scope,
so rolling our own means re-deriving what `gh` already knows. The per-fetch
process start is spent twice per thirty minutes against a 15–25 s cycle; it
does not register.

**`gh` becomes a hard runtime dependency of the Dashboard**, on the footing
tmux and `claude` are on. It was not one before — a machine with the harness
and no `gh` runs today. This is accepted explicitly, and it is also why the
harness holds no credential: it inherits the login you have already granted.

### A cycle is two passes

GitHub computes mergeability on a background test-merge commit and serves
`UNKNOWN` until it lands. Measured: `cli/cli` went from 0/30 `UNKNOWN` to
**14/30 in twenty minutes**, because the test merge commit is invalidated
whenever the base branch moves — so a thirty-minute poll arrives cold every
time. A re-read five seconds later resolved 30/30 and held at +10, +15, +20 s.

A cycle is therefore: query both lists, collect the rows that came back
`UNKNOWN`, wait five seconds, re-read just those, repaint.

**It cannot be one pass.** `mergeable` and `mergeStateStatus` go `UNKNOWN`
**together**, and they are the primary signals for `Conflicted`, `Behind` and
`Landable`. An unresolved AUTHORED row falls through the whole chain to
`Sent` — so the three states that mean *Your move* collapse into the one that
means the opposite. This is the harness's own "a notice that is wrong is worse
than one that is late", applied to a row. REQUESTED is untouched: `isDraft` and
`reviewDecision` never go `UNKNOWN`.

**Atomic, not progressive.** A progressive paint — everything at ~6 s, the
unresolved rows settling at ~12 s — buys six seconds exactly once, at Dashboard
start, and charges for it permanently: a provisional rendering seen for five
seconds every half hour, and a section carrying two ages at once when the
chrome line has to state one. In the steady state the twelve seconds are
invisible, because the section spends them showing the previous cycle.

### Nothing is remembered across restarts

Not the set, not the timestamp. Every Dashboard start fetches, and
**`state.json` gains no section** — deliberately, not by oversight.

`release.Remembered` keeps `latest` because a published version is the same
answer an hour later, and because its ten-hour window protects a budget.
Neither holds here. A Pull decays inside the poll interval, and the rate cost is
**4 points per hour against 5,000** — 0.08% — so there is nothing to be polite
about. Memory would buy politeness that is not needed and pay in accuracy,
which is the sharpest failure already measured here: search served two merged
PRs as open for two hours. A set replayed from disk at start is that failure
with a longer fuse.

It also keeps `state.json` what `ARCHITECTURE.md` says it is — claims, notes,
ticket overrides, popup ownership, seen tracking: the harness's own decisions,
not a cache of a third party's data, in a file whose own code comment calls it
one "somebody may well end up reading".

Remembering only `checkedAt` was rejected outright: a restart five minutes in
would show an empty section for the remaining twenty-five.

**The price:** Pulls has an unfilled state at every Dashboard start, roughly
twelve seconds. It needed a rendering anyway, since the first cycle of any
session has one.

### A row still `UNKNOWN` after pass two

It keeps its number, repo and marks — all from fields that never go `UNKNOWN` —
and **draws no state word**. Two consequences taken with it:

1. **A stateless row is not Your move**, so it marks no Session row. It
   undercounts rather than overcounts, which is the direction a wrong answer
   should err in.
2. **No third pass.** The next cycle picks it up. Building retry machinery for
   a case never once observed is speculation, and the row is honest meanwhile.

**No tenth state, and no `CONTEXT.md` word is due.** The nine all answer one
question — whose move is this. "We could not tell" answers a different one, and
it would put GitHub's lazy test-merge commit into a glossary that holds no
implementation detail. Every other word there survives a different forge; this
one would not.

### The age is on the chrome line, and silence is ruled out

**"Stale" is not the word, and there is no word.** `CONTEXT.md` lists "stale"
under `Behind`'s *Avoid*, so using it for an old fetch would give one word two
meanings inside one section.

The fetch time rides at the right-hand end of the `PULLS` label — the chrome
line, which costs no rows. That is the shape `header()` already uses
(`spread("GANYMEDE", joined(m.counts(), m.clock()), m.width)`), and the
Dashboard's own clock sits a few lines above. Header 16:45, Pulls 14:31: two
hours old has been read without a word for it, a threshold to cross, or a line
spent.

**The update check's answer does not transfer.** Its silence reads as "you are
up to date", true on nearly every day. An empty Pulls reads as "you have none",
which on the measuring day is false seventeen times over. So four bodies have
to be visibly different from each other:

| Situation | Signal | The section says |
|---|---|---|
| Nothing fetched yet | first cycle in flight | that it is fetching — never blank |
| Genuinely no Pulls | fetch succeeded, both lists empty | so, with the time on the chrome line proving it fresh |
| Cannot fetch | exit 4, or exit 1 with `HTTP 401` | so, and names `gh auth login` |
| Cannot fetch | exit 1 without 401 | GitHub unreachable; the last good rows stay |

The first three replace the lists. **A network failure does not** — the last
good rows stay under their own timestamp and one row carries the reason:

```
⚠ Unreachable — retrying 5m
AUTHORED 1
 …us-service-bookkeeping#1010      Draft
```

**Two clocks.** A network failure retries at **five minutes**, not at the end of
the window — the instinct behind `release`'s 30-minute `Retry` against a
10-hour `Every`, scaled. An auth failure drops to the **plain thirty-minute
poll and does not stop**. This last one overrides the research doc, which
recommended halting after a 401 on the grounds that `gh` never refreshes
itself: true, but stopping would strand the section until a Dashboard restart,
where thirty minutes costs nothing and lets a `gh auth login` in another pane
heal it without one.

### The clock runs whether or not the section is drawn

Not a preference but a consequence of the Session mark: the **Your move** mark
lives in the tree, and the tree is always drawn. Collapsing Pulls stops it
drawing rows, never fetching.

## Keys

**`p` from the Dashboard and no global chord; `p` moves the cursor into Pulls
and `esc` brings it back; `o` and `⏎` keep one meaning each and gain a
subject; nothing is remembered and nothing but you opens or closes it.**

### `p` opens it, `esc` closes it, closed means gone

**No fourth global chord.** `C-]`, `` C-` ``/`` M-` `` and `M-g` are each bound
at tmux's root table — taken from every pane of every Session permanently, each
justified in a paragraph of `tmuxconf.go` about why nothing else needed it.
Pulls is a census you glance at, kept right by a thirty-minute clock; arriving
one keypress sooner buys nothing, and `⌥g` is already the answer to "I want the
Dashboard".

**`p`** is free — the only runes bound on the Dashboard are `o`, `t`, `g`, `w`
and `c` — and every Dashboard key is already the first letter of what it does.
It toggles, and **`esc` also closes**, because every other thing that takes the
foot answers to `esc`; the SELECTED box's own line reads `⏎ set · esc cancel`.

**Collapsed is fully hidden.** A one-line spine carrying a count was rejected
twice over: it costs one of the 21 rows the tree is guaranteed, paid in the
common case for the rare one — and the tree already answers "is there anything
in Pulls", because the Session marks are drawn whether Pulls is open or not.

### One cursor at a time

`p` moves the cursor **into** Pulls. While it is open `↑↓` drives its rows and
the tree keeps its highlight but freezes; `esc` or `p` puts the cursor back
exactly where it was.

Two cursors with a focus-switch key was rejected: it spends a key the legend
cannot afford, needs a focus indicator inside 40 columns, and leaves `↑↓` doing
two things with nothing loud saying which. Leaving `↑↓` on the tree was
disqualified outright — with no cursor in Pulls there is no selected Pull, and
opening one is half the point.

**Losing the SELECTED box while Pulls is open is acceptable, and nothing
auto-closes.** `c`, `w` and `t` stay live and act on the frozen tree row,
because each opens a flow that names its subject before anything happens —
`claimingView`'s first line is `elide(c.label, m.width)`, the root's own label,
and the flow's `⏎ claim · esc cancel` is the confirmation. You never act blind;
you act on a row whose highlight never moved.

### `o` and `⏎` gain a subject, and no key is added

| Key | Its one meaning | On a tree row | On a Pull row |
|---|---|---|---|
| `o` | open in the browser | the JIRA ticket | the pull request |
| `⏎` | put this in front of me | the Session or repo | the Session the Pull belongs to |

`⏎` reuses the Session match in the other direction, for free, and it is the
one genuinely useful gesture here: a Pull in **Rework** is a row you want to be
standing *in*, not reading about. When no Session matches — measured at 3 of 15
— it does nothing and sets a notice naming `o`, mirroring `open()`'s existing
`"no ticket — press t to set one"`.

**`internal/browser` needs nothing.** `Browser.Open(url string)` takes any URL;
`ticket.Tickets.Open` merely builds the JIRA address before calling it, and a
Pull carries its own `url`.

`r` runs a whole cycle by hand — both passes — and resets the window, so a
refresh at 14:29 does not get a second at 14:31. It is a **no-op while a cycle
is in flight**, so holding it cannot stack twelve-second cycles, and it is the
recovery path out of an expired login. It earns its place on an argument
specific to this harness: **Pulls is the first thing the harness knows that
nothing can push to it.** The registry watch is a file watcher, hooks are
sub-second edges, the reconciler cross-checks something local — each learns of
a change because the change announces itself. GitHub does not.

### Nothing is remembered, nothing opens or closes it but you

A restored "open" flag would collide head-on with remembering nothing: a
Dashboard restarting into an open Pulls opens onto the *fetching* body, because
the first cycle takes roughly twelve seconds. You would be restoring a view of
nothing. It also matches what the sidecar has always held — decisions and
observations. **No view state has ever survived a restart**: not the cursor,
not an open picker, not a half-typed input.

An empty list does not close it — "no Pulls" has its own body, and auto-closing
would make `p` look broken on a quiet day while hiding the one rendering that
proves the fetch worked. No width or height threshold does either: the
sidepanel's width is fixed by the Dock's topology and the height cap is
proportional, so it degrades with no threshold to cross.

### The legend, measured against the live `dock.conf`

`p pulls` goes in the **chords group**, after `` ⌃` popup shell `` and before
`w spawn`:

| Key | today | with Pulls |
|---|---|---|
| `↑↓ select` · `⏎ jump` · `⌥g focus` · `` ⌃` popup shell `` | 9 · 18 · 29 · 46 | unchanged |
| `p pulls` | — | **56** |
| `w spawn` | 56 | 66 |
| `c claim/release/takeover` | 83 | 93 |
| `t ticket` | 94 | 104 |
| `o open` (was `o open ticket`) | 110 | 113 |
| `g repo picker` | 126 | **129** |

**At an 80-column Dock this is a strict gain — six keys visible instead of
five.** `c`, `t`, `o` and `g` were already past the edge at 80 and still are;
`p` lands at 56 and displaces nothing. At 200 the whole legend fits with 71
columns spare. **Net cost: three columns**, because `o open ticket` shortening
to `o open` handed seven back.

The placement follows `legendKeys`'s own rule — movement first, then the chords
nothing else advertises, because no row is ever standing on them. No row is
ever standing on `p` either.

**The section's own keys stay off the legend** and ride on its last line, the
way `claimingView` ends with `⏎ claim · esc cancel`:

```
⏎ jump · o open · r refresh · esc close
```

39 of the 40 columns. The split follows the legend's own rule that "offering a
key that would silently do nothing is worse than not offering it": `p` always
fires, `r` fires only inside Pulls, and Pulls is on screen exactly when `r` is
live.

**Cost:** that line takes the 20th row — the one left spare at 2 headings + 17
rows = 19. On the measuring day the section is exactly full and the 18th Pull
scrolls. Scrolling was already load-bearing from day one; this makes it true
one row sooner.

## Matching a Session to its Pull

**`origin`'s `nameWithOwner` plus the head branch is the whole rule, and the
Session row's mark says Your move or says nothing.**

### Branch alone is dead

**5 of 15 Pulls share a head branch with a Pull in a different repo.**

| Head branch | Pulls |
|---|---|
| `security/bump-fast-uri-3.1.8` | three repos |
| `dependabot/composer/guzzlehttp/guzzle-8.2.0` | two repos |

A checkout was sitting on the first of those at the time of measuring, so a
branch-only rule would match one checkout to three Pulls in three repos.

### The repo comes from `git remote get-url origin`

GitHub gives `repository { nameWithOwner }`. The harness has no equivalent —
a repo is a path labelled with `filepath.Base` (`internal/dashboard/rows.go:112`).
Three ways to make one, measured across the 103 clones:

| | Cost | Wrong on |
|---|---|---|
| **`git remote get-url origin`** *(chosen)* | ~15 ms per root, once per process | nothing; it is the same string GitHub returns |
| the `~/Projects/<org>/<repo>` convention | free, no git | **5 of 103**, and **invisibly** |
| the directory's basename | free | every cross-org collision, silently |

The convention holds for 98 of 103 and fails silently on the rest: a clone
under `presentations/` would be matched against `teamleadercrm/claude-code`, a
repo that does not exist, and the row would simply never light up —
indistinguishable from having no Pull.

Read **once per Main root and cached for the process lifetime**. A repository
whose remote is re-pointed while the Dashboard is up stays stale until restart,
which is the trade for never asking twice.

**This makes one of the earlier sentences false, and the correction matters.**
Not filtering by repo was partly justified on "no `owner/repo` ever has to be
derived from a git remote, which the harness has never done". True of the
fetch; matching breaks it. **What survives is the boundary that matters**:
`git remote get-url origin` reads `.git/config`, so nothing about the network
boundary changes here — it is the fetch that makes the harness's outbound-call
sentence false, and that is redrawn below.

### The rule, and its six cases

A Pull and a Session's checkout are the same work when
**`repository.nameWithOwner` equals the `origin` of the checkout's Main root,
and `headRefName` equals the branch the checkout is on.** Both halves are
already in hand — `repo.Branch(dir)` is read for every Session to derive its
ticket (`internal/ticket/tickets.go:91`) — so **the join costs no request**.

The rule is about the **checkout**, not the process. Two Sessions sharing one
checkout are both on that branch, so both rows are marked, which is right: the
mark is a claim about the working directory.

| Case | What happens |
|---|---|
| Worktree branch never pushed | No Pull has that head. No match, no mark. |
| Main-root Session on the default branch | `master`/`main` heads 0 of 15 Pulls, and there is **no special case for it** — a `master → release` Pull would light the main root up, correctly. |
| Two open Pulls from one branch | The mark fires if **either** is Your move; the row never picks. Pulls highlights both. |
| Branch whose Pull is closed or merged | `is:open` means it is not fetched. With the measured caveat: a merged Pull keeps marking its row for up to two hours. |
| Two Sessions on one branch in different checkouts | Both rows carry the mark. Git forbids two worktrees of one repo on one branch, so in practice this is two Sessions in one checkout. |
| A Pull in a repo with no Session | Nothing matched, nothing highlighted. Pulls shows the row; the tree says nothing. |

### The mark says Your move, and nothing else

**One mark — `◆`, one column — drawn only when the matched Pull is Your move.**
Silent otherwise, including for a `Sent` Pull, a `Draft`, and no Pull at all.

Not existence, because existence is measurably empty of information: of the 3
Pulls with a checkout on their head branch, **all three were `Sent`**, and **0
of 8 authored Pulls were Your move**. An existence mark would have put a glyph
on three rows and named no action.

Your move earns its columns because those four AUTHORED states are the ones you
act on **in that checkout**: `Conflicted` and `Behind` are rebases in that
working directory, `Rework` is code you write there, `Landable` is the one
press. It also reuses a term `CONTEXT.md` already carries rather than minting a
row-level vocabulary, so **no new word is due**.

**It is not an AUTHORED-only mark.** REQUESTED scored 0 of 7 on the measuring
day — no colleague's branch was checked out — but that is the state of an
afternoon, not a structural fact. `CONTEXT.md` calls a Main root "the directory
PR reviews happen in", and a main root on a colleague's branch carries a
REQUESTED Pull in state `Yours`, which is Your move, and is arguably the case
the mark was invented for.

**Two costs, both accepted.** The mark is dark on every row in the current
working set, so it will not be seen working until a Pull goes `Behind`. And "no
Pull" and "`Sent` Pull" read identically on the row — correct for a tree whose
entire ordering rule is what is asking something of you, but a real loss.

### Where it sits, and what it never does

**At the far right end of the tail, after the age.** `spread()` right-aligns
the tail at column 40, so the mark lands in **the same column on every Session
row** however wide the ticket and the age are. That is the harness's own stated
reason for where a repo header's root mark sits. It costs 2 of the 25 columns a
worktree label already elides into, taking them to 23.

It also keeps `marks()` meaning what its docstring says — what *you* have done
to a row, frozen and popup — where a Pull is something the world has done.

**Header rows never carry it.** A repo can sit on the rail with no live
Session, and its header row is the Main root: the mark's claim is that the work
in *this checkout* has something waiting on you, and a repo with nothing
running in it has no work in flight. The header's far-right column is also
spoken for — it carries the Main root's state.

**A Your-move Pull never reorders anything.** `moreUrgent` and `louder` rank by
Session state, and Attention is Sessions only. Promoting a row would put a repo
at the top of the rail for a reason the tree's ordering rule cannot express.

**The section's own highlight follows `m.active`** — "the PID of the Session the
working client's pane last showed" (`dashboard.go:266`) — not `m.cursor`. That
is the Session you are working with, and it is the only one that stays put,
because `↑↓` parks the tree cursor wherever you abandoned it once Pulls is
open. It can light **more than one row**. When the working pane holds a Session
whose branch has no Pull, nothing is highlighted, and that silence is correct.

### The glyph's colour is the one thing measurement could not settle

`◆` collides with nothing in the harness's inventory — `█ ● ⠿ ○ ❯ ⚠ ❄ ⇡ ⏵ ▣ ⚑ ▢`.
The mock draws it in **Ready's green**, which is the rail's existing "there is
something here for you" and is what the mark means. The objection is that
`CONTEXT.md` keeps **Attention** for Sessions and **Your move** for Pulls, and
a shared colour blurs two categories the glossary separates. Amber was the
alternative and was dropped for colliding with the caution line directly above
it on header rows.

**This is the one open question the implementation inherits.** It wants a look
in the live Dock before it is frozen.

## Where it stops

**Nothing outside the Dashboard carries a Pulls number.**

The attention strip carries what the harness can **push *and* clear**: a hook
fires when a Session blocks and the strip repaints in the same breath, and
focus landing on a pane clears Ready itself. That is what lets a line you never
asked for sit under your eye line all day — it can be trusted to go quiet the
moment you act.

**Your move** is none of that. It is a poll of someone else's system on a
thirty-minute clock, cleared by a review you do in a browser the harness cannot
see, so a count would stay lit for up to half an hour after it stopped being
true. `strip()`'s own comment names the cost: *"a status line that is always lit
is one you stop reading, which would cost the Blocked count the only thing it is
for."* Measured later the same day the row anatomy was drawn — 16 Pulls by then
against the 11 open at 08:12 — **10 were Your move**, every one a review owed — the strip would essentially never be blank again, and a blank
strip is what makes a lit one mean something.

**It would not have fitted either.** Measured through real tmux at the 40
columns the working client has in an 80-column Dock — the width
`TestANarrowStatusLineKeepsTheCountAndGivesUpTheSignature` pins:

```
 40 today  | █ 2 blocked · ● 3 ready
 40 +pulls |   blocked · ● 3 ready · ◆ 10 pulls
 40 widest |    locked · ● 99 ready · ◆ 99 pulls
159 +pulls | █ 2 blocked · ● 3 ready · ◆ 10 pulls · ganymede
```

tmux trims this segment from its **left** end. `signedWhenItFits` already
spends the `ganymede` signature to keep the count, on the stated grounds that
the count "is the whole reason the line is here". A third segment has nothing
left to spend, so it eats the Blocked count itself. `signatureColumns` would
have had to rise from 60 to about 73.

The naming says the same from the other end: the option behind the line is
`@ganymede-attention`, and Attention is Sessions only.

**The cost is named rather than hidden.** With Pulls closed on every start,
nothing tells you ten reviews are waiting until you press `p`. That is
accepted: a surface that comes to you uninvited has to be able to stop, and the
Dashboard is a surface you go to. `p` is the going.

**The Tile is out for its own separate reason** — one number there cannot say
which of several things it is about, and a thirty-minute fetch makes a poor
notification source.

**This section changes no code.** `strip.go`, `signatureColumns`,
`signedWhenItFits` and the 40-column test are untouched, and the 40-column
measurement is why.

## Not doing

- **Acting on a Pull** — approve, merge, comment. Pulls is a viewer. Opening
  the PR in a browser is the honest fallback the harness already leans on
  everywhere else.
- **Per-check CI detail** — which named check failed. That is the build-health
  board the original boundary ruled out, and it survives the reframe: a red
  check is your PR's problem, not a decision waiting on you.
- **Any Pulls number on the Tile, in OS notifications, or on the attention
  strip.** Above.
- **Non-GitHub forges.**
- **Telling you a worktree is cleanable once its Pull has merged.** The section
  half already happens — the Pull leaves the `is:open` set, its row goes, and
  the Session's mark goes with it. The harness noticing and saying so is the
  worktree-cleanup lifecycle `ARCHITECTURE.md` defers.
- **A verification pass on whether a Pull still exists.** Measured as costing a
  wasted click at worst; see `is:open` above.
- **Fixing the four foot labels that already hardcode `SELECTED`.** The lie
  predates this work.

## Testing

**Every decision above is testable in Go without the network**, because the
fetch is a subprocess whose output is JSON and the rendering is a pure
function of the fetched set. The one thing that is not is the tmux width
arithmetic, which is already covered by an existing test at 40 columns.

State model, in the shape of the existing state tables:

- Each AUTHORED state from its primary signal, and the chain's order where two
  signals co-occur — in particular `Rework` over `Conflicted`, and `Draft` over
  everything.
- `Landable` withheld when `baseRefName` is not the default branch, falling
  through to `Sent`.
- Each REQUESTED state, and that `reviewDecision: null` reaches `Yours`.
- A row with `mergeable: UNKNOWN` carries no state, is not Your move, and marks
  no Session row.
- The marks are orthogonal: an approved PR with a failing rollup is `Sent`
  carrying both `✓` and `✗`.
- Only FAILURE and ERROR draw a check mark; SUCCESS and PENDING draw none.
- The stack parent resolves by local join on `headRefName`, and falls back to
  nothing when the parent is not in the set.

Rendering, against the mock's own cases:

- No line exceeds 40 columns, over every case — the mock's `-check` already
  does this and reports 0.
- The identity column is the widest tail in the section, so one repo never
  renders two ways.
- The tail is kept, not the head: two repos sharing a 17-character prefix
  render distinguishably.
- 17 rows fit; the 18th scrolls, and the chrome line carries `▴`/`▾` counts.
- The heading sticks and carries its list's count.
- A stateless row draws `—`.
- The four bodies are distinguishable, and the network body keeps its rows.

Fetch and cadence:

- Exit 4, exit 1 with `HTTP 401`, and exit 1 without it reach three different
  bodies.
- An auth failure keeps polling at thirty minutes; a network failure retries at
  five.
- A cycle re-reads only the rows that came back `UNKNOWN`, and repaints once.
- `r` during a cycle is a no-op; `r` between cycles resets the window.
- Nothing is written to `state.json`.

Dashboard integration:

- `p` toggles; `esc` closes; `↑↓` drives Pulls while open and the tree's
  highlight is frozen, not moved.
- `c`, `w` and `t` take the foot while Pulls is open, and Pulls returns when
  they close.
- The tree keeps at least 21 rows at height 45 with Pulls open.
- `⏎` on a Pull with no matching Session sets the notice naming `o`.
- The Session mark fires on Your move only, never on a header row, and never
  reorders the tree.

Manual acceptance, in the live Dock:

- Open Pulls on a real set at 80 and at 200 columns; confirm the working client
  is untouched at both.
- Check `◆` against the live palette beside the section's own marks — the one
  open question.

## Docs

- **`CONTEXT.md`** — landed in `ac6ba0b`: all eight terms, and `panel` added to
  the Dashboard's `_Avoid_` line.
- **`ARCHITECTURE.md`** — landed in three commits and complete:
  - `5331de8` — the "PR/CI status display" boundary narrowed to "per-check CI
    detail and acting on a pull request" with the reasoning; "the harness's only
    outbound network call" became "one of the harness's two outbound network
    calls"; a new `## Pulls` section. **The JIRA line needed no edit** — "Ticket
    ID and link only — no JIRA API dependency, ever" is a statement about JIRA,
    and a GitHub call does not falsify it; it gained a clause noting that `gh`
    carries a login you already granted where JIRA would mean a token the
    harness has to keep. **`## What the harness writes` did not change either**,
    which is worth saying out loud: a new poller adding a sidecar section is
    the obvious expectation, and remembering nothing ruled it out.
  - `3419115` — `## Pulls` gained *Reaching it* and *Moving in it*; the
    SELECTED-box bullet under `## Dashboard internals` says Pulls takes its
    place while open.
  - `778ccb0` — `## Pulls`'s *Where it stops* now rules out the attention strip
    with its reason; `## Claude Code updates`'s *Where it stops* corrected,
    having claimed the strip carries the Blocked count alone when it has
    carried both Attention tiers since it was written.
- **`README.md`** — **still owed, and this is work the implementation does.**
  The Prerequisites table gains a `gh` row. It was deliberately not written
  with the rest: it is user-facing documentation of a built product, and today
  a reader would install `gh` for nothing.

**Branch order.** Three branches carry the doc work and want merging in this
order: `docs/name-the-pulls-section` (`ac6ba0b`) →
`docs/pulls-fetch-and-cadence` (`5331de8`, `3419115`) →
`docs/pulls-and-the-attention-strip` (`778ccb0`).

`proto/pulls-row-anatomy` carries `cmd/pullsmock` and is **throwaway** — it is
the mock this design's row anatomy was measured on, not code to merge.

## Where the decisions live

Each heading above restates one decision in full. The ticket behind it holds
the measurements that did not fit here, and the alternatives that were tried
and dropped.

| This document | Ticket |
|---|---|
| Where it lives, the space contract | [#67](https://github.com/BrechtBonte/ganymede/issues/67) |
| What it shows, the query | [#68](https://github.com/BrechtBonte/ganymede/issues/68) |
| GitHub's fields and what they cost | [#69](https://github.com/BrechtBonte/ganymede/issues/69) · `docs/research/github-pr-fields.md` |
| The state model | [#70](https://github.com/BrechtBonte/ganymede/issues/70) |
| Vocabulary, the foot's label | [#71](https://github.com/BrechtBonte/ganymede/issues/71) |
| Matching a Session to its Pull | [#72](https://github.com/BrechtBonte/ganymede/issues/72) |
| Fetching, the cadence, going stale | [#73](https://github.com/BrechtBonte/ganymede/issues/73) |
| Keys, the legend | [#74](https://github.com/BrechtBonte/ganymede/issues/74) |
| The row | [#75](https://github.com/BrechtBonte/ganymede/issues/75) |
| Where it stops | [#79](https://github.com/BrechtBonte/ganymede/issues/79) |
