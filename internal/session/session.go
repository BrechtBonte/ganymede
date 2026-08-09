// Package session is what the harness knows about one live Claude Code
// process, and the states it can be in.
//
// The vocabulary is CONTEXT.md's, which is normative. Gone is not here: it is
// not a state a Session can be found in but the absence of one, and it shows
// as a row that has disappeared.
//
// Nothing in here reads a file or asks tmux anything. It is the currency the
// registry, the hooks and the Dashboard all count in, which is why it sits
// below every one of them.
package session

import "time"

// State is what a Session is doing.
type State string

const (
	// Working: the Session's turn (or its subagents) is running; nothing is
	// asked of you.
	Working State = "Working"
	// Blocked: the Session cannot continue without your decision. Always
	// displayed with its Reason.
	Blocked State = "Blocked"
	// Ready: the turn finished and you have not seen the output yet — an
	// unread badge, not a plain Idle. Only the harness can award it: the
	// registry has no idea what you have looked at.
	Ready State = "Ready"
	// Idle: at the prompt, seen, nothing pending.
	Idle State = "Idle"
	// Shell: occupied by you, in the Session's shell mode.
	Shell State = "Shell"
)

// Session is one live Claude Code process, shown as a row on the Dashboard.
type Session struct {
	// PID is the Claude Code process. It is what the harness follows to find
	// the tmux pane the Session is running in.
	PID int
	// ID is Claude Code's own session id, which is how the hooks name the
	// Session they are reporting about.
	ID string
	// Dir is the Session's working directory — a Main root or a worktree.
	// It is ground truth for which repo the Session belongs to, whether or
	// not that repo lies under a scan root.
	Dir string
	// Name is the Session's name, which for a Worktree session carries the
	// ticket.
	Name string
	// State is what the Session is doing.
	State State
	// Reason is what a Blocked Session is waiting for; empty otherwise.
	Reason string
	// Snippet is the last thing a Ready Session said, which is what makes the
	// unread badge worth reading; empty otherwise.
	Snippet string
	// Since is when the Session entered its current state, which is what a
	// wait age counts from.
	Since time.Time
}

// Attention is the union of Blocked and Ready: everything waiting on you.
func (s Session) Attention() bool {
	return s.State == Blocked || s.State == Ready
}
