// Package topology brings up the docked sidepanel: one emulator window holding
// two tmux clients side by side, the left one permanently showing the Dashboard
// and the right one being the working client.
package topology

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// DashboardSession is the persistent tmux session hosting the Dashboard. The
// name is reserved: no repo may take it.
const DashboardSession = "ganymede"

// WorkingSessionName is the tmux session name for the repo containing dir:
// one session per repo, named after the repo. Directories outside any repo are
// named after themselves.
func WorkingSessionName(dir string) (string, error) {
	name, err := repoName(dir)
	if err != nil {
		return "", err
	}
	name = addressable(name)
	if name == DashboardSession {
		// The Dashboard owns this name; steering the working client at it
		// would put a second Dashboard on the right.
		return name + "-repo", nil
	}
	return name, nil
}

func repoName(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return filepath.Base(strings.TrimSpace(string(out))), nil
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	return filepath.Base(abs), nil
}

// addressable makes a name tmux can still find. In a target, "." separates the
// window from the pane and ":" separates the session from the window — tmux
// splits on them before the "=" exact-match prefix is considered, so a session
// carrying either can be created and then never addressed again.
func addressable(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' || r == ':' {
			return '-'
		}
		return r
	}, name)
}
