package tmuxconf_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// hostile is a directory name holding every character that means something to
// one of the two readers the harness's path has to survive — tmux's own
// parser, and the shell tmux hands the command to. A harness built somewhere
// like this is still a harness, and a path it cannot write down would not just
// cost the seen-tracking: the fragment is one file, and a line tmux refuses to
// read takes the settings under it with it.
const hostile = `o'brien $x #y "z ` + "`w"

// recorder is a stand-in for the ganymede binary that writes down every
// argument it was run with.
func recorder(t *testing.T) (command, record string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), hostile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make a hostile directory: %v", err)
	}
	command = filepath.Join(dir, "gan ymede")
	record = filepath.Join(dir, "seen.txt")
	body := "#!/bin/sh\nprintf '%s ' \"$@\" >> \"" + record + "\"\n"
	if err := os.WriteFile(command, []byte(body), 0o755); err != nil {
		t.Fatalf("write the recorder: %v", err)
	}
	return command, record
}

// installedWithRecorder installs a fragment whose harness is the recorder.
func installedWithRecorder(t *testing.T) (layout tmuxconf.Layout, record string) {
	t.Helper()
	layout = layoutIn(t)
	layout.Command, record = recorder(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	return layout, record
}

// Seen-tracking rests on tmux telling the harness when a pane is looked at.
// The pid has to still be a format after the config is read — expanded at load
// it would name one pane forever, and the wrong one.
func TestTheInstalledConfigAsksTmuxToReportFocus(t *testing.T) {
	layout, _ := installedWithRecorder(t)

	tmux := tmuxWithConf(t, layout.UserConf)
	hook := tmux("show-hooks", "-g", "pane-focus-in")

	if !strings.Contains(hook, "seen") {
		t.Errorf("focus on a pane does not run the harness: %q", hook)
	}
	// The settings the harness cannot work without are read whatever the
	// harness is called: a path tmux chokes on must not cost them.
	if got := tmux("show-options", "-A", "-s", "-v", "focus-events"); got != "on" {
		t.Errorf("focus-events = %q, want %q", got, "on")
	}
	if !strings.Contains(hook, "#{pane_pid}") {
		t.Errorf("the pane was decided when the config was read, not when the focus lands: %q", hook)
	}
}

// The status line of the Session you are working in is where the ambient
// attention strip goes, so the installed config has to keep that line and hand
// its right-hand end to the harness.
func TestTheInstalledConfigKeepsTheStatusLineForTheAttentionStrip(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	tmux := tmuxWithConf(t, layout.UserConf)

	if got := tmux("show-options", "-A", "-g", "-v", "status"); got != "on" {
		t.Errorf("status = %q, want the line the strip is drawn on", got)
	}
	if got := tmux("show-options", "-A", "-g", "-v", "status-right"); !strings.Contains(got, tmuxconf.AttentionOption) {
		t.Errorf("status-right = %q, want the harness's %s in it", got, tmuxconf.AttentionOption)
	}
}

// tmux places the strip and the Dashboard writes it, so a server the Dashboard
// has never spoken to draws nothing rather than an error, and one it has draws
// the counts as they were written.
func TestTheStripShowsWhatTheDashboardHasWrittenAndNothingBefore(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)

	if got := tmux("display-message", "-p", "#{E:status-right}"); strings.TrimSpace(got) != "" {
		t.Errorf("a Dashboard that has said nothing leaves %q on the status line", got)
	}

	tmux("set", "-g", tmuxconf.AttentionOption, "█ 2 blocked")

	if got := tmux("display-message", "-p", "#{E:status-right}"); !strings.Contains(got, "█ 2 blocked") {
		t.Errorf("status line shows %q, want what the Dashboard wrote", got)
	}
}

// The whole path, through real tmux: a client attaches to a Session's pane,
// tmux reports the focus, and the harness is run for that pane's own process —
// which is what the Dashboard turns back into Sessions you have now seen.
func TestFocusLandingOnAPaneRunsTheHarnessForIt(t *testing.T) {
	layout, record := installedWithRecorder(t)
	tmux := tmuxWithConf(t, layout.UserConf)
	pane := tmux("display-message", "-p", "-t", "=probe", "#{pane_pid}")

	// A client with a pty of its own, the way the dock attaches to a Session.
	// Only an attached client has a focus to land anywhere.
	attach(t, sessionsSocket(t))

	if !settled(func() bool { return strings.Contains(read(t, record), pane) }) {
		t.Errorf("the harness was run with %q, want the focused pane's process %s", read(t, record), pane)
	}
}

// sessionsSocket is the socket tmuxWithConf started its server on.
func sessionsSocket(t *testing.T) string {
	t.Helper()
	return "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-")
}

// attach opens a client onto the probe session from a tmux server of its own,
// which stands in for the emulator holding the dock.
func attach(t *testing.T, socket string) {
	t.Helper()
	emulator := socket + "-emulator"
	out, err := exec.Command("tmux", "-L", emulator, "new-session", "-d", "sh", "-c",
		"env -u TMUX tmux -L "+socket+" attach -t =probe").CombinedOutput()
	if err != nil {
		t.Fatalf("attach a client: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", emulator, "kill-server").Run() })
}

// read is what the recorder has written down so far.
func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(body)
}

// settled polls until cond holds, so the test does not race tmux's client
// setup and the shell it starts.
func settled(cond func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
