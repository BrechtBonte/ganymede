package topology

import "fmt"

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
func (h Harness) Spawn(dir, name, prompt string) error {
	session, err := h.broughtUp(dir)
	if err != nil {
		return err
	}

	build := h.Worktree
	if build == nil {
		build = WorktreeCommand
	}
	args := append([]string{"new-window", "-d", "-t", "=" + session + ":", "-n", name, "-c", dir}, build(name, prompt)...)
	if err := h.sessions().run(args...); err != nil {
		return fmt.Errorf("spawn worktree session %s: %w", name, err)
	}
	return nil
}
