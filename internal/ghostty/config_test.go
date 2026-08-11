package ghostty_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/ghostty"
)

// ghosttyBinary locates the CLI Ghostty ships as part of its own app bundle
// on macOS, falling back to PATH — the same two places Emulator looks — so
// what the harness writes is checked by Ghostty's own parser rather than by
// re-implementing it here.
func ghosttyBinary(t *testing.T) string {
	t.Helper()
	const bundleBinary = "/Applications/Ghostty.app/Contents/MacOS/ghostty"
	if _, err := os.Stat(bundleBinary); err == nil {
		return bundleBinary
	}
	path, err := exec.LookPath("ghostty")
	if err != nil {
		t.Skip("Ghostty is not installed")
	}
	return path
}

// layoutIn returns a Layout rooted in a throwaway config directory, plus that
// directory so tests can point Ghostty's own CLI at it via XDG_CONFIG_HOME.
func layoutIn(t *testing.T) (ghostty.Layout, string) {
	t.Helper()
	home := t.TempDir()
	return ghostty.Layout{
		Fragment: filepath.Join(home, "ganymede", "ghostty.conf"),
		UserConf: filepath.Join(home, "ghostty", "config.ghostty"),
	}, home
}

// showConfig runs Ghostty's own +show-config action over home's config.
func showConfig(t *testing.T, home string) string {
	t.Helper()
	cmd := exec.Command(ghosttyBinary(t), "+show-config")
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ghostty +show-config: %v\n%s", err, out)
	}
	return string(out)
}

func writeUserConf(t *testing.T, layout ghostty.Layout, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(layout.UserConf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.UserConf, []byte(body), 0o644); err != nil {
		t.Fatalf("seed user conf: %v", err)
	}
}

func readUserConf(t *testing.T, layout ghostty.Layout) string {
	t.Helper()
	body, err := os.ReadFile(layout.UserConf)
	if err != nil {
		t.Fatalf("read user conf: %v", err)
	}
	return string(body)
}

// The point of the fragment: Ghostty ends up with the harness's fresh
// defaults rather than whatever it shipped with (§13).
func TestInstalledConfigSetsFontAndTheme(t *testing.T) {
	layout, home := layoutIn(t)

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	out := showConfig(t, home)
	if !strings.Contains(out, "font-family = "+ghostty.Font) {
		t.Errorf("config = %q, want font-family %q", out, ghostty.Font)
	}
	if !strings.Contains(out, "theme = "+ghostty.Theme) {
		t.Errorf("config = %q, want theme %q", out, ghostty.Theme)
	}
}

// On non-US keyboard layouts (e.g. Belgian AZERTY), Ghostty's default for
// Option leaves it as a macOS Unicode compose key rather than Alt, so
// Option+G types "©" instead of sending the M-g tmux's dock config binds at
// the root table (tmuxconf.FocusKey). The harness must force Option to
// behave as Alt regardless of layout.
func TestInstalledConfigTreatsOptionAsAlt(t *testing.T) {
	layout, home := layoutIn(t)

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	out := showConfig(t, home)
	if !strings.Contains(out, "macos-option-as-alt = true") {
		t.Errorf("config = %q, want macos-option-as-alt = true", out)
	}
}

// Warp's one surviving muscle memory (§13): Cmd+F must land in tmux copy-mode
// search rather than Ghostty's own scrollback search — its stock binding for
// the same key.
func TestInstalledConfigBindsCmdFToTmuxCopyModeSearch(t *testing.T) {
	layout, home := layoutIn(t)

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	out := showConfig(t, home)
	if !strings.Contains(out, "keybind = super+f=text:") {
		t.Errorf("config = %q, want super+f rebound off Ghostty's own search", out)
	}
	if strings.Contains(out, "keybind = super+f=start_search") {
		t.Errorf("config = %q, still has Ghostty's own scrollback search on Cmd+F", out)
	}
}

// Shift+⏎ is the newline gesture on the way in to Claude, and the terminal's
// stock encoding gives it none of its own: it arrives as the plain carriage
// return that submits the turn. The harness binds it to the escape and
// carriage return Option+⏎ already sends, which is what Claude Code reads as a
// newline. Ghostty prints a configured text: sequence back with its own
// backslashes doubled, so that is what the installed binding looks like here.
func TestInstalledConfigBindsShiftEnterToANewline(t *testing.T) {
	layout, home := layoutIn(t)

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	out := showConfig(t, home)
	if !strings.Contains(out, `keybind = shift+enter=text:\\x1b\\r`) {
		t.Errorf("config = %q, want shift+enter sending escape then carriage return", out)
	}
}

// The user's Ghostty config stays theirs: the harness adds its block and
// leaves everything else alone.
func TestInstallKeepsTheUsersOwnSettings(t *testing.T) {
	layout, home := layoutIn(t)
	writeUserConf(t, layout, "window-padding-x = 10\n")

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	out := showConfig(t, home)
	if !strings.Contains(out, "window-padding-x = 10") {
		t.Errorf("config = %q, the user's own setting was lost", out)
	}
	if !strings.Contains(out, "font-family = "+ghostty.Font) {
		t.Errorf("config = %q, want font-family %q", out, ghostty.Font)
	}
}

// Installing is safe to repeat: the second run leaves the file exactly as
// the first one did.
func TestInstallingTwiceChangesNothingTheSecondTime(t *testing.T) {
	layout, _ := layoutIn(t)
	writeUserConf(t, layout, "window-padding-x = 10\n")

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	afterFirst := readUserConf(t, layout)

	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	if afterSecond := readUserConf(t, layout); afterSecond != afterFirst {
		t.Errorf("second install changed the file:\n--- after first ---\n%s\n--- after second ---\n%s", afterFirst, afterSecond)
	}
}

// Re-installing rewrites the block where it stands, so a moved fragment is
// picked up and settings the user keeps after the block survive.
func TestReinstallRewritesTheBlockInPlace(t *testing.T) {
	layout, _ := layoutIn(t)
	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	writeUserConf(t, layout, readUserConf(t, layout)+"window-padding-x = 10\n")

	moved := layout
	moved.Fragment = filepath.Join(filepath.Dir(layout.Fragment), "elsewhere", "ghostty.conf")
	if err := ghostty.Install(moved); err != nil {
		t.Fatalf("Install after move: %v", err)
	}

	conf := readUserConf(t, layout)
	if !strings.Contains(conf, moved.Fragment) {
		t.Errorf("conf does not point at the new fragment %s:\n%s", moved.Fragment, conf)
	}
	if strings.Contains(conf, layout.Fragment+"\"") {
		t.Errorf("conf still points at the old fragment:\n%s", conf)
	}
	if !strings.Contains(conf, "window-padding-x = 10") {
		t.Errorf("settings after the block were lost:\n%s", conf)
	}
}

// The harness must target the config file Ghostty documents as its default,
// under whichever XDG config directory the user has set.
func TestDefaultLayoutTargetsGhosttysDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	layout, err := ghostty.DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout: %v", err)
	}
	if want := filepath.Join(home, ".config", "ghostty", "config.ghostty"); layout.UserConf != want {
		t.Errorf("UserConf = %q, want %q", layout.UserConf, want)
	}
	if want := filepath.Join(home, ".config", "ganymede", "ghostty.conf"); layout.Fragment != want {
		t.Errorf("Fragment = %q, want %q", layout.Fragment, want)
	}
}

// Belt and braces alongside the +show-config checks above: the installed
// config must actually pass Ghostty's own validator.
func TestInstalledConfigValidates(t *testing.T) {
	layout, home := layoutIn(t)
	if err := ghostty.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	cmd := exec.Command(ghosttyBinary(t), "+validate-config")
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("ghostty +validate-config: %v\n%s", err, out)
	}
}
