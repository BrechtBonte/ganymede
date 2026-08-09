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

	"github.com/BrechtBonte/ganymede/internal/session"
)

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
//
// No Session comes back Ready. The registry cannot know whether you have seen
// a finished turn; that is the state model's to decide, over these.
func (r Registry) Read() ([]session.Session, error) {
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
	sessions := make([]session.Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		found, ok := readSession(filepath.Join(r.Dir, entry.Name()))
		if !ok || !alive(found.PID) {
			continue
		}
		sessions = append(sessions, found)
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
func readSession(path string) (session.Session, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return session.Session{}, false
	}
	var r record
	if err := json.Unmarshal(body, &r); err != nil || r.PID == 0 {
		return session.Session{}, false
	}
	return session.Session{
		PID:    r.PID,
		ID:     r.SessionID,
		Dir:    r.CWD,
		Name:   r.Name,
		State:  stateOf(r.Status),
		Reason: reasonOf(r.WaitingFor),
		Since:  since(r.StatusUpdatedAt),
	}, true
}

// since reads when the registry last moved a Session. A record that does not
// say gets no time at all rather than the start of the Unix epoch: a Session
// the registry cannot timestamp must not read as one that has been waiting on
// you since 1970, to the ordering or to anything weighing it against a clock.
func since(millis int64) time.Time {
	if millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// stateOf reads the registry's status. An unknown status is Idle: it is the
// state that claims the least about a Session this harness cannot read, and it
// keeps the row on the Dashboard rather than pretending the Session is Gone.
func stateOf(status string) session.State {
	switch status {
	case "busy":
		return session.Working
	case "waiting":
		return session.Blocked
	case "shell":
		return session.Shell
	default:
		return session.Idle
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
