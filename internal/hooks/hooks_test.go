package hooks_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/hooks"
)

// parse reads a payload that the harness is expected to act on.
func parse(t *testing.T, payload string) hooks.Event {
	t.Helper()
	event, ok := hooks.Parse([]byte(payload))
	if !ok {
		t.Fatalf("the harness ignored a payload it acts on:\n%s", payload)
	}
	return event
}

// ignored reports whether the harness passes a payload over.
func ignored(t *testing.T, payload string) bool {
	t.Helper()
	_, ok := hooks.Parse([]byte(payload))
	return !ok
}

// Ready is the state the registry cannot give, and Stop is where it comes
// from — with the message the turn ended on, which is what makes an unread
// badge worth reading.
func TestATurnEndingCarriesWhatTheSessionLastSaid(t *testing.T) {
	event := parse(t, `{
		"session_id": "11105884-3fd7-496b-a49e-833cea89a5c7",
		"transcript_path": "/Users/b/.claude/projects/x/y.jsonl",
		"cwd": "/repos/service-billing",
		"hook_event_name": "Stop",
		"last_assistant_message": "The integration suite is green (214 passed)."
	}`)

	if event.Kind != hooks.Finished {
		t.Errorf("a turn ending reads as %s, want %s", event.Kind, hooks.Finished)
	}
	if event.Session != "11105884-3fd7-496b-a49e-833cea89a5c7" {
		t.Errorf("the event names Session %q", event.Session)
	}
	if event.Snippet != "The integration suite is green (214 passed)." {
		t.Errorf("snippet = %q, want what the Session last said", event.Snippet)
	}
}

// A Blocked row is always displayed with its reason, and this is the earliest
// the harness can know it: the permission dialog is going up as the hook runs.
func TestAPermissionRequestSaysWhichToolIsWaiting(t *testing.T) {
	event := parse(t, `{
		"session_id": "s1",
		"hook_event_name": "PermissionRequest",
		"tool_name": "Bash",
		"tool_input": {"command": "git push"},
		"tool_use_id": "toolu_01"
	}`)

	if event.Kind != hooks.Blocked {
		t.Errorf("a permission request reads as %s, want %s", event.Kind, hooks.Blocked)
	}
	if event.Reason != "permission: Bash" {
		t.Errorf("reason = %q, want %q", event.Reason, "permission: Bash")
	}
}

// Not every notification means a Session is stuck. The ones that do are the
// ones that put a dialog in the pane; the rest are none of the harness's
// business, and a row that flipped to Blocked over an auth message would be a
// lie the whole Attention ordering is built on.
func TestOnlyTheNotificationsThatStopASessionBlockIt(t *testing.T) {
	for _, c := range []struct {
		kind    string
		blocked bool
	}{
		{"permission_prompt", true},
		{"elicitation_dialog", true},
		{"agent_needs_input", true},
		{"idle_prompt", false},
		{"auth_success", false},
		{"agent_completed", false},
	} {
		t.Run(c.kind, func(t *testing.T) {
			payload := `{"session_id":"s1","hook_event_name":"Notification",
				"notification_type":"` + c.kind + `","message":"Claude needs your permission to use Bash"}`

			event, ok := hooks.Parse([]byte(payload))

			if !c.blocked {
				if ok {
					t.Errorf("a %s notification reads as %s, want the harness to pass it over", c.kind, event.Kind)
				}
				return
			}
			if !ok || event.Kind != hooks.Blocked {
				t.Fatalf("a %s notification does not Block the Session (%+v, ok=%v)", c.kind, event, ok)
			}
			if event.Reason == "" {
				t.Errorf("a %s notification Blocks the Session without saying why", c.kind)
			}
		})
	}
}

// Submitting a prompt is the harness's most reliable seen signal: whatever the
// last turn left unread, you were looking at the Session when you typed.
func TestSubmittingAPromptIsReported(t *testing.T) {
	event := parse(t, `{"session_id":"s1","hook_event_name":"UserPromptSubmit","user_input":"fix the paging"}`)

	if event.Kind != hooks.Prompted {
		t.Errorf("submitting a prompt reads as %s, want %s", event.Kind, hooks.Prompted)
	}
}

// A Session starting or ending is where the harness forgets what it was
// holding about it — a resumed session id must not inherit an old badge.
func TestASessionStartingAndEndingAreReported(t *testing.T) {
	if got := parse(t, `{"session_id":"s1","hook_event_name":"SessionStart","source":"startup"}`).Kind; got != hooks.Started {
		t.Errorf("SessionStart reads as %s, want %s", got, hooks.Started)
	}
	if got := parse(t, `{"session_id":"s1","hook_event_name":"SessionEnd","reason":"logout"}`).Kind; got != hooks.Ended {
		t.Errorf("SessionEnd reads as %s, want %s", got, hooks.Ended)
	}
}

// The harness's own seen events travel the same path as Claude Code's, so the
// receiver has one way in and one thing to parse.
func TestTheHarnessOwnSeenEventTravelsTheSamePath(t *testing.T) {
	event := parse(t, string(hooks.SeenPayload("s1")))

	if event.Kind != hooks.Seen {
		t.Errorf("a seen report reads as %s, want %s", event.Kind, hooks.Seen)
	}
	if event.Session != "s1" {
		t.Errorf("the seen report names Session %q, want %q", event.Session, "s1")
	}
}

// The harness installs itself on six events but Claude Code fires many more,
// and settings are the user's to edit. Anything else is passed over rather
// than guessed at.
func TestPayloadsTheHarnessDoesNotActOnArePassedOver(t *testing.T) {
	for _, payload := range []string{
		`{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Bash"}`,
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash"}`,
		// An event from a Claude Code newer than this harness.
		`{"session_id":"s1","hook_event_name":"Transcended"}`,
		// A hook that fired for something the harness cannot place.
		`{"hook_event_name":"Stop","last_assistant_message":"done"}`,
		`not json at all`,
		``,
	} {
		if !ignored(t, payload) {
			t.Errorf("the harness acted on a payload it has no business with:\n%s", payload)
		}
	}
}

// last_assistant_message is a whole reply — markdown, code blocks, however
// long the turn ran. The Dashboard has one line of a 40-column sidepanel for
// it, and the harness holds this in memory for every Session, so it is cut
// down where it arrives rather than where it is drawn.
func TestALongLastMessageIsCutDownToASnippet(t *testing.T) {
	message := "Fixed the paging bug.\n\n```go\nfor i := range pages {\n\tdoThing(i)\n}\n```\n\n" +
		strings.Repeat("and then some more prose about it. ", 40)
	payload := `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":` +
		quoted(message) + `}`

	snippet := parse(t, payload).Snippet

	if strings.ContainsAny(snippet, "\n\t") {
		t.Errorf("the snippet is not one line: %q", snippet)
	}
	if len(snippet) > 240 {
		t.Errorf("the snippet is %d bytes, too much to hold for every Session: %q", len(snippet), snippet)
	}
	if !strings.HasPrefix(snippet, "Fixed the paging bug.") {
		t.Errorf("the snippet lost the start of what was said: %q", snippet)
	}
}

// quoted writes s as a JSON string.
func quoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Blocked is always displayed with its reason, which means there has to be
// one. A notification that arrives without a message must not leave a row
// saying a Session is stuck and refusing to say on what.
func TestABlockingNotificationAlwaysCarriesAReason(t *testing.T) {
	event := parse(t, `{"session_id":"s1","hook_event_name":"Notification",
		"notification_type":"agent_needs_input"}`)

	if event.Reason == "" {
		t.Error("a Session was Blocked without a reason")
	}
}

// The same for a permission request whose tool the payload does not name.
func TestAPermissionRequestWithoutAToolStillSaysItIsOne(t *testing.T) {
	event := parse(t, `{"session_id":"s1","hook_event_name":"PermissionRequest"}`)

	if !strings.Contains(event.Reason, "permission") {
		t.Errorf("reason = %q, want it to say a permission is being waited on", event.Reason)
	}
}
