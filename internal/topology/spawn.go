package topology

import (
	"fmt"
	"strings"
	"time"
)

// WorktreeCommand is what a spawned Worktree session runs: claude --worktree
// adopted as-is, named after name and started in auto permission mode —
// spawned Worktree sessions always start there, since the worktree's
// isolation is what justifies it and whatever auto still gates simply
// surfaces as Blocked (§6, a standing decision). Prompt, when not empty, is
// appended so the session starts Working at once rather than waiting at its
// prompt.
func WorktreeCommand(name, prompt string) []string {
	command := []string{"claude", "--worktree", name, "-n", name, "--permission-mode", "auto"}
	if prompt != "" {
		command = append(command, prompt)
	}
	return command
}

// Spawn starts a background Worktree session for the repo at dir: a new tmux
// window in that repo's Session, named after name, running the Worktree
// command. It never switches any attached client's window — a background
// session is not one that steals your eye the moment it starts.
//
// The repo's own Session is brought up first if nothing is running there
// yet: spawning into a repo from the picker has to work whether or not the
// repo has ever been opened.
//
// It returns the window it opened, which is the handle SpawnDied is asked
// about afterwards. The window's own name is no use for that: claude renames
// its window as it runs, so the name Spawn asked for is gone within seconds.
func (h Harness) Spawn(dir, name, prompt string) (string, error) {
	session, err := h.broughtUp(dir)
	if err != nil {
		return "", err
	}

	build := h.Worktree
	if build == nil {
		build = WorktreeCommand
	}
	// The window is opened empty, told to hold its pane open, and only then
	// given the command to run. Opening it on the command directly loses the
	// race in exactly the case worth reporting: a session that dies on startup
	// is gone before a second tmux call can land, and tmux erases the window —
	// with the reason in it — the moment the command exits. Setting the option
	// first is what leaves a corpse to read.
	window, err := h.sessions().output("new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", "="+session+":", "-n", name, "-c", dir)
	if err != nil {
		return "", fmt.Errorf("spawn worktree session %s: %w", name, err)
	}
	window = strings.TrimSpace(window)

	// Best effort: a window tmux would not hold open has still been spawned, and
	// a session dying in it simply reads as the window SpawnDied finds gone.
	_ = h.sessions().run("set-option", "-w", "-t", window, "remain-on-exit", "on")

	respawn := append([]string{"respawn-pane", "-k", "-t", window, "-c", dir}, build(name, prompt)...)
	if err := h.sessions().run(respawn...); err != nil {
		// The window is sitting on the bare shell it was opened with, which would
		// read as a Worktree session that started and be counted as one.
		_ = h.sessions().run("kill-window", "-t", window)
		return "", fmt.Errorf("spawn worktree session %s: %w", name, err)
	}
	return window, nil
}

// SpawnDied reports whether the Worktree session in window has died, and the
// last thing it said before it did.
//
// This is the question a spawn cannot answer at the time it spawns. tmux
// accepting a new window says only that the command was started, and the
// failures worth reporting happen well after that: a WorktreeCreate hook that
// fails takes some ten seconds to bring claude down with it. Asked and asked
// again, this is what tells a spawn that worked from one that only started.
//
// A window that is no longer there counts as died with nothing to read: its
// command outlived neither the spawn nor the option meant to hold its pane
// open. tmux answers for a window it cannot find with an empty line and no
// complaint at all, so silence is that answer rather than an error to pass on.
// Once a death has been read the window goes — the reason is in hand by then,
// and a rail's worth of dead windows is its own kind of mess.
func (h Harness) SpawnDied(window string) (string, bool) {
	dead, err := h.sessions().output("display-message", "-p", "-t", window, "#{pane_dead}")
	if err != nil || strings.TrimSpace(dead) == "" {
		return "", true
	}
	if strings.TrimSpace(dead) != "1" {
		return "", false
	}

	// The whole history rather than the visible pane: claude tears its own
	// screen down on the way out, and what is left on show is as often as not
	// the tail of a single wrapped line.
	capture, err := h.sessions().output("capture-pane", "-p", "-S", "-", "-t", window)
	if err != nil {
		return "", true
	}
	_ = h.sessions().run("kill-window", "-t", window)
	return lastWords(capture), true
}

// How long a spawned session is watched for a death on startup, and how often
// it is looked at.
//
// The half minute is measured rather than guessed: claude takes some ten
// seconds to exit when a WorktreeCreate hook fails, which is the failure that
// sent anyone looking at this code in the first place. A watch that gave up
// before then would report every one of those spawns as a success.
const (
	spawnWatchFor   = 30 * time.Second
	spawnWatchEvery = 2 * time.Second
)

// SpawnWatch waits out the startup a spawn cannot vouch for, and reports the
// session dying in it along with the last thing it said.
//
// It blocks for as long as it takes to be sure — up to WatchFor — which is why
// the Dashboard asks it away from the goroutine that draws. A session still
// running by then is one that started, and the scaffolding holding its pane
// open comes off behind it.
func (h Harness) SpawnWatch(window string) (string, bool) {
	watchFor, every := h.WatchFor, h.WatchEvery
	if watchFor == 0 {
		watchFor = spawnWatchFor
	}
	if every == 0 {
		every = spawnWatchEvery
	}

	for waited := time.Duration(0); ; waited += every {
		if output, died := h.SpawnDied(window); died {
			return output, true
		}
		if waited+every >= watchFor {
			break
		}
		time.Sleep(every)
	}

	// Best effort, like the option going on: a spawn that started is not worth
	// a word to you either way, and the worst a failure here costs is a window
	// that sits dead instead of closing when you are finished with it.
	_ = h.SpawnSettled(window)
	return "", false
}

// SpawnSettled drops what Spawn put on the window once the session has proven
// it started. A session ending an hour later is a day's work finishing, not a
// spawn that failed, and its window should close behind it the way every other
// one does rather than sit there dead.
func (h Harness) SpawnSettled(window string) error {
	return h.sessions().run("set-option", "-w", "-t", window, "remain-on-exit", "off")
}

// lastWords is what a dead pane's capture is worth on a forty-column rail: the
// lines that carry something, without the marker tmux leaves in a pane it is
// holding open, and only the first few — a startup error says what went wrong
// at the top, and everything under it is the same news at greater length.
func lastWords(capture string) string {
	var said []string
	for _, line := range strings.Split(capture, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Pane is dead") {
			continue
		}
		said = append(said, line)
		if len(said) == 3 {
			break
		}
	}
	return strings.Join(said, " ")
}
