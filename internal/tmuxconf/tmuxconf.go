// Package tmuxconf installs the harness's tmux configuration.
//
// The harness owns one fragment file and makes the user's tmux.conf source it,
// so the user's own configuration stays theirs to edit.
package tmuxconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// The markers delimit the harness's block in the user's tmux.conf, so a
// re-install can replace it in place rather than append a second copy.
const (
	beginMarker = "# >>> ganymede >>>"
	endMarker   = "# <<< ganymede <<<"
)

// Layout locates the files Install touches.
type Layout struct {
	// Fragment is the harness-owned config file holding the settings.
	Fragment string
	// UserConf is the user's tmux.conf, which Install makes source Fragment.
	UserConf string
}

// fragmentBody is what the harness needs from every tmux server running a
// Session: passthrough so an application can talk past tmux to the emulator,
// and focus events so the Dashboard learns when a Session has been seen.
const fragmentBody = `# Managed by ganymede. Edit ganymede, not this file.
set -g allow-passthrough on
set -g focus-events on
`

// DefaultLayout resolves the standard locations: the fragment under the
// harness's own config directory, and whichever tmux.conf the user already
// loads — tmux reads the XDG path in preference to the home dotfile.
func DefaultLayout() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, fmt.Errorf("locate home directory: %w", err)
	}

	userConf := filepath.Join(home, ".tmux.conf")
	if xdg := filepath.Join(config.Home(home), "tmux", "tmux.conf"); exists(xdg) {
		userConf = xdg
	}

	return Layout{
		Fragment: filepath.Join(config.Home(home), "ganymede", "tmux.conf"),
		UserConf: userConf,
	}, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Install writes the fragment and makes UserConf source it.
func Install(l Layout) error {
	if err := config.Replace(l.Fragment, []byte(fragmentBody)); err != nil {
		return fmt.Errorf("write fragment: %w", err)
	}
	existing, err := os.ReadFile(l.UserConf)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", l.UserConf, err)
	}
	if err := config.Replace(l.UserConf, []byte(withBlock(string(existing), l.Fragment))); err != nil {
		return err
	}
	return nil
}

// withBlock returns conf carrying exactly one harness block: the existing one
// rewritten where it stands if conf already has it, appended otherwise.
//
// It works a line at a time and never drops a line it cannot account for. A
// block whose end marker has gone missing is repaired by replacing the opening
// marker alone — everything below it is the user's, however it got there.
func withBlock(conf, fragment string) string {
	block := []string{beginMarker, fmt.Sprintf("source-file -q %q", fragment), endMarker}

	lines := strings.Split(conf, "\n")
	trailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	begin := indexOfLine(lines, beginMarker, 0)
	if begin < 0 {
		return join(append(lines, block...))
	}

	end := indexOfLine(lines, endMarker, begin+1)
	if end < 0 {
		end = begin
	}
	kept := append(append(append([]string{}, lines[:begin]...), block...), lines[end+1:]...)
	return join(kept)
}

// indexOfLine finds the first line at or after start whose content is marker,
// ignoring the whitespace an editor may have left around it.
func indexOfLine(lines []string, marker string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			return i
		}
	}
	return -1
}

func join(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// dockBody configures the dock server. The dock is only a frame: it holds the
// sidepanel and the working client side by side and otherwise stays out of the
// way, so its prefix is disabled and every key reaches the client inside the
// pane. It needs passthrough and focus events of its own, because both have to
// travel through the dock to reach the emulator and the Sessions behind it.
const dockBody = `# Managed by ganymede. Edit ganymede, not this file.
set -g prefix None
set -g prefix2 None
set -g status off
set -g mouse off
set -g escape-time 0
set -g base-index 0
setw -g pane-base-index 0
set -g allow-passthrough on
set -g focus-events on

# %s moves between the sidepanel and the working client.
bind -n %s select-pane -t :.+

# The sidepanel keeps its width however the window is resized. window-resized
# is the one that matters: client-resized fires before tmux has recalculated
# the layout, so a width set there is scaled away by the resize that follows.
set-hook -g client-attached 'resize-pane -t :.0 -x %d'
set-hook -g client-resized 'resize-pane -t :.0 -x %d'
set-hook -g window-resized 'resize-pane -t :.0 -x %d'
`

// FocusKey moves focus between the sidepanel and the working client. The dock
// has no prefix, so this is a bare key in the root table.
const FocusKey = "M-g"

// WriteDockConf writes the dock server's configuration.
func WriteDockConf(path string, sidepanelWidth int) error {
	body := fmt.Sprintf(dockBody, FocusKey, FocusKey, sidepanelWidth, sidepanelWidth, sidepanelWidth)
	if err := config.Replace(path, []byte(body)); err != nil {
		return fmt.Errorf("write dock config: %w", err)
	}
	return nil
}
