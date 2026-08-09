// Package reconciler cross-checks the harness's account of the working set
// against `claude agents --json`.
//
// The registry the Dashboard runs on is undocumented: it was read off a
// running Claude Code rather than out of a specification, and nothing promises
// it will mean the same thing after the next upgrade. `claude agents --json`
// is the interface Claude Code does document, so it is the one worth falling
// back to — but asking costs a process, so it is asked slowly and the registry
// keeps the Dashboard live between times.
//
// That buys two things. A Session the registry watch never saw — a file whose
// shape it no longer knows, a directory it is not reading — appears on the
// next cross-check. And where the two disagree about a Session, there is a
// documented account to prefer. Which of them wins is the state model's to
// settle; this package only goes and asks.
package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// defaultEvery is how long between cross-checks. This is insurance against a
// registry shape that has moved underneath the harness, not a way of watching
// Sessions — that is the registry watch's job, and it is sub-second — so it
// runs slowly enough that the Claude Code process it costs does not matter.
const defaultEvery = 30 * time.Second

// defaultTimeout is how long one cross-check may take. Cross-checks are made
// one after another, so a Claude Code that never answers would be the last one
// ever asked — and the picture it left behind would go on being laid over the
// registry for the rest of the day. Generous next to the fifth of a second
// this takes in practice, and short next to how often it is asked.
const defaultTimeout = 10 * time.Second

// Reconciler asks Claude Code which Sessions it is running.
type Reconciler struct {
	// Claude is the Claude Code binary to ask. Empty means the claude on PATH.
	Claude string
	// Every is how long between cross-checks. Zero means the default.
	Every time.Duration
	// Timeout is how long one cross-check may take before it is given up on.
	// Zero means the default.
	Timeout time.Duration
}

// Reconciled is what one cross-check found.
type Reconciled struct {
	// At is when the cross-check was asked for — not when it answered. It is
	// what the state model weighs a registry record against, and the picture
	// cannot show anything that happened after it was taken. Taking the
	// earlier of the two moments means a registry record written while Claude
	// Code was still answering counts as the newer word, which is the safe way
	// round: the cross-check corrects a registry that has drifted, never one
	// that has simply moved on.
	At time.Time
	// Sessions is the working set Claude Code reported.
	//
	// No Session comes back with a Reason or a Since. The cross-check says
	// what a Session is doing, not why it is waiting or since when — those
	// belong to the registry and the hooks, and are laid back over the top.
	//
	// A Session whose status this harness could not read comes back with no
	// State at all, which is the cross-check saying it has no opinion on that
	// row rather than an opinion it made up.
	Sessions []session.Session
}

// record is the part of one `claude agents --json` record the harness reads.
//
// startedAt is deliberately not among them. It says when a Session started,
// which is not when it entered the state it is in, and reading one as the
// other would put an hours-long wait age on a Session that has been Blocked
// for a moment.
type record struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

// Read asks Claude Code once.
func (r Reconciler) Read(ctx context.Context) (Reconciled, error) {
	claude := r.Claude
	if claude == "" {
		claude = "claude"
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, giveUp := context.WithTimeout(ctx, timeout)
	defer giveUp()

	asked := time.Now()
	// Output keeps whatever Claude Code has to say for itself out of the
	// terminal, which the Dashboard is drawing in.
	answer, err := exec.CommandContext(ctx, claude, "agents", "--json").Output()
	if err != nil {
		return Reconciled{}, fmt.Errorf("ask %s for its agents: %w", claude, err)
	}

	var reported []record
	if err := json.Unmarshal(answer, &reported); err != nil {
		return Reconciled{}, fmt.Errorf("read the Sessions %s reported: %w", claude, err)
	}

	sessions := make([]session.Session, 0, len(reported))
	for _, said := range reported {
		// A record the harness cannot even find a process in is a shape it has
		// lost hold of, and it costs its own Session and no more. The pid is
		// also what a Session is matched on, so a record without one could not
		// be reconciled against anything anyway.
		if said.PID <= 0 {
			continue
		}
		state, known := session.StateOf(said.Status)
		if !known {
			// Idle is what a reader with nothing else to go on falls back to,
			// and it must not be reported as an answer from here: this account
			// is the one preferred where the two disagree, so a default dressed
			// up as a reading would blank a Dashboard full of good rows the day
			// Claude Code renames a status. No state at all is the honest
			// report, and the state model knows what to do with one.
			state = ""
		}
		sessions = append(sessions, session.Session{
			PID:   said.PID,
			ID:    said.SessionID,
			Dir:   said.CWD,
			Name:  said.Name,
			State: state,
		})
	}
	return Reconciled{At: asked, Sessions: sessions}, nil
}

// Watch cross-checks once now and again every Every, until ctx ends. The
// channel is closed when the watch stops.
//
// The first cross-check does not wait for the timer, so that a Session the
// registry watch cannot see at all is not missing from the Dashboard for a
// whole slow tick after it comes up.
//
// Nothing here is worth an error. A cross-check that cannot be made reports
// nothing rather than an empty working set — insurance that paid out by taking
// every row off the Dashboard would be worse than none — and a machine whose
// claude will not run at all still has a registry to watch, so the watch
// starts either way and keeps trying.
func (r Reconciler) Watch(ctx context.Context) <-chan Reconciled {
	checks := make(chan Reconciled)
	go r.watch(ctx, checks)
	return checks
}

func (r Reconciler) watch(ctx context.Context, checks chan<- Reconciled) {
	defer close(checks)

	every := r.Every
	if every <= 0 {
		every = defaultEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		// Every cross-check is reported, whether or not it found anything new.
		// An unchanged picture is still a fresher one, and how fresh it is, is
		// half of what the state model weighs it by.
		if checked, err := r.Read(ctx); err == nil {
			select {
			case checks <- checked:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
