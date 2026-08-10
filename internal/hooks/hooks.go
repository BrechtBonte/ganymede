// Package hooks carries Claude Code's hook events to the Dashboard.
//
// The registry says what a Session is doing; the hooks say the moment it
// changed, and carry what the registry never holds — the message a turn ended
// on, the tool a permission prompt is waiting for. Claude Code runs a command
// per event, the harness installs itself as that command, and all that command
// does is hand the payload to the Dashboard over a local socket.
//
// Nothing here may cost a Session anything. Hook commands run inside the
// Session's own turn, so the forwarder gives up rather than waits, and a
// Dashboard that is not running is not the Session's problem.
package hooks

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// Kind is what an event tells the harness. It is the harness's own vocabulary
// rather than Claude Code's: the payloads are the shape of somebody else's
// feature set, and this is the short list the state model acts on.
type Kind string

const (
	// Started: a Session has begun, and the harness knows nothing about it yet.
	Started Kind = "Started"
	// Ended: the Session is Gone.
	Ended Kind = "Ended"
	// Prompted: you submitted a prompt, which starts a turn — and means you
	// were looking at the Session when you did.
	Prompted Kind = "Prompted"
	// Finished: a turn ended, carrying what it last said. This is where Ready
	// comes from.
	Finished Kind = "Finished"
	// Blocked: the Session cannot continue without your decision, and says
	// why.
	Blocked Kind = "Blocked"
	// Seen: the harness's own — focus landed on the Session's pane, or you
	// jumped to it, either of which clears Ready.
	Seen Kind = "Seen"
)

// Event is one thing that happened to a Session.
type Event struct {
	// Kind is what happened.
	Kind Kind
	// Session is Claude Code's session id, which is the only name a hook
	// payload gives the Session it is about.
	Session string
	// Reason is why a Blocked Session cannot continue.
	Reason string
	// Snippet is what a Finished turn last said.
	Snippet string
	// At is when the receiver heard about it. Hook payloads carry no clock of
	// their own, and the state model compares this against the registry's, so
	// it is stamped on arrival rather than parsed.
	At time.Time
}

// payload is the part of a hook's stdin JSON the harness reads. Every field is
// optional: hooks the harness installs on one event get payloads shaped for
// another as Claude Code grows, and a field that has moved costs the event it
// belonged to rather than the read.
type payload struct {
	Event                string `json:"hook_event_name"`
	SessionID            string `json:"session_id"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	ToolName             string `json:"tool_name,omitempty"`
	NotificationType     string `json:"notification_type,omitempty"`
	Message              string `json:"message,omitempty"`
}

// seenEvent names the harness's own event on the wire. It is deliberately not
// a name Claude Code could grow into.
const seenEvent = "GanymedeSeen"

// Limits on what the harness keeps. A snippet is drawn on one line of a
// 40-column sidepanel and held for every Session in the working set; a reason
// is a phrase, and a Blocked notification's message is written for a banner.
const (
	snippetLimit = 200
	reasonLimit  = 120
)

// Parse reads a hook payload, reporting whether it says anything the state
// model acts on. Everything else — the events Claude Code fires that the
// harness never installed itself on, an event from a newer Claude Code, a
// payload that will not parse — is passed over rather than guessed at.
func Parse(body []byte) (Event, bool) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, false
	}
	// An event the harness cannot pin on a Session has nowhere to land: the
	// session id is the only join between a payload and a row.
	if p.SessionID == "" {
		return Event{}, false
	}
	event := Event{Session: p.SessionID}

	switch p.Event {
	case "SessionStart":
		event.Kind = Started
	case "SessionEnd":
		event.Kind = Ended
	case "UserPromptSubmit":
		event.Kind = Prompted
	case "Stop":
		event.Kind = Finished
		event.Snippet = oneLine(p.LastAssistantMessage, snippetLimit)
	case "PermissionRequest":
		event.Kind = Blocked
		event.Reason = permissionReason(p.ToolName)
	case "Notification":
		if !stops(p.NotificationType) {
			return Event{}, false
		}
		event.Kind = Blocked
		// A Blocked row is always displayed with its reason, so there has to
		// be one. The notification's own kind is a poor reason but an honest
		// one, and better than a row that says a Session is stuck and will not
		// say on what.
		event.Reason = oneLine(p.Message, reasonLimit)
		if event.Reason == "" {
			event.Reason = p.NotificationType
		}
	case seenEvent:
		event.Kind = Seen
	default:
		return Event{}, false
	}
	return event, true
}

// SeenPayload is what the harness sends itself when a Session has been put in
// front of you, in the same envelope Claude Code uses so the receiver has one
// way in.
func SeenPayload(id string) []byte {
	body, err := json.Marshal(payload{Event: seenEvent, SessionID: id})
	if err != nil {
		// payload is two strings; encoding/json cannot fail on it.
		return nil
	}
	return body
}

// stops reports whether a notification means the Session has stopped and is
// waiting on you. The others — an idle nudge, an auth message, a subagent
// reporting in — leave the Session where it is, and a row that flipped to
// Blocked over one of them would be a lie the whole Attention ordering rests
// on.
func stops(kind string) bool {
	switch kind {
	case "permission_prompt", "elicitation_dialog", "agent_needs_input":
		return true
	}
	return false
}

// permissionReason phrases a permission request the way the registry phrases
// its own, so a row reads the same whichever of the two got there first.
func permissionReason(tool string) string {
	if tool == "" {
		return "permission"
	}
	return session.PermissionPrefix + oneLine(tool, reasonLimit)
}

// oneLine flattens text into something a row can hold: one line, no runs of
// whitespace, and short enough that holding it for every Session in the
// working set costs nothing.
func oneLine(s string, limit int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s))
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= limit {
		return s
	}
	// Cut on a rune boundary, and say that there is more.
	cut := limit
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// utf8Start reports whether b begins a UTF-8 rune rather than continuing one.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
