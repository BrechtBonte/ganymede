// Package state is the harness's state model: one account of every Session,
// merged from the two things that report them.
//
// The registry is authoritative and slow — it says what a Session is doing,
// but only once it has written the file. The hooks are early and partial —
// they say the moment something changed and carry what the registry never
// holds, but only for Sessions running with the hooks installed.
//
// Laying one over the other is this package's whole job, and it comes down to
// two rules. A hook edge holds a row only until the registry has moved since
// it arrived; and Ready is the harness's alone, because whether you have seen
// a finished turn is a thing only the harness is watching.
package state

import (
	"context"
	"time"

	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// raised is how many events the harness can raise itself before it has to drop
// one. Only the Dashboard raises them, one per keystroke.
const raised = 8

// Model is what the Dashboard draws: every Session the registry reports, with
// what the hooks have said about it since laid over the top.
type Model struct {
	// raised carries the events the harness raises itself — a jump clearing
	// Ready — down the same path as the ones Sessions report.
	raised chan hooks.Event
	// marks is what the hooks have said, by session id. It is only ever
	// touched from the watch's own goroutine.
	marks map[string]mark
	// latest is the registry's last word on the working set.
	latest []session.Session
}

// mark is what the hooks have said about one Session that the registry has not
// caught up with, or cannot say at all.
type mark struct {
	// finished is when a turn ended and snippet what it said, held until
	// something says you have seen it. This is where Ready comes from.
	finished time.Time
	snippet  string
	// blocked is when the Session last said it cannot continue without you,
	// and reason is why.
	blocked time.Time
	reason  string
}

// New returns a state model that has heard nothing yet.
func New() *Model {
	return &Model{raised: make(chan hooks.Event, raised), marks: map[string]mark{}}
}

// Watch reports the working set — the registry's, with the hooks' account over
// it — every time either of them says something, until ctx ends. The channel
// is closed when the watch stops.
func (m *Model) Watch(ctx context.Context, snapshots <-chan []session.Session, events <-chan hooks.Event) <-chan []session.Session {
	merged := make(chan []session.Session)
	go m.run(ctx, snapshots, events, merged)
	return merged
}

// Seen says a Session has been put in front of you, which is what clears
// Ready. The Dashboard calls it as it jumps, rather than waiting for tmux to
// report the focus, because it is the one that moved you.
//
// It never blocks the caller: it is called from the goroutine drawing the
// Dashboard, and a model too busy to take an event is a model about to redraw
// anyway.
func (m *Model) Seen(id string) {
	select {
	case m.raised <- hooks.Event{Kind: hooks.Seen, Session: id, At: time.Now()}:
	default:
	}
}

func (m *Model) run(ctx context.Context, snapshots <-chan []session.Session, events <-chan hooks.Event, merged chan<- []session.Session) {
	defer close(merged)

	for {
		select {
		case <-ctx.Done():
			return
		case set, ok := <-snapshots:
			if !ok {
				// The registry watch has stopped. What it last said is better
				// than an empty Dashboard, and the hooks keep working over it.
				snapshots = nil
				continue
			}
			m.arrived(set)
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			m.apply(event)
		case event := <-m.raised:
			m.apply(event)
		}

		select {
		case merged <- m.merged():
		case <-ctx.Done():
			return
		}
	}
}

// arrived takes the registry's word for the working set, and lets go of what
// it has moved past.
func (m *Model) arrived(set []session.Session) {
	m.latest = set
	for _, s := range set {
		held, ok := m.marks[s.ID]
		if !ok || held.blocked.IsZero() {
			continue
		}
		// The registry has the Session doing something other than waiting, and
		// moved it there after the hook said it was stuck: whatever it was
		// waiting for has been answered, and the reason with it. A registry
		// that cannot say when it moved is taken at its word rather than
		// argued with, or one record without a clock would leave a row reading
		// Blocked for the rest of the Session's life.
		if s.State != session.Blocked && (s.Since.IsZero() || s.Since.After(held.blocked)) {
			held.blocked, held.reason = time.Time{}, ""
			m.hold(s.ID, held)
		}
	}
}

// apply takes in what a Session reported.
func (m *Model) apply(event hooks.Event) {
	if event.Session == "" {
		return
	}
	held := m.marks[event.Session]

	switch event.Kind {
	case hooks.Started, hooks.Ended:
		// A Session beginning or going leaves nothing behind: a badge that
		// outlived the Session it was about could never be cleared.
		delete(m.marks, event.Session)
		return
	case hooks.Finished:
		held.finished, held.snippet = event.At, event.Snippet
	case hooks.Prompted, hooks.Seen:
		held.finished, held.snippet = time.Time{}, ""
	case hooks.Blocked:
		held.blocked, held.reason = event.At, event.Reason
	default:
		return
	}
	m.hold(event.Session, held)
}

// hold keeps what the hooks have said about a Session, and forgets one they
// have nothing left to say about. Session ids are not reused, so a mark left
// behind could never be taken for another Session — but the map would grow for
// as long as the Dashboard is up.
func (m *Model) hold(id string, held mark) {
	if held == (mark{}) {
		delete(m.marks, id)
		return
	}
	m.marks[id] = held
}

// merged is the working set as the Dashboard should draw it.
func (m *Model) merged() []session.Session {
	working := make([]session.Session, len(m.latest))
	for i, s := range m.latest {
		working[i] = merge(s, m.marks[s.ID])
	}
	return working
}

// merge lays what the hooks have said over the registry's account of one
// Session.
//
// The order is the model. A hook edge takes the row while it is ahead of the
// registry, and hands it back the moment the registry has moved since — the
// hooks are early, never a second opinion. Which of them is ahead is a
// question only a clock can settle, so against a registry record carrying no
// time at all the hooks do not get to answer it. What they alone carry is a
// reason, which a Blocked row is always displayed with and the registry does
// not always have. And Ready is neither of theirs: it is an Idle Session with
// a turn nothing has said you saw.
func merge(s session.Session, held mark) session.Session {
	switch {
	case !held.blocked.IsZero() && !s.Since.IsZero() && held.blocked.After(s.Since):
		s.State, s.Reason = session.Blocked, held.reason
	case s.State == session.Blocked && s.Reason == "":
		s.Reason = held.reason
	case s.State == session.Idle && !held.finished.IsZero():
		s.State, s.Snippet = session.Ready, held.snippet
	}
	return s
}
