// Package registry reads Claude Code's per-session registry: the directory of
// JSON files, one per running Session, that is the authoritative account of
// which Sessions exist and what each of them is doing.
//
// The registry is undocumented. This package was built against Claude Code
// 2.1.220 and is written to survive the shape moving underneath it: a file it
// cannot read costs only its own Session, and a status it does not recognise
// reads as Idle rather than dropping the row.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// State is a Session's state as far as the registry can tell. Ready is not one
// of them: the registry cannot know whether you have seen a finished turn, so
// deciding Ready from Idle is the harness's own job.
type State string

const (
	Working State = "Working"
	Blocked State = "Blocked"
	Idle    State = "Idle"
	Shell   State = "Shell"
)

// Session is one live Claude Code process, as the registry describes it.
type Session struct {
	// PID is the Claude Code process. It is what the harness follows to find
	// the tmux pane the Session is running in.
	PID int
	// ID is Claude Code's own session id.
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
	// Since is when the Session entered its current state, which is what a
	// wait age counts from.
	Since time.Time
}

// Registry is Claude Code's per-session registry directory.
type Registry struct {
	// Dir holds one JSON file per Session.
	Dir string
	// Alive reports whether a process is still running. Nil means ask the
	// operating system; tests set it to decide who is alive.
	Alive func(pid int) bool
	// Poll is how often Watch re-reads the registry even when nothing in the
	// directory has changed. Zero means the default.
	Poll time.Duration
}

// Default is the registry where Claude Code keeps it.
func Default() (Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Registry{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Registry{Dir: filepath.Join(home, ".claude", "sessions")}, nil
}

// Read returns the live Sessions the registry describes, in a stable order.
// Sessions whose process has died are Gone and are left out, as is a registry
// directory that does not exist: an absent registry is an empty working set,
// not a failure.
func (r Registry) Read() ([]Session, error) {
	entries, err := os.ReadDir(r.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the session registry at %s: %w", r.Dir, err)
	}

	alive := r.Alive
	if alive == nil {
		alive = running
	}

	// os.ReadDir sorts by filename, so the order out of here is stable — which
	// is what lets a watcher tell a real change from a re-read.
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		session, ok := readSession(filepath.Join(r.Dir, entry.Name()))
		if !ok || !alive(session.PID) {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// record is the part of a registry file the harness reads.
type record struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	// WaitingFor is only ever read as text, but it is kept raw so that a
	// richer shape in some later Claude Code costs the harness a reason
	// rather than the whole record.
	WaitingFor json.RawMessage `json:"waitingFor"`
}

// readSession reads one registry file, reporting whether it held a Session.
// Claude Code writes these files while the harness is reading them, so a file
// that will not parse is a normal event and not worth an error: the next read
// picks it up whole.
func readSession(path string) (Session, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Session{}, false
	}
	var r record
	if err := json.Unmarshal(body, &r); err != nil || r.PID == 0 {
		return Session{}, false
	}
	return Session{
		PID:    r.PID,
		ID:     r.SessionID,
		Dir:    r.CWD,
		Name:   r.Name,
		State:  stateOf(r.Status),
		Reason: reasonOf(r.WaitingFor),
		Since:  time.UnixMilli(r.StatusUpdatedAt),
	}, true
}

// stateOf reads the registry's status. An unknown status is Idle: it is the
// state that claims the least about a Session this harness cannot read, and it
// keeps the row on the Dashboard rather than pretending the Session is Gone.
func stateOf(status string) State {
	switch status {
	case "busy":
		return Working
	case "waiting":
		return Blocked
	case "shell":
		return Shell
	default:
		return Idle
	}
}

// reasonOf reads waitingFor, which has only ever been observed as a string.
func reasonOf(raw json.RawMessage) string {
	var reason string
	if err := json.Unmarshal(raw, &reason); err != nil {
		return ""
	}
	return reason
}

// running asks the kernel whether a process is still there. Signal 0 is
// delivered to nobody; it only reports whether the process exists, and a
// process belonging to another user exists just as much as one of ours.
func running(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
