package tmuxconf_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
)

// layoutIn returns a Layout rooted in a throwaway directory.
func layoutIn(t *testing.T) tmuxconf.Layout {
	t.Helper()
	dir := t.TempDir()
	return tmuxconf.Layout{
		Fragment: filepath.Join(dir, "ganymede", "tmux.conf"),
		UserConf: filepath.Join(dir, "tmux.conf"),
	}
}

// tmuxWithConf starts a throwaway tmux server that loads conf and returns a
// function for querying it.
func tmuxWithConf(t *testing.T, conf string) func(args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-")
	run := func(args ...string) string {
		out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("-f", conf, "new-session", "-d", "-s", "probe", "sleep 300")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
	})
	return run
}

// writeUserConf seeds UserConf with settings the user already had.
func writeUserConf(t *testing.T, layout tmuxconf.Layout, body string) {
	t.Helper()
	if err := os.WriteFile(layout.UserConf, []byte(body), 0o644); err != nil {
		t.Fatalf("seed user conf: %v", err)
	}
}

func readUserConf(t *testing.T, layout tmuxconf.Layout) string {
	t.Helper()
	body, err := os.ReadFile(layout.UserConf)
	if err != nil {
		t.Fatalf("read user conf: %v", err)
	}
	return string(body)
}

// The point of the fragment: a tmux server reading the user's config ends up
// with passthrough and focus events on.
func TestInstalledConfigTurnsOnPassthroughAndFocusEvents(t *testing.T) {
	layout := layoutIn(t)

	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	tmux := tmuxWithConf(t, layout.UserConf)
	if got := tmux("show-options", "-A", "-p", "-t", "probe:0.0", "-v", "allow-passthrough"); got != "on" {
		t.Errorf("allow-passthrough = %q, want %q", got, "on")
	}
	if got := tmux("show-options", "-A", "-s", "-v", "focus-events"); got != "on" {
		t.Errorf("focus-events = %q, want %q", got, "on")
	}
}

// The user's tmux.conf stays theirs: the harness adds its block and leaves
// everything else alone.
func TestInstallKeepsTheUsersOwnSettings(t *testing.T) {
	layout := layoutIn(t)
	writeUserConf(t, layout, "set -g mouse on\nset -g history-limit 50000\n")

	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	tmux := tmuxWithConf(t, layout.UserConf)
	if got := tmux("show-options", "-A", "-g", "-v", "mouse"); got != "on" {
		t.Errorf("the user's mouse setting was lost: got %q", got)
	}
	if got := tmux("show-options", "-A", "-s", "-v", "focus-events"); got != "on" {
		t.Errorf("focus-events = %q, want %q", got, "on")
	}
}

// Installing is safe to repeat: the second run leaves the file exactly as the
// first one did.
func TestInstallingTwiceChangesNothingTheSecondTime(t *testing.T) {
	layout := layoutIn(t)
	writeUserConf(t, layout, "set -g mouse on\n")

	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	afterFirst := readUserConf(t, layout)

	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	if afterSecond := readUserConf(t, layout); afterSecond != afterFirst {
		t.Errorf("second install changed the file:\n--- after first ---\n%s\n--- after second ---\n%s", afterFirst, afterSecond)
	}
}

// Re-installing rewrites the block where it stands, so settings the user keeps
// after it survive and a moved fragment is picked up.
func TestReinstallRewritesTheBlockInPlace(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	writeUserConf(t, layout, readUserConf(t, layout)+"set -g mouse on\n")

	moved := layout
	moved.Fragment = filepath.Join(filepath.Dir(layout.UserConf), "elsewhere", "tmux.conf")
	if err := tmuxconf.Install(moved); err != nil {
		t.Fatalf("Install after move: %v", err)
	}

	conf := readUserConf(t, layout)
	if !strings.Contains(conf, moved.Fragment) {
		t.Errorf("conf does not point at the new fragment %s:\n%s", moved.Fragment, conf)
	}
	if strings.Contains(conf, layout.Fragment+"\"") {
		t.Errorf("conf still points at the old fragment:\n%s", conf)
	}
	if !strings.Contains(conf, "set -g mouse on") {
		t.Errorf("settings after the block were lost:\n%s", conf)
	}
}

// The harness must install into whichever tmux.conf the user actually loads.
func TestDefaultLayoutTargetsTheConfigTheUserAlreadyUses(t *testing.T) {
	t.Run("XDG location when the user keeps their config there", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		xdg := filepath.Join(home, ".config", "tmux", "tmux.conf")
		if err := os.MkdirAll(filepath.Dir(xdg), 0o755); err != nil {
			t.Fatal(err)
		}
		writeUserConf(t, tmuxconf.Layout{UserConf: xdg}, "set -g mouse on\n")

		layout, err := tmuxconf.DefaultLayout()
		if err != nil {
			t.Fatalf("DefaultLayout: %v", err)
		}
		if layout.UserConf != xdg {
			t.Errorf("UserConf = %q, want %q", layout.UserConf, xdg)
		}
	})

	t.Run("home dotfile otherwise", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")

		layout, err := tmuxconf.DefaultLayout()
		if err != nil {
			t.Fatalf("DefaultLayout: %v", err)
		}
		if want := filepath.Join(home, ".tmux.conf"); layout.UserConf != want {
			t.Errorf("UserConf = %q, want %q", layout.UserConf, want)
		}
		if want := filepath.Join(home, ".config", "ganymede", "tmux.conf"); layout.Fragment != want {
			t.Errorf("Fragment = %q, want %q", layout.Fragment, want)
		}
	})
}

// A block whose end marker has gone missing — hand-edited, mangled by an
// editor, mauled by a merge — must not cost the user the rest of their config.
func TestInstallKeepsSettingsAfterAnUnterminatedBlock(t *testing.T) {
	layout := layoutIn(t)
	writeUserConf(t, layout, "set -g mouse on\n"+
		"# >>> ganymede >>>\n"+
		"source-file -q \"/gone/tmux.conf\"\n"+
		"set -g history-limit 50000\n"+
		"bind r source-file ~/.tmux.conf\n")

	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	conf := readUserConf(t, layout)
	for _, want := range []string{"set -g mouse on", "set -g history-limit 50000", "bind r source-file"} {
		if !strings.Contains(conf, want) {
			t.Errorf("Install destroyed %q:\n%s", want, conf)
		}
	}

	// And the repair has to leave a well-formed block, or the next install
	// would add a second one.
	before := readUserConf(t, layout)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if after := readUserConf(t, layout); after != before {
		t.Errorf("installing over the repaired block changed it again:\n%s", after)
	}
}
