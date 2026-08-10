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
	FindKeybind + "\n"

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
