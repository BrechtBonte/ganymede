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

// Layout locates the files Install touches, and names the harness itself.
type Layout struct {
	// Fragment is the harness-owned config file holding the settings.
	Fragment string
	// UserConf is the user's tmux.conf, which Install makes source Fragment.
	UserConf string
	// Command is the ganymede binary, which tmux runs when focus lands on a
	// pane. A Layout with no command installs no such hook: a harness that
	// cannot say where it lives must not leave tmux running something that is
	// not there.
	Command string
}

// settings are what the harness needs from every tmux server running a
// Session: passthrough so an application can talk past tmux to the emulator,
// and focus events, which is tmux agreeing to say when a pane is looked at.
const settings = `# Managed by ganymede. Edit ganymede, not this file.
set -g allow-passthrough on
set -g focus-events on
`

// seenHook is what turns tmux's focus into the harness's seen-tracking, and
// with it Ready back into Idle. The pane's own process is all tmux can say
// about what is running there; the harness works the Sessions out from it.
//
// The harness's path goes in an option of its own rather than into the hook,
// so that the hook can hand it to the shell through #{q:}, which is tmux's own
// escaping and the only thing that survives every character a path is allowed
// to hold — including the quote that would otherwise end the hook's own line
// and take the whole fragment down with it.
//
// The rest is left literal, so #{pane_pid} is still a format when the hook
// fires rather than something expanded at load to a pane nobody was looking
// at; -b leaves tmux free the moment it has started the command; and the hook
// is set rather than appended, so that re-sourcing this fragment onto a server
// that already had it — which is what bringing the harness up does — leaves
// one hook rather than another copy of it. That does mean the harness owns
// this hook: a pane-focus-in of the user's own would be replaced by it.
const seenHook = `
set -g @ganymede-seen "%s"
set-hook -g pane-focus-in 'run-shell -b "#{q:@ganymede-seen} seen #{pane_pid}"'
`

// AttentionOption is where the Dashboard writes the ambient attention strip.
// tmux only places the option in the status line; everything the strip says is
// the Dashboard's.
const AttentionOption = "@ganymede-attention"

// strip hands the right-hand end of the status line to the harness: the counts
// of what is waiting on you, under your eye line in the Session you are working
// in rather than only over in the sidepanel.
//
// The Dashboard writes the whole strip — marks, counts and colours — into an
// option of its own, so a status line redrawn on every keystroke costs tmux
// nothing but a lookup, and a server whose Dashboard has never written to it
// draws an empty strip rather than an error. Setting the option is enough to
// put it on screen: tmux redraws its clients when an option changes.
//
// The harness owns these two settings. A status line the user had turned off
// is turned back on, because a strip nobody can see is not a strip, and a
// right-hand segment of the user's own is replaced by this one.
const strip = `
set -g status on
set -g status-right "#{` + AttentionOption + `}"
`

// fragment is the harness's tmux configuration for a Layout. What the harness
// cannot work without comes first, so that a line tmux will not read costs
// only what is under it.
func fragment(l Layout) string {
	if l.Command == "" {
		return settings + strip
	}
	return settings + strip + fmt.Sprintf(seenHook, quoteForOption(l.Command))
}

// quoteForOption writes a path into a tmux double-quoted string. What tmux
// reads there is the escape itself, the quote that would end the string, and
// the two characters that begin an expansion.
func quoteForOption(path string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`$`, `\$`,
		`#`, `\#`,
	).Replace(path)
}

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
	// Whatever is running this install is what tmux will run on a focus. A
	// harness that cannot say where it lives still installs the settings; it
	// just leaves the seen-tracking to the Dashboard's own jump.
	command, err := os.Executable()
	if err != nil {
		command = ""
	}

	return Layout{
		Fragment: filepath.Join(config.Home(home), "ganymede", "tmux.conf"),
		UserConf: userConf,
		Command:  command,
	}, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Install writes the fragment and makes UserConf source it.
func Install(l Layout) error {
	if err := config.Replace(l.Fragment, []byte(fragment(l))); err != nil {
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
