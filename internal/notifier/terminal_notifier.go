package notifier

import (
	"fmt"
	"os/exec"
	"strings"
)

// TerminalNotifier sends a Notification through terminal-notifier, the one
// macOS notifier that can run a command when you click it — AppleScript's own
// `display notification` cannot (§9).
//
// Its own notification style has to be set to Alerts by hand, once, in System
// Settings ▸ Notifications ▸ terminal-notifier — nothing here can do that for
// you, and nothing here needs to: that is what makes a Blocked banner sticky
// until dismissed or resolved.
type TerminalNotifier struct {
	// Binary is the terminal-notifier executable. Empty means whatever PATH
	// resolves it to.
	Binary string
}

// Send shows n, sound and all.
func (t TerminalNotifier) Send(n Notification) error {
	binary := t.Binary
	if binary == "" {
		binary = "terminal-notifier"
	}

	args := []string{"-title", n.Title, "-message", n.Body}
	if n.Sound {
		args = append(args, "-sound", "Ping")
	}
	if len(n.Click) > 0 {
		args = append(args, "-execute", shellJoin(n.Click))
	}

	if out, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("terminal-notifier: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellJoin writes argv as one shell command, the shape terminal-notifier's
// -execute wants: it hands the whole string to `/bin/sh -c` rather than
// exec'ing argv itself.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
