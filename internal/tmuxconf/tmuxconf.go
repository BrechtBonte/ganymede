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
set -g extended-keys on
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

// FindKey is Warp's one surviving muscle memory (§13), bound at Ghostty's own
// level: Cmd+F sends it directly. It is bound at the root table — the same
// table PopupToggleKey uses — rather than inside tmux's own prefix, because
// the harness never touches the user's own tmux prefix (it may not even be
// the stock C-b) and Ghostty's static keybind has no way to ask a session
// what its prefix is. Ctrl-] is unbound by tmux itself and by everything
// else the harness has to share a pane with, so claiming it globally is
// safe — the same trade PopupToggleKey already makes with Ctrl-backtick.
const FindKey = "C-]"

// findHook enters copy mode and opens the same incremental search-backward
// tmux's own emacs keytable already gives C-r, as one chained command rather
// than a keytable binding — which is what makes the result the same
// whether the session's mode-keys is emacs or vi, and needs no harness
// command of its own, so it is installed even when Command is empty.
const findHook = `
bind -n ` + FindKey + ` copy-mode \; command-prompt -i -I "#{pane_search_string}" -T search -p "(search up)" { send-keys -X search-backward-incremental -- "%%" }
`

// frozenHook is what tells the harness a pane has started or stopped holding a
// mode over its live view, which is what puts the Frozen mark on the rail and
// takes it off again.
//
// pane-mode-changed fires on entering and on leaving, and #{pane_in_mode} reads
// 0 on the leaving edge — so one command covers both directions, and the mark
// goes the moment you leave the mode rather than waiting on the half-minute
// cross-check to notice.
//
// Like seenHook it reads the harness's path out of @ganymede-seen rather than
// carrying a second copy, leaves #{pane_pid} a format so it names the pane the
// mode changed in rather than whichever pane this config was read from, and
// takes -b so tmux is free the moment the command has started. It is set
// rather than appended for the same reason too: re-sourcing this fragment onto
// a server that already had it leaves one hook rather than another copy.
//
// That makes it the second global tmux hook the harness owns, with the same
// consequence: a pane-mode-changed hook of your own would be replaced by it.
const frozenHook = `
set-hook -g pane-mode-changed 'run-shell -b "#{q:@ganymede-seen} frozen #{pane_pid} #{pane_in_mode}"'
`

// PopupToggleKey opens and closes the Popup shell (§8): one no-prefix key,
// bound at the root table so it reaches every pane on every Session. Ghostty's
// kitty keyboard protocol is what lets it transmit Ctrl+backtick distinctly
// from the NUL byte most terminals collapse it to — extended-keys, in
// settings above, is what tells tmux to make use of that when it is there.
const PopupToggleKey = "C-`"

// PopupToggleFallbackKey is bound to the same action as PopupToggleKey, for an
// emulator that cannot transmit Ctrl+backtick distinctly and would otherwise
// leave the Popup shell with no way to open at all.
const PopupToggleFallbackKey = "M-`"

// popupHook is the toggle's opening half. Closing is not this fragment's
// concern: it is bound on the popup's own hidden tmux server (see
// topology.Harness), which this fragment's server never reaches once a
// popup has taken the pane over.
//
// It reads the harness's own path out of @ganymede-seen rather than carrying
// a second copy of it: that option exists to answer exactly this question —
// where the binary these hooks run is — for the seen-tracking hook above,
// and a fragment naming its own harness twice is one hook install away from
// naming it two different ways.
//
// Both keys run the same command, carrying the three things "ganymede popup
// open" needs to decide where the popup belongs: the directory of the pane
// the key was pressed in, the Session that pane is part of (the rail asks for
// the Dashboard's own selection instead of its own directory — see the popup
// package), and the pane itself, which is where the overlay is drawn. All
// three are left as formats for run-shell to expand when the key is actually
// pressed, for the same reason #{pane_pid} is in seenHook: expanded now, they
// would name whichever pane this config happened to be loaded from, forever.
const popupHook = `
bind -n ` + PopupToggleKey + ` run-shell -b "#{q:@ganymede-seen} popup open #{q:pane_current_path} #{q:session_name} #{q:pane_id}"
bind -n ` + PopupToggleFallbackKey + ` run-shell -b "#{q:@ganymede-seen} popup open #{q:pane_current_path} #{q:session_name} #{q:pane_id}"
`

// PopupDirOption is where the Dashboard writes which directory its cursor is
// on, which is where a popup opens when the toggle is pressed with focus on
// the rail rather than a Session's own pane — the rail has no pane of its
// own to answer that question, so the harness asks the Dashboard instead.
const PopupDirOption = "@ganymede-popup-dir"

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
		return settings + strip + findHook
	}
	return settings + strip + findHook + fmt.Sprintf(seenHook, quoteForOption(l.Command)) + popupHook + frozenHook
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
	body := config.WithBlock(string(existing), []string{fmt.Sprintf("source-file -q %q", l.Fragment)})
	return config.Replace(l.UserConf, []byte(body))
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
