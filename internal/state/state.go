// Package state is the harness's state model: one account of every Session,
// merged from the three things that report them.
//
// The registry is quick and undocumented — it says what a Session is doing as
// soon as it has written the file, in a shape nobody promised. The reconciler
// is slow and documented — it asks Claude Code the same question every half
// minute, in words that will still mean this after an upgrade. The hooks are
// earlier than either and partial — they say the moment something changed and
// carry what neither of the others holds, but only for Sessions running with
// the hooks installed.
//
// Laying them over one another is this package's whole job, and it comes down
// to three rules. The cross-check corrects a registry that has drifted, but
// never one that has simply moved on since it was asked. A hook edge holds a
// row only until the registry has moved since it arrived. And Ready is the
// harness's alone, because whether you have seen a finished turn is a thing
// only the harness is watching.
package state

import (
	"context"
	"time"

	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/reconciler"
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
	// alerts is what the notifier watches (§9): Blocked and Ready decisions
	// this model has already made the call on, so the notifier need only ask
	// whether Ghostty is frontmost and build the banner.
	alerts chan Alert
	// marks is what the hooks have said, by session id. It is only ever
	// touched from the watch's own goroutine.
	marks map[string]mark
	// latest is the registry's last word on the working set.
	latest []session.Session
	// checked is the reconciler's last cross-check of it.
	checked reconciler.Reconciled
}

// mark is what the hooks have said about one Session that the registry has not
// caught up with, or cannot say at all.
type mark struct {
	// finished is when a turn ended and snippet what it said, held until
	// something says you have seen it. This is where Ready comes from.
	finished time.Time
	snippet  string
	// escalated says the Ready cycle since finished has already raised its one
	// Alert, so a repeated idle_prompt — or one arriving after Seen has
	// already cleared finished — cannot raise a second.
	escalated bool
	// blocked is when the Session last said it cannot continue without you,
	// and reason is why.
	blocked time.Time
	reason  string
}

// Alert is an Attention decision worth raising beyond the Dashboard (§9) —
// what happened and to which Session, not the OS notification itself, which
// is the notifier's to build once it has checked whether Ghostty is
// frontmost.
type Alert struct {
	// Kind is what the Alert is about.
	Kind AlertKind
	// Session is Claude Code's own session id, the same join the hooks use.
	Session string
	// Reason is why a Blocked Session cannot continue; empty for a Ready one.
	Reason string
	// Snippet is what a Ready Session's turn ended on; empty for a Blocked
	// one.
	Snippet string
	// At is when the model raised it.
	At time.Time
}

// AlertKind is what an Alert reports.
type AlertKind string

const (
	// AlertBlocked: a Session has gone Blocked. It is edge-triggered — raised
	// once when the Session was not already Blocked, and not again for as
	// long as it stands, which is what "no re-nagging" means on this side of
	// the notifier.
	AlertBlocked AlertKind = "Blocked"
	// AlertReady: a Ready Session is still unseen when the first-party 60s
	// idle_prompt signal arrives. Raised once per Ready cycle — a Session
	// seen and left Ready again by a later turn can escalate again, but the
	// same turn's idle_prompt (however many times it fires) cannot raise a
	// second.
	AlertReady AlertKind = "Ready"
)

// New returns a state model that has heard nothing yet.
func New() *Model {
	return &Model{raised: make(chan hooks.Event, raised), alerts: make(chan Alert, raised), marks: map[string]mark{}}
}

// Alerts is what the notifier watches (§9). The channel is never closed: the
// model has no end of its own to signal, and a notifier that stops reading
// costs itself the Alerts and nothing this model keeps.
func (m *Model) Alerts() <-chan Alert { return m.alerts }

// emit hands an Alert to the notifier without ever blocking the goroutine that
// runs the state model on one that is not reading. A dropped Alert here is a
// notifier that is not keeping up, not a Dashboard that stops drawing over it.
func (m *Model) emit(a Alert) {
	select {
	case m.alerts <- a:
	default:
	}
}

// Watch reports the working set — the registry's, corrected by the cross-check
// and with the hooks' account over both — every time any of them says
// something, until ctx ends. The channel is closed when the watch stops.
func (m *Model) Watch(ctx context.Context, snapshots <-chan []session.Session, checks <-chan reconciler.Reconciled, events <-chan hooks.Event) <-chan []session.Session {
	merged := make(chan []session.Session)
	go m.run(ctx, snapshots, checks, events, merged)
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

func (m *Model) run(ctx context.Context, snapshots <-chan []session.Session, checks <-chan reconciler.Reconciled, events <-chan hooks.Event, merged chan<- []session.Session) {
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
		case checked, ok := <-checks:
			if !ok {
				// The reconciler has stopped. Its last cross-check stands and
				// goes on ageing, which costs it every argument with a
				// registry record newer than it — the right way for insurance
				// nobody is renewing to lapse.
				checks = nil
				continue
			}
			m.checked = checked
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
		held.finished, held.snippet, held.escalated = event.At, event.Snippet, false
	case hooks.Prompted, hooks.Seen:
		held.finished, held.snippet, held.escalated = time.Time{}, "", false
	case hooks.Blocked:
		if held.blocked.IsZero() {
			// Going Blocked, not staying there: a second permission request
			// arriving while the first is still open is not a new decision to
			// ping about, and Alerts style already keeps the first banner up
			// (§9) — this is what "no re-nagging" means before it ever
			// reaches the notifier.
			m.emit(Alert{Kind: AlertBlocked, Session: event.Session, Reason: event.Reason, At: event.At})
		}
		held.blocked, held.reason = event.At, event.Reason
	case hooks.Escalate:
		// The first-party 60s idle_prompt signal says nothing about whether
		// this Session is still unseen — only the harness's own seen-tracking
		// does, which is exactly what finished being non-zero means.
		if !held.finished.IsZero() && !held.escalated {
			held.escalated = true
			m.emit(Alert{Kind: AlertReady, Session: event.Session, Snippet: held.snippet, At: event.At})
		}
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
	working := m.reconciled()
	for i, s := range working {
		working[i] = merge(s, m.marks[s.ID])
	}
	return working
}

// reconciled is the registry's working set with the last cross-check laid over
// it: the Sessions the registry never reported added to it, and the ones it
// describes differently corrected.
//
// The two are matched on the process. That is the field the harness has
// checked — it names something alive, and no two live Sessions share one —
// while the session id comes from a file whose shape this cross-check exists
// to insure against.
//
// Nothing here takes a row off. The cross-check is always the older picture of
// the two, so a Session it has not heard of is far likelier to be one that has
// just started than one that has gone. Rows it added are another matter: the
// registry is not reporting them, so nothing is liveness-checking them either,
// and one whose Session has ended stays up until the next cross-check drops it
// — a tick, which is what the whole component is accurate to.
func (m *Model) reconciled() []session.Session {
	working := make([]session.Session, len(m.latest), len(m.latest)+len(m.checked.Sessions))
	copy(working, m.latest)

	// running is where each process already sits in the working set. Rows the
	// cross-check adds go into it too, so that one process is one row however
	// many times it was reported — the Dashboard keys its rows by process, and
	// a second row for one could never be selected.
	running := make(map[int]int, len(working))
	for i, s := range working {
		running[s.PID] = i
	}
	for _, checked := range m.checked.Sessions {
		i, known := running[checked.PID]
		if known {
			working[i] = correct(working[i], checked, m.checked.At)
			continue
		}
		if checked.State == "" {
			// The registry never reported this Session and the cross-check
			// cannot read what it is doing. Idle is the right answer here after
			// all: there is nothing to fall back on, and a row you can see and
			// jump to beats a Session nothing on the Dashboard mentions.
			checked.State = session.Idle
		}
		// As far as anything here knows, it has been in that state since the
		// harness first heard of it.
		checked.Since = m.checked.At
		running[checked.PID] = len(working)
		working = append(working, checked)
	}
	return working
}

// correct settles what one Session is doing between the registry's account of
// it and the cross-check's.
//
// The cross-check wins. Where the two disagree, one of them read an
// undocumented file that may have moved underneath it and the other asked
// Claude Code a documented question — and this whole component exists because
// only the second of those can be relied on to still mean what it meant.
//
// A registry that has moved the Session since the picture was taken is the one
// exception, and it is not really a disagreement: the cross-check is the
// slower of the two by design, and handing a live row back to a picture half a
// minute old would be the flicker this must not have. A record that cannot say
// when it last moved cannot make that case — and a clock the harness can no
// longer read is drift of exactly the kind being insured against.
// A cross-check that could not read a Session's status is the other exception,
// and the plainest one: it has no opinion, so there is nothing to prefer.
func correct(s, checked session.Session, at time.Time) session.Session {
	if checked.State == "" || s.State == checked.State {
		return s
	}
	if !s.Since.IsZero() && s.Since.After(at) {
		return s
	}
	// The reason the registry gave goes with the state just overruled — it was
	// about that state — and the cross-check has none to put in its place. A
	// Blocked row can come out of here with nothing under it, which is worse
	// than a Blocked row with its reason and better than an Idle row over a
	// Session that is stopped waiting for you.
	//
	// The wait age runs from the cross-check. That under-reports it, and the
	// alternatives are worse: no time at all reads as a Session waiting since
	// 1970 to a tree that sorts Attention by longest-waiting first, and keeps
	// the hooks off the row besides, since a hook edge can only be weighed
	// against a clock.
	s.State, s.Reason, s.Since = checked.State, "", at
	return s
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
