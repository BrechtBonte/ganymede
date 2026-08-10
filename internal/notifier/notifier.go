// Package notifier turns the state model's Alerts into OS notifications —
// attention reaching you beyond the Dashboard (§9), which for this harness
// means whenever Ghostty is not the window in front of you.
//
// The state model has already made the call on what deserves a notification
// and why (state.Alert): a Session going Blocked, or a Ready one still unseen
// when the first-party 60s idle_prompt signal arrives. This package's own job
// is smaller — find out what the Alert is about (which repo, which ticket,
// which pid to jump to if it is clicked), ask whether Ghostty is frontmost,
// and build the one banner that decision is worth.
package notifier

import (
	"context"
	"path/filepath"

	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/state"
	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// Notification is what the notifier has decided to put in front of you.
type Notification struct {
	// Title names the repo the Session belongs to, and its ticket when it has
	// one.
	Title string
	// Body is the reason a Blocked Session cannot continue, or what a Ready
	// one's turn ended on.
	Body string
	// Sound is true on Blocked only — Ready's escalation is silent (§9).
	Sound bool
	// Click is run when the notification is clicked. Empty when there is no
	// Session left to jump to.
	Click []string
}

// Sender puts a Notification in front of you, on whatever the OS channel is.
type Sender interface {
	Send(Notification) error
}

// Frontmost reports whether Ghostty is the window in front of you. No banner
// fires while it is — the sidepanel already shows the same state.
type Frontmost func() (bool, error)

// Tickets is which ticket a Session's checkout is about — the same question
// the Dashboard asks, so a notification's title reads the same as the row it
// is about.
type Tickets interface {
	Of(dir, root string) ticket.Key
}

// Notifier watches the working set for what each Alert is about, and turns
// the ones that survive being focus-aware into Notifications.
type Notifier struct {
	// Send puts a Notification in front of you. Nil means alerts are decided
	// but never shown — a Notifier a caller has not finished wiring up rather
	// than one asked to be silent.
	Send Sender
	// Frontmost says whether to hold every banner back. Nil never holds one
	// back: an alert this sure of itself fails open rather than silently.
	Frontmost Frontmost
	// Tickets names a Notification's title alongside its repo. Nil leaves
	// every title as the repo alone.
	Tickets Tickets
	// Click builds the command a clicked Notification runs, given the pid to
	// jump to. Nil leaves every Notification unclickable.
	Click func(pid int) []string
}

// Run watches sessions for what each Alert names — the directory, the pid to
// jump to — and turns alerts into Notifications until either channel closes.
//
// sessions is read continuously so the notifier's picture of a Session never
// falls behind by more than the last tick; alerts is what actually drives a
// Notification, since a working set on its own is never worth interrupting
// you over.
func (n Notifier) Run(ctx context.Context, sessions <-chan []session.Session, alerts <-chan state.Alert) {
	known := map[string]session.Session{}
	for {
		select {
		case <-ctx.Done():
			return
		case set, ok := <-sessions:
			if !ok {
				// The working set has stopped arriving. What is already known
				// about each Session stands — better than refusing every
				// Alert from here on for want of a title.
				sessions = nil
				continue
			}
			known = indexed(set)
		case alert, ok := <-alerts:
			if !ok {
				return
			}
			n.fire(alert, known[alert.Session])
		}
	}
}

// indexed is a working set keyed by Claude Code's own session id, the same
// join an Alert carries.
func indexed(sessions []session.Session) map[string]session.Session {
	byID := make(map[string]session.Session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	return byID
}

// fire decides whether alert is worth a Notification, and sends the one it
// builds.
func (n Notifier) fire(alert state.Alert, s session.Session) {
	if n.Frontmost != nil {
		// A question that could not be answered fails open: an alert this
		// harness has already decided is worth raising must not go missing
		// because System Events could not be asked.
		if front, err := n.Frontmost(); err == nil && front {
			return
		}
	}

	notification := Notification{Title: n.title(s)}
	switch alert.Kind {
	case state.AlertBlocked:
		notification.Body, notification.Sound = alert.Reason, true
	case state.AlertReady:
		notification.Body = alert.Snippet
	default:
		return
	}
	if n.Click != nil && s.PID != 0 {
		notification.Click = n.Click(s.PID)
	}
	if n.Send == nil {
		return
	}
	// Best effort: a Notification that could not be shown is no different
	// from one that was dismissed unread, and there is nowhere here that
	// could tell you about it without corrupting the Dashboard it is drawn
	// beside.
	_ = n.Send.Send(notification)
}

// title is a Notification's own reading of a row: the repo the Session
// belongs to, and its ticket when it has one — the same anatomy §9 asks for,
// "service-ai-assistant · FIRE-1234".
func (n Notifier) title(s session.Session) string {
	root := repo.Root(s.Dir)
	name := filepath.Base(root)
	if n.Tickets == nil {
		return name
	}
	if key := n.Tickets.Of(s.Dir, root); key != "" {
		return name + " · " + string(key)
	}
	return name
}
