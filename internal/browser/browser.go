// Package browser hands a link to whatever the desktop reads links with.
//
// It is one command, and that is the point. The harness knows a ticket's ID and
// the address that ID makes; everything else about that ticket is the browser's
// business, and keeping it that way is what a harness with no JIRA API costs and
// buys.
package browser

import (
	"fmt"
	"os/exec"
	"strings"
)

// desktop is macOS's own launcher, which opens a link in whichever browser you
// have told the system to use.
const desktop = "open"

// Browser opens links.
type Browser struct {
	// Binary is the command that opens them. Empty means the desktop's.
	Binary string
}

// Open shows url in the browser, and returns once the browser has been asked.
//
// The wait is deliberate: the command is the launcher, not the browser, and it
// is gone in a moment. Waiting is what turns "no such command" and "that is not
// a link I can open" into something the Dashboard can put in front of you,
// rather than a keystroke that silently did nothing.
func (b Browser) Open(url string) error {
	binary := b.Binary
	if binary == "" {
		binary = desktop
	}
	if out, err := exec.Command(binary, url).CombinedOutput(); err != nil {
		if complaint := strings.TrimSpace(string(out)); complaint != "" {
			return fmt.Errorf("open %s: %w: %s", url, err, complaint)
		}
		return fmt.Errorf("open %s: %w", url, err)
	}
	return nil
}
