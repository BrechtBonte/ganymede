# Ganymede Harness

The terminal harness replacing Warp for multi-repo Claude Code work: a tmux-based environment whose dashboard shows every repo's agent sessions, their attention states, and main-root availability. This glossary is the ubiquitous language of that dashboard.

## Language

### Structure

**Dashboard**:
The always-visible sidepanel TUI listing the working set of repos and their sessions, from which all harness actions are taken.
_Avoid_: overview, home screen, monitor, rail

**Dock**:
The tmux frame filling the emulator window, holding the dashboard on the left and the working client on the right. It has no prefix key of its own, so every keystroke belongs to the client inside the pane.
_Avoid_: frame, container, wrapper, shell, layout

**Working set**:
The repos the dashboard shows: those with a live session, a Claimed root, or recent harness activity. Everything else lives behind the picker.
_Avoid_: active repos, favourites, registry

**Session**:
One live Claude Code process shown as a row on the dashboard, tied to a working directory (a main root or a worktree).
_Avoid_: agent, instance, tab

**Main root**:
A repository's primary checkout — the directory PR reviews happen in.
_Avoid_: main checkout, repo root, primary clone

**Worktree session**:
A session running in a git worktree spawned for background work, leaving the main root untouched.
_Avoid_: background session, side session

**Popup shell**:
The toggleable overlay shell belonging to the session in focus, opening in that session's current directory. Hidden, not killed, on close; never an occupant of a main root.
_Avoid_: scratch terminal, quick terminal, drawer

**Tile**:
Ganymede's own macOS Dock tile and the menu-bar item beside it, carrying the number of Blocked sessions — the harness's presence outside the emulator window. Clicking the menu-bar item opens a dropdown with the full Blocked/Ready/Working breakdown, each in its tier's colour, and an Open Ganymede action. Lives exactly as long as the dashboard.
_Avoid_: dock icon, dock badge, app badge — the Dock is the tmux frame, not macOS's

**Update notice**:
The single line under the Dashboard's header saying the Claude Code installed on this machine is behind the build its auto-update channel is publishing. Carries both versions, and is absent altogether whenever the install is current.
_Avoid_: update banner, upgrade prompt, version warning, new version available

### Session states

**Working**:
The session's turn (or its subagents) is running; nothing is asked of you.
_Avoid_: busy, running

**Blocked**:
The session cannot continue without your decision — a permission prompt, question, or dialog. Always displayed with its reason.
_Avoid_: waiting, stuck, pending

**Ready**:
The turn finished and you have not seen the output yet — an unread badge, not a plain idle. Seeing the session or prompting it clears Ready to Idle.
_Avoid_: done, finished, idle-with-output

**Idle**:
At the prompt, seen, nothing pending.
_Avoid_: inactive, sleeping

**Shell**:
Occupied by you — you are in the session's shell mode running commands.
_Avoid_: manual mode

**Gone**:
The session's process has ended; the row disappears.
_Avoid_: dead, closed, ended

**Attention**:
The union of Blocked and Ready — everything waiting on you. Blocked outranks Ready; within a tier, longest-waiting first.
_Avoid_: alerts, notifications, needs-input

### Pane view

**Frozen**:
The Session's pane is showing a held tmux view rather than the live Session — your keys reach the mode, not Claude. Orthogonal to every Session state: a Frozen pane can sit over a Working one, which is exactly when it reads as a hang.
_Avoid_: stuck, hung, copy-mode, scrolled

### Main-root states

**Free**:
No live session in the main root and no claim on it — safe to check out a PR.
_Avoid_: available, open

**In use by agent**:
Any live session has the main root as its working directory — even an Idle one, since an idle agent still holds context bound to that checkout.
_Avoid_: busy, occupied, locked

**Claimed**:
Explicitly reserved by you (typically for a PR review), optionally with a note. Warns agent spawns away until released.
_Avoid_: reserved, checked out

**Takeover**:
Claiming a main root whose only occupant is an Idle session by ending that session in the same action. Refused when the occupant is Working or Blocked.
_Avoid_: force claim, steal
