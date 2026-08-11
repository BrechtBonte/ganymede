package ghostty

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// Font and Theme are Ghostty's fresh defaults, picked once at install time
// rather than carried from Warp — which runs stock, so there is nothing of
// its own to port (§13).
const (
	Font  = "JetBrains Mono"
	Theme = "Builtin Dark"
)

// FindKeybind rebinds Ghostty's own Cmd+F — stock bound to start_search, its
// own scrollback search — onto tmux copy-mode search instead, since tmux
// owns the scrollback (§13). It sends Ctrl-] (\x1d) rather than the
// session's own prefix followed by a copy-mode key: the harness never
// touches the user's tmux prefix, so a static Ghostty keybind has no way to
// know what it is. tmuxconf's fragment instead binds Ctrl-] directly at
// tmux's root table (see tmuxconf.FindKey), the same way it binds the Popup
// shell's own toggle, so no prefix is involved at all.
const FindKeybind = `keybind = super+f=text:\x1d`

// NewlineKeybind gives Shift+⏎ the newline it has nowhere else: an escape
// followed by a carriage return, which is what Option+⏎ already sends through
// macos-option-as-alt below and what Claude Code reads as "another line, not
// yet my turn". Stock, Shift+⏎ has no sequence of its own — the terminal
// sends the same bare carriage return ⏎ does, and the half-written prompt goes
// off to the agent.
//
// It is bound at Ghostty's own level, like FindKeybind, because nothing under
// it can tell the two apart: what the dock, the Session's tmux and Claude
// itself see are the bytes, and after this they are Option+⏎'s bytes. Which is
// also the trade — every application in the window now reads Shift+⏎ as
// Option+⏎, including the Dashboard, where the prompt box takes Option+⏎ on a
// Working Session as interrupt-then-send.
const NewlineKeybind = `keybind = shift+enter=text:\x1b\r`

// OptionAsAlt forces Option to behave as Alt rather than macOS's own Unicode
// compose key. Ghostty's default for this varies by keyboard layout — true
// only for U.S. Standard/International — so on other layouts (e.g. Belgian
// AZERTY) Option+G types "©" instead of sending the M-g tmux's dock config
// binds at the root table (tmuxconf.FocusKey).
const OptionAsAlt = "macos-option-as-alt = true"

// Layout locates the files Install touches.
type Layout struct {
	// Fragment is the harness-owned config file holding the settings.
	Fragment string
	// UserConf is Ghostty's own config file, which Install makes load
	// Fragment.
	UserConf string
}

// fragment is the harness's Ghostty configuration for every Layout: nothing
// here varies by install, so unlike tmuxconf's it needs no per-install
// values spliced in.
const fragment = "# Managed by ganymede. Edit ganymede, not this file.\n" +
	`font-family = "` + Font + "\"\n" +
	`theme = "` + Theme + "\"\n" +
	OptionAsAlt + "\n" +
	FindKeybind + "\n" +
	NewlineKeybind + "\n"

// DefaultLayout resolves the standard locations: the fragment under the
// harness's own config directory, and the config file Ghostty documents as
// its default — under whichever XDG config directory the user has set.
func DefaultLayout() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Layout{
		Fragment: filepath.Join(config.Home(home), "ganymede", "ghostty.conf"),
		UserConf: filepath.Join(config.Home(home), "ghostty", "config.ghostty"),
	}, nil
}

// Install writes the fragment and makes UserConf load it.
func Install(l Layout) error {
	if err := config.Replace(l.Fragment, []byte(fragment)); err != nil {
		return fmt.Errorf("write fragment: %w", err)
	}
	existing, err := os.ReadFile(l.UserConf)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", l.UserConf, err)
	}
	body := config.WithBlock(string(existing), []string{fmt.Sprintf("config-file = %q", l.Fragment)})
	return config.Replace(l.UserConf, []byte(body))
}
