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

	"github.com/charmbracelet/lipgloss"

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

// The validated mock's palette, as the two status lines the harness dresses
// read it: the Dock's own and the working client's. Literals of their own
// rather than anything derived from a Session's state colour — the chrome is
// not a state, and has to be free to move without dragging one with it, which
// is how the Dashboard already keeps its brand and its ticket colour apart.
const (
	chromePanel      = "#161b22"
	chromeForeground = "#c9d1d9"
	chromeQuiet      = "#8b949e"
	chromeFaint      = "#484f58"
	chromeBrand      = "#58a6ff"
)

// signature is what the working client's status line signs itself, so that a
// harness window is tellable from a plain terminal at a glance.
const signature = "ganymede"

// strip hands the right-hand end of the status line to the harness: the counts
// of what is waiting on you, under your eye line in the Session you are working
// in rather than only over in the sidepanel, and the harness's own signature
// after them.
//
// The Dashboard writes the whole strip — marks, counts and colours — into an
// option of its own, so a status line redrawn on every keystroke costs tmux
// nothing but a lookup, and a server whose Dashboard has never written to it
// draws the signature alone rather than an error. Setting the option is enough
// to put it on screen: tmux redraws its clients when an option changes.
//
// The separator is inside the conditional rather than beside it, so a quiet
// working set leaves no punctuation behind: tmux reads an unset or empty option
// as false, which is exactly the case the Dashboard writes for nothing waiting.
// The conditional is tmux's own, evaluated where the line is drawn, so this
// stays one static setting rather than something the Dashboard has to rewrite.
// The styling is here for the same reason the strip is: stock tmux draws this
// line in green, which would be the one loud thing in an otherwise dark Dock.
// The current window is given its own weight, since the green it used to be
// told apart by has gone.
//
// The harness owns these settings. A status line the user had turned off is
// turned back on, because a strip nobody can see is not a strip, and a
// right-hand segment of the user's own is replaced by this one.
const strip = `
set -g status on
set -g status-style "bg=` + chromePanel + `,fg=` + chromeQuiet + `"
set -g window-status-current-style "fg=` + chromeForeground + `,bold"
set -g status-right-length 100
set -g status-right "#{?` + AttentionOption + `,#{` + AttentionOption + `} #[fg=` + chromeFaint + `]· ,}#[fg=` + chromeBrand + `]` + signature + `#[default]"
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

// dockBody configures the dock server. The dock is mostly a frame: it holds
// the sidepanel and the working client side by side and otherwise stays out of
// the way, so its prefix is disabled and every key reaches the client inside
// the pane. It needs passthrough and focus events of its own, because both have
// to travel through the dock to reach the emulator and the Sessions behind it.
// The one thing it says for itself is the key legend along its own status line
// — the only full-width row in the Dock.
const dockBody = `# Managed by ganymede. Edit ganymede, not this file.
set -g prefix None
set -g prefix2 None
set -g mouse off
set -g escape-time 0
set -g base-index 0
setw -g pane-base-index 0
set -g allow-passthrough on
set -g focus-events on

# %s moves between the sidepanel and the working client.
bind -n %s select-pane -t :.+

# The Dock is the frame holding both panes, which makes its own status line the
# only full-width row there is — so it is where the key legend goes, along the
# bottom of the whole Dock. status-format is set rather than status-left, so the
# line is the legend and nothing else: no window list to share it with, and none
# of status-left's ten-column budget to be cut down to.
set -g status on
set -g status-style "bg=%s,fg=%s"
set -g status-format[0] "%s"

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

// legendKeys is the harness's complete vocabulary, in the order the keys are
// worth: a Dock too narrow for all of it gives up the tail (see legend), so
// what is worth least is what goes first.
//
// Moving about comes first, then the two chords nothing else advertises — the
// SELECTED box offers a row's own keys as you land on it, but no row is ever
// standing on the key that moves focus or opens the Popup shell. The rest is
// §7.3's action set: what a repo header answers to, then a Session, then the
// ticket, then the picker.
//
// The chords are built from the constants the keys are actually bound to, so
// that a rebinding cannot leave the legend lying, and are written the way a Mac
// user presses them rather than in tmux's own notation.
//
// This is deliberately the complete vocabulary rather than what would fire on
// the row you are standing on — which is a legend listing keys that do nothing
// here, and cuts against the rule the SELECTED box follows ("offering a key
// that would silently do nothing is worse than not offering it"). The division
// of labour is the point: the legend is for learning the harness, the box
// remains the authority on what this row will actually do.
//
// Where a key's label changes with what it is over, the legend says both:
// "c claim/takeover" is one key over a Free root and over one somebody else is
// in. What it must never say is what the prototype's shared bar said — "!" for the
// Popup shell, "x takeover" when x is interrupt, or "q quit" when q ends a
// Session and the Dashboard answers to no quit key at all.
var legendKeys = []string{
	"↑↓ select",
	"⏎ jump",
	macChord(FocusKey) + " focus",
	macChord(PopupToggleKey) + " popup shell",
	"w spawn",
	"c claim/takeover",
	"p prompt",
	"y approve",
	"n deny",
	"x interrupt",
	"q end",
	"t ticket",
	"o open ticket",
	"g repo picker",
}

// macChord writes a tmux key the way it is pressed on the keyboard in front of
// you: tmux's M- is the Option key and its C- is Control, and a legend that
// asked for "M-g" would be one more thing to translate rather than one less.
func macChord(key string) string {
	return strings.NewReplacer("M-", "⌥", "C-", "⌃").Replace(key)
}

// legendSeparator divides one key from the next, fainter than either, so what
// the eye runs along is the keys.
const legendSeparator = "#[fg=" + chromeFaint + "] · "

// legend draws the vocabulary for the Dock's status line: the key itself in the
// foreground and its label quiet behind it, which is how the SELECTED box
// already draws the keys it offers — the two are one vocabulary and should read
// as one.
//
// Everything past the first key is wrapped in a conditional on the width of the
// client the line is being drawn for, so a Dock too narrow for the whole legend
// drops whole keys off the tail. That is fitKeys' greedy fit, made by tmux at
// draw time instead of by us at write time — and it has to be, because the
// window this config is written for can be any width and can be resized after.
// Left to tmux's own truncation the line would be cut wherever the last column
// happened to fall: "x interrup", or a separator with nothing after it, which
// reads as a Dock that has glitched rather than one that ran out of room.
//
// The width each key needs is the whole line up to and including it, measured
// the way the Dashboard measures its own keys. Both the separator and the key
// are inside the conditional, so a key that does not fit takes its separator
// with it.
func legend() string {
	var format, line string
	for _, key := range legendKeys {
		entry, plain := hinted(key), key
		if line != "" {
			entry, plain = legendSeparator+entry, " · "+plain
		}
		line += plain
		if format == "" {
			// The first key is drawn whatever the width: a legend that could
			// vanish altogether would leave the Dock's own row saying nothing.
			format = entry
			continue
		}
		format += fmt.Sprintf("#{?#{e|>=:#{client_width},%d},%s,}", lipgloss.Width(line), entry)
	}
	return format
}

// hinted draws one key: the character in the panel's own foreground and the
// label quiet behind it. A label carrying a comma would end tmux's conditional
// early, which is why they are written with slashes.
func hinted(key string) string {
	char, label, ok := strings.Cut(key, " ")
	if !ok {
		return "#[fg=" + chromeForeground + "]" + char
	}
	return "#[fg=" + chromeForeground + "]" + char + "#[fg=" + chromeQuiet + "] " + label
}

// WriteDockConf writes the dock server's configuration.
func WriteDockConf(path string, sidepanelWidth int) error {
	body := fmt.Sprintf(dockBody, FocusKey, FocusKey,
		chromePanel, chromeQuiet, legend(),
		sidepanelWidth, sidepanelWidth, sidepanelWidth)
	if err := config.Replace(path, []byte(body)); err != nil {
		return fmt.Errorf("write dock config: %w", err)
	}
	return nil
}
