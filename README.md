# Ganymede

> **Status:** actively evolving — claim/takeover, worktree spawn, inline actions, and notifications all work day-to-day; rough edges are still being sanded down.

## Table of contents

- [What it is](#what-it-is)
- [How to use it](#how-to-use-it)
  - [See what needs you](#see-what-needs-you)
  - [Spawn a worktree session for a ticket](#spawn-a-worktree-session-for-a-ticket)
  - [Respond to a permission prompt inline](#respond-to-a-permission-prompt-inline)
  - [Claim a root for PR review](#claim-a-root-for-pr-review)
  - [Get notified when you're away](#get-notified-when-youre-away)
  - [Keys](#keys)
- [How to install](#how-to-install)
  - [Prerequisites](#prerequisites)
  - [Install](#install)
  - [Run](#run)
- [Learn more](#learn-more)

## What it is

![Ganymede dashboard](docs/assets/dashboard.png)
<!-- TODO: replace with a real capture. Best shot: a Ghostty window with the
     sidepanel showing 2+ repos, at least one session Blocked and one Ready,
     and the working client focused on a session on the right.
     `./bin/ganymede up`, get a couple of sessions into those states, then
     `screencapture -i docs/assets/dashboard.png`. -->

Ganymede is a terminal harness for day-to-day multi-repo Claude Code work on macOS: an always-visible dashboard that shows which repos have agent sessions open, which of those need you, and which main checkouts are free — so you can jump straight to whatever needs attention instead of hunting across terminal windows.

## How to use it

Once it's running (see [How to install](#how-to-install)), Ganymede docks a sidepanel to the left of your terminal. Everything below happens from there.

### See what needs you

The sidepanel lists your repos, each with its sessions nested underneath. Sessions needing you sort to the top — **Blocked** above **Ready**, longest-waiting first — so the row at the top of the list is always the most urgent thing waiting on you. Use `↑`/`↓` to move the selection; the box at the foot of the panel shows full detail for whatever's highlighted — the blocked reason, the last message, the ticket, the working directory.

Press `⏎` on a session row to jump the working client straight to it, clearing that session's Ready badge as you go. Press `⏎` on a repo's own header row to go to that repo instead, starting a session at its main root if nothing is running there yet.

Don't see the repo you want? Press `g` to open a fuzzy picker over every repo Ganymede can find under `~/Projects`. Typing narrows it — `gnm` reaches `ganymede` — and `⏎` takes you there and adds it to the sidepanel.

### Spawn a worktree session for a ticket

Press `w` on a repo to open a background session in its own git worktree, leaving the main root untouched. The dialog takes two optional fields: a JIRA ticket ID (the worktree gets named after it, e.g. `FIRE-2841-paging`) and a first prompt — fill that in and the session starts working immediately, fire-and-forget. Worktree sessions always start in Claude Code's auto permission mode, since the isolation of a worktree justifies it.

### Respond to a permission prompt inline

When a session goes **Blocked** on a permission prompt, you don't have to jump into its pane for a plain yes/no: press `y` to approve or `n` (or `Esc`) to deny, right from the sidepanel. Anything richer than a straight approve/deny still needs `⏎` to jump in — the sidepanel only ever scripts what it can verify first.

Press `p` on an Idle, Ready, or Working session to send it a prompt from the detail box without switching panes. On a Working session, `Enter` queues the prompt for after the current turn; `Ctrl+Enter` interrupts it and sends immediately. Press `x` to interrupt a Working session outright, and `q` to end an Idle or Ready one (with a confirmation first).

### Claim a root for PR review

Checking out a PR means using a repo's main root — but if an agent session already has it checked out, even an idle one, it's still holding context tied to that checkout. Press `c` on a repo's header row to see what that does for its current state:

- **Free** root: opens a Claim dialog (with an optional note, e.g. "reviewing FIRE-2841") — this reserves the root and warns worktree spawns away from it.
- **Claimed** root (by you): releases it immediately, no confirmation.
- **In-use** root, with its only occupant Idle: opens a **Takeover** confirmation — accepting it ends that session and claims the root behind it in one action. Refused if the occupant is Working or Blocked.

### Get notified when you're away

Whenever Ghostty isn't the frontmost app, Ganymede's notifier is the one place alerts come from. A **Blocked** session pings you immediately and stays sticky until you resolve it. A **Ready** session — done, but you haven't looked yet — stays a silent dashboard badge at first, and only escalates to a notification if it's still unseen about a minute later. Clicking a notification focuses Ghostty and jumps you straight to that session.

### Keys

| Key | On | Action |
|---|---|---|
| `⏎` | session row | Jump — switch the working client to the session (clears Ready) |
| `⏎` | repo row | Go to the repo — switch the working client to its session, started at the main root if nothing is running there |
| `y` / `n` | Blocked | Approve (`y`) / deny (`n` or `Esc`). Richer choices always jump in |
| `p` | Idle, Ready, Working | Prompt from the detail box. On Working, `Enter` queues; `Ctrl+Enter` interrupts-then-sends |
| `x` | Working | Interrupt |
| `q` | Idle, Ready | End session (confirm first). Refused on Working/Blocked |
| `w` | repo | Spawn a worktree session |
| `c` | repo header | Claim (Free) / release (Claimed) / Takeover (In-use, sole Idle occupant) |
| `g` | anywhere | Fuzzy repo picker over the full inventory |
| `t` | session row | Set or correct the JIRA ticket |
| `o` | session row | Open the ticket link in the browser |
| `` Ctrl+` `` | global, no prefix | Toggle the popup shell — a scratch shell in the current session's directory. Closing hides it, never kills it |
| `↑` / `↓` | anywhere | Move the selection |
| `Alt+g` | anywhere | Move focus between the sidepanel and the working client |
| `Shift+⏎` | typing to Claude | Newline instead of sending — it sends what `Alt+⏎` sends |

Digit keys are never scripted, because permission-dialog rows are dynamic.

The bottom row of the dock is this table, so the whole vocabulary is on screen while you're learning it. It lists every key, including the ones that do nothing on the row you happen to be standing on — the SELECTED box is the one that only ever offers what will actually fire.

## How to install

### Prerequisites

| Requirement | Why | Get it |
|---|---|---|
| macOS | Ganymede is built around Ghostty and macOS-only notification APIs — there's no Linux support | — |
| [Claude Code](https://code.claude.com) | Ganymede is a harness *for* Claude Code sessions | https://code.claude.com |
| [Ghostty](https://ghostty.org) | The terminal emulator Ganymede docks its dashboard into | https://ghostty.org |
| tmux (3.3+) | The multiplexer Ganymede's dashboard and sessions run on | `brew install tmux` |
| Go toolchain | To build the `ganymede` binary | https://go.dev/dl/ |
| [terminal-notifier](https://github.com/julienXX/terminal-notifier) | Ganymede's own OS notification channel — without it, Blocked/Ready alerts don't fire beyond the dashboard | `brew install terminal-notifier` |

### Install

Clone this repo, then build the binary:

```sh
go build -o bin/ganymede ./cmd/ganymede
# or: make build
```

### Run

| Command | Does |
|---|---|
| `ganymede up [directory]` | Open the harness for the repo at *directory* (default: the current one) — installs config and hooks, brings up sessions, opens the Ghostty window |
| `ganymede dashboard` | Run the dashboard standalone in the current terminal |
| `ganymede install` | Install the tmux/Ghostty config and the Claude Code hooks only, without opening a window |

`ganymede up` is what you want day-to-day:

```sh
./bin/ganymede up
# or: make up
```

It's safe to re-run — it reuses whatever's already up and installs over itself rather than beside itself. Inside the window, `Alt+g` moves between the sidepanel and the working client; the dock itself runs with no prefix key, so `C-b` and everything else belongs to the session you're working in.

Rebuilding the binary is not enough to pick up a change: `up` only ever reuses an already-running Dashboard rather than restarting it. `make refresh` (or `scripts/refresh.sh`) rebuilds and restarts the Dashboard's own tmux pane in place, leaving the dock, the working client and every repo Session untouched.

`make launcher` installs a `Ganymede.app` into `~/Applications`, so you can bring the harness up from Spotlight instead of a terminal. Run it once; re-run it if this checkout ever moves, since the app bakes in its absolute path.

## Learn more

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — internals: layer choices, data flow, the full state model, the guarded action protocol, and the design decisions behind them
- [CONTEXT.md](CONTEXT.md) — the normative vocabulary this dashboard uses
