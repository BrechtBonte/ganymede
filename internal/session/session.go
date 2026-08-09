// Package session is what the harness knows about one live Claude Code
// process, and the states it can be in.
//
// The vocabulary is CONTEXT.md's, which is normative. Gone is not here: it is
// not a state a Session can be found in but the absence of one, and it shows
// as a row that has disappeared.
//
// Nothing in here reads a file or asks tmux anything. It is the currency the
// registry, the hooks, the reconciler and the Dashboard all count in, which is
// why it sits below every one of them.
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

// StateOf reads Claude Code's own word for what a Session is doing, and says
// whether it was a word this harness knows. It is one vocabulary written in
// two places — the status in a registry file, and the status in `claude agents
// --json` — so it is read here, once, rather than left to drift between the
// two things that read it.
//
// A status this harness does not know is Idle: it is the state that claims the
// least about a Session it cannot read, and it keeps the row on the Dashboard
// rather than pretending the Session is Gone. That is the right answer for a
// reader with nothing else to go on, and the wrong one for a reader whose word
// is about to be preferred over somebody else's — so it says which of the two
// it just handed you.
func StateOf(status string) (State, bool) {
	switch status {
	case "busy":
		return Working, true
	case "waiting":
		return Blocked, true
	case "shell":
		return Shell, true
	case "idle":
		return Idle, true
	}
	return Idle, false
}

// Glyph is how a State reads at a glance: one column wide, from the validated
// sidepanel mock. It is here rather than in whichever surface draws it because
// the harness shows the same states in more than one place — the rail, the
// attention strip in the status line — and two surfaces disagreeing about what
// Blocked looks like would be two vocabularies, not one.
//
// A state with no mark is one this harness does not draw.
func (s State) Glyph() string {
	switch s {
	case Blocked:
		return "█"
	case Ready:
		return "●"
	case Working:
		return "⠿"
	case Idle:
		return "○"
	case Shell:
		return "❯"
	}
	return ""
}

// Colour is the State's colour, as a hex triplet every surface can say in its
// own way. Attention is what has to carry across a room — a Session that has
// stopped is red, an unread turn green — while the states asking nothing of
// you have no colour of their own and are drawn in whatever quiet the surface
// keeps for them.
func (s State) Colour() string {
	switch s {
	case Blocked:
		return "#f85149"
	case Ready:
		return "#3fb950"
	case Working:
		return "#58a6ff"
	case Shell:
		return "#d2a8ff"
	}
	return ""
}

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

// Attention is how much of a working set is waiting on you, counted by tier.
// It is what the ambient surfaces show — the ones with room for a number and
// not for a row.
type Attention struct {
	// Blocked is how many Sessions cannot continue without your decision.
	Blocked int
	// Ready is how many turns have finished that you have not read.
	Ready int
}

// Any reports whether anything at all is waiting on you. A working set that
// asks nothing of you is drawn as nothing rather than as a pair of zeroes:
// a strip that is always lit is one you stop reading.
func (a Attention) Any() bool { return a.Blocked > 0 || a.Ready > 0 }

// AttentionIn counts what is waiting on you across a working set.
func AttentionIn(sessions []Session) Attention {
	var counted Attention
	for _, s := range sessions {
		switch s.State {
		case Blocked:
			counted.Blocked++
		case Ready:
			counted.Ready++
		}
	}
	return counted
}
