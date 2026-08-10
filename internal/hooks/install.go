package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// reads are the events the harness installs itself on: everything the state
// model needs and nothing else. Claude Code fires a great many more, and a
// hook the harness does not read is a command run inside every Session's turn
// for nothing.
var reads = []string{
	// Ready, carrying the message that makes it worth reading.
	"Stop",
	// Blocked with its reason, ahead of the registry. PermissionRequest is a
	// reporter only: it never answers the dialog, it only says one has gone up.
	"PermissionRequest",
	"Notification",
	// Seen: a new prompt means you were looking at the Session.
	"UserPromptSubmit",
	// When to start holding something about a Session, and when to let go.
	"SessionStart",
	"SessionEnd",
}

// Command is what Claude Code runs on each event: this binary, handing the
// payload it is given to the Dashboard. The path is quoted because a harness
// built in a directory with a space in its name is still a harness.
func Command(binary string) string {
	return "'" + strings.ReplaceAll(binary, "'", `'\''`) + "' hook"
}

// Install makes Claude Code report to the harness, by putting command on every
// event the state model reads.
//
// It is safe to repeat, which it has to be: `ganymede up` installs on every
// run. The harness's own entries are taken out before they are put back, so a
// second install replaces the first rather than doubling it, and a command
// left behind by a binary that has since moved goes with them.
//
// The harness's notifier is also made the single OS channel (§9): Claude
// Code's own built-in desktop notification is turned off, and any existing
// osascript wiring that put a notification up itself is absorbed alongside
// the harness's own stale entries — otherwise a Blocked or Ready Session would
// bank a notification twice over.
//
// Everything else in the file is the user's — their model, their permissions,
// their own hooks, their secrets. It is read as an unopened tree and written
// back whole, so a key this harness has never heard of survives it.
func Install(settings, command string) error {
	doc, err := readSettings(settings)
	if err != nil {
		return err
	}

	installed := map[string]any{}
	if existing := doc["hooks"]; existing != nil {
		hooked, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: hooks is not a set of events (the harness will not rewrite settings it cannot read)", settings)
		}
		installed = hooked
	}
	for _, event := range reads {
		var groups []any
		if existing := installed[event]; existing != nil {
			listed, ok := existing.([]any)
			if !ok {
				return fmt.Errorf("%s: the %s hook is not a list (the harness will not rewrite settings it cannot read)", settings, event)
			}
			groups = listed
		}
		installed[event] = append(without(groups), group(command))
	}
	doc["hooks"] = installed
	// The one setting outside "hooks" this harness ever touches: unconditional,
	// because the notifier being the sole channel is not a preference this
	// harness offers a way out of (§9).
	doc["preferredNotifChannel"] = "notifications_disabled"

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", settings, err)
	}
	return config.Replace(settings, append(body, '\n'))
}

// DefaultSettings is Claude Code's user-level settings file. Hooks go there
// rather than in a project so that every repo the harness shows is covered.
func DefaultSettings() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// group is one hook entry, on no matcher: the harness reads every event of the
// kind it asked for.
func group(command string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
			// Nothing waits on the harness. The payload is handed over and the
			// Session gets on with its turn — which is what keeps the pane's
			// permission dialog from lagging behind the Dashboard.
			"async": true,
		}},
	}
}

// without returns groups with the harness's own commands taken out, along with
// any group that held nothing else. Everything it does not recognise is left
// exactly where it stands: settings are the user's file, and a shape this
// harness cannot read is more likely a Claude Code newer than it than a
// mistake.
func without(groups []any) []any {
	kept := make([]any, 0, len(groups))
	for _, entry := range groups {
		found, isGroup := entry.(map[string]any)
		if !isGroup {
			kept = append(kept, entry)
			continue
		}
		handlers, isList := found["hooks"].([]any)
		if !isList {
			kept = append(kept, entry)
			continue
		}

		remaining := make([]any, 0, len(handlers))
		for _, handler := range handlers {
			if !isOurs(handler) && !isNotificationWiring(handler) {
				remaining = append(remaining, handler)
			}
		}
		switch {
		case len(remaining) == len(handlers):
			kept = append(kept, entry)
		case len(remaining) > 0:
			found["hooks"] = remaining
			kept = append(kept, found)
		}
	}
	return kept
}

// isOurs reports whether a hook handler is one the harness installed. It goes
// by what the command runs rather than by a marker of its own, because the one
// thing that changes between installs is where the binary lives.
//
// It is read exactly the way Command writes it — this binary, then the hook
// subcommand — and nothing looser. Anything that is merely named after the
// harness, a wrapper of the user's own among them, is theirs to keep: the only
// thing worse than installing twice is deleting somebody else's hook.
func isOurs(handler any) bool {
	found, ok := handler.(map[string]any)
	if !ok {
		return false
	}
	command, _ := found["command"].(string)
	binary, forwards := strings.CutSuffix(strings.TrimSpace(command), " hook")
	if !forwards {
		return false
	}
	return filepath.Base(unquote(binary)) == "ganymede"
}

// isNotificationWiring reports whether a hook handler is the osascript
// recipe for a desktop notification — the one Claude Code's own docs suggest,
// and the one this machine already had wired on Stop and Notification before
// the harness existed. The harness's notifier is now the sole OS channel
// (§9), so this is absorbed alongside the harness's own stale entries rather
// than left beside it, doubling every Blocked and Ready banner.
//
// It goes by the shape of the command — osascript told to display a
// notification — rather than by the binary alone: an osascript hook doing
// something else entirely (a Finder command, a dialog) is none of the
// harness's business.
func isNotificationWiring(handler any) bool {
	found, ok := handler.(map[string]any)
	if !ok {
		return false
	}
	command, _ := found["command"].(string)
	return strings.Contains(command, "osascript") && strings.Contains(command, "display notification")
}

// unquote takes off the shell quoting Command put on.
func unquote(arg string) string {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || arg[0] != '\'' || arg[len(arg)-1] != '\'' {
		return arg
	}
	return strings.ReplaceAll(arg[1:len(arg)-1], `'\''`, "'")
}

// readSettings reads settings.json as an unopened tree.
//
// A file that will not parse is left alone and reported: a hand-edited
// settings.json with a trailing comma is a thing to be told about, not a thing
// to lose. So is an absent one — Claude Code writes it on first run, and the
// harness must not need it to have run first.
func readSettings(path string) (map[string]any, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	// Numbers keep the exact shape the user wrote them in. Decoded the usual
	// way they all become floats, and a large one is written back in exponent
	// notation — which is a different value to anything reading it.
	decoder.UseNumber()

	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("read %s: %w (the harness will not rewrite settings it cannot parse)", path, err)
	}
	return doc, nil
}
