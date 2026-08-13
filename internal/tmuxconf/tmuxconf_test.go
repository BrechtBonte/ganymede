package tmuxconf_test

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// record is written inside the recorder script's own double quotes, so it
	// needs the same four characters escaped that any double-quoted shell
	// string does — and hostile carries three of them, including a lone
	// backtick that would otherwise open a command substitution the script
	// never closes.
	body := "#!/bin/sh\nprintf '%s ' \"$@\" >> \"" + dquote(record) + "\"\n"
	if err := os.WriteFile(command, []byte(body), 0o755); err != nil {
		t.Fatalf("write the recorder: %v", err)
	}
	return command, record
}

// dquote escapes s for use inside a double-quoted POSIX shell string: a
// backslash before each of the characters double quotes still leave special.
func dquote(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return replacer.Replace(s)
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

// extended-keys is what lets tmux tell Ctrl+backtick apart from the NUL byte
// most terminals collapse it to — without it, the Popup shell's primary
// toggle would be indistinguishable from Ctrl+Space on any terminal that
// bothered to send the distinction at all.
func TestInstalledConfigTurnsOnExtendedKeys(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	tmux := tmuxWithConf(t, layout.UserConf)
	if got := tmux("show-options", "-A", "-s", "-v", "extended-keys"); got != "on" {
		t.Errorf("extended-keys = %q, want %q", got, "on")
	}
}

// The Popup shell's toggle has no prefix of its own to hide behind (§8), so
// both the primary key and the Alt-chord fallback have to be bound at the
// root table — reachable from every pane on every Session, the same way the
// seen-tracking hook above is installed for all of them at once.
func TestInstalledConfigBindsThePopupToggleKeysAtTheRootTable(t *testing.T) {
	layout, _ := installedWithRecorder(t)
	tmux := tmuxWithConf(t, layout.UserConf)

	bound := tmux("list-keys", "-T", "root")
	for _, key := range []string{tmuxconf.PopupToggleKey, tmuxconf.PopupToggleFallbackKey} {
		if !strings.Contains(bound, key) {
			t.Errorf("root table = %q, want %q bound in it", bound, key)
		}
	}
	if strings.Count(bound, "popup open") != 2 {
		t.Errorf("root table = %q, want both keys opening the popup", bound)
	}
}

// A harness that cannot say where it lives installs no hook at all (see
// seenHook) — and the popup toggle is no exception: a binding that ran a
// command nobody could name would leave every press of it doing nothing
// silently, which is worse than the key not being bound.
func TestNoCommandBindsNoPopupToggle(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	tmux := tmuxWithConf(t, layout.UserConf)
	if bound := tmux("list-keys", "-T", "root"); strings.Contains(bound, "popup open") {
		t.Errorf("root table = %q, want no popup binding with no harness command to run", bound)
	}
}

// The Frozen mark rests on tmux telling the harness when a pane starts or
// stops holding a mode over its live view. Like the focus hook, the pid has
// to still be a format after the config is read: expanded at load it would
// name one pane forever, and the wrong one.
func TestTheInstalledConfigAsksTmuxToReportModeChanges(t *testing.T) {
	layout, _ := installedWithRecorder(t)

	tmux := tmuxWithConf(t, layout.UserConf)
	hook := tmux("show-hooks", "-g", "pane-mode-changed")

	if !strings.Contains(hook, "frozen") {
		t.Errorf("a pane changing mode does not run the harness: %q", hook)
	}
	if !strings.Contains(hook, "#{pane_pid}") {
		t.Errorf("the pane was decided when the config was read, not when the mode changed: %q", hook)
	}
	if !strings.Contains(hook, "#{pane_in_mode}") {
		t.Errorf("the hook does not say which way the mode changed: %q", hook)
	}
}

// A Layout that cannot say where the harness lives installs no hook that runs
// it — the same trade the seen hook and the popup toggle already make.
func TestNoCommandReportsNoModeChanges(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// An unset hook is reported as its bare name, not as nothing at all, so
	// what says it was never installed is that it runs no harness.
	tmux := tmuxWithConf(t, layout.UserConf)
	if hook := tmux("show-hooks", "-g", "pane-mode-changed"); strings.Contains(hook, "frozen") {
		t.Errorf("pane-mode-changed = %q, want no harness to run with no command to run it", hook)
	}
}

// Both edges, for real. pane-mode-changed fires on entering and on leaving,
// and pane_in_mode reads 0 on the leaving one — which is what lets a single
// hook both raise the Frozen mark and take it down again.
func TestAPaneEnteringAndLeavingAModeRunsTheHarnessForIt(t *testing.T) {
	layout, record := installedWithRecorder(t)
	tmux := tmuxWithConf(t, layout.UserConf)
	pane := tmux("display-message", "-p", "-t", "=probe:0.0", "#{pane_pid}")

	tmux("copy-mode", "-t", "=probe:0.0")
	if !settled(func() bool { return strings.Contains(read(t, record), "frozen "+pane+" 1") }) {
		t.Errorf("entering a mode recorded %q, want the pane %s reported frozen", read(t, record), pane)
	}

	tmux("send-keys", "-X", "-t", "=probe:0.0", "cancel")
	if !settled(func() bool { return strings.Contains(read(t, record), "frozen "+pane+" 0") }) {
		t.Errorf("leaving a mode recorded %q, want the pane %s reported thawed", read(t, record), pane)
	}
}

// Warp's one surviving muscle memory (§13): Ghostty's Cmd+F sends FindKey
// directly, so it has to be bound at the root table — not behind whatever
// prefix the session happens to have, which the harness never touches and a
// static Ghostty keybind has no way to know.
func TestInstalledConfigBindsFindKeyAtTheRootTable(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)

	bound := tmux("list-keys", "-T", "root")
	if !strings.Contains(bound, tmuxconf.FindKey) {
		t.Errorf("root table = %q, want %q bound in it", bound, tmuxconf.FindKey)
	}
	if !strings.Contains(bound, "copy-mode") || !strings.Contains(bound, "search-backward") {
		t.Errorf("root table = %q, want it to enter copy mode and search backward", bound)
	}
}

// FindKey has to be bound even when the harness cannot say where it lives:
// unlike the seen and popup hooks, it needs no command of its own to run.
func TestFindKeyIsBoundWithNoCommand(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)

	if bound := tmux("list-keys", "-T", "root"); !strings.Contains(bound, tmuxconf.FindKey) {
		t.Errorf("root table = %q, want %q bound in it even with no harness command", bound, tmuxconf.FindKey)
	}
}

// The whole path, through real tmux: FindKey has to reach the session
// whatever the session's own tmux prefix is (the harness never touches it),
// which a root-table binding guarantees and a prefixed one could not.
func TestPressingFindKeyEntersCopyMode(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)
	socket := sessionsSocket(t)

	attach(t, socket)

	if !settled(func() bool {
		pressKey(t, socket, tmuxconf.FindKey)
		return tmux("display-message", "-p", "-t", "=probe:0.0", "#{pane_in_mode}") == "1"
	}) {
		t.Fatalf("pane_in_mode = %q, want copy mode entered after pressing %s", tmux("display-message", "-p", "-t", "=probe:0.0", "#{pane_in_mode}"), tmuxconf.FindKey)
	}
}

// The whole path, through real tmux: the Alt-chord fallback is what a plain
// terminal can transmit distinctly, so it stands in here for "the toggle was
// pressed" — proving the root-table binding actually reaches the harness with
// the pressed pane's own context, which is what decides where the popup
// opens. Ctrl+backtick binds the same command (proven statically above) but
// is not exercised live: disambiguating it from the NUL every terminal
// collapses it to needs a kitty-protocol client, which send-keys is not.
func TestPopupToggleFallbackRunsPopupOpen(t *testing.T) {
	layout, record := installedWithRecorder(t)
	tmux := tmuxWithConf(t, layout.UserConf)
	pane := tmux("display-message", "-p", "-t", "=probe:0.0", "#{pane_id}")

	attach(t, sessionsSocket(t))

	// Pressed inside the poll rather than once before it: the emulator's
	// nested client can take a moment past attach returning before it is
	// actually reading its pty, and a key sent into that gap is a key the
	// client was never there to receive.
	if !settled(func() bool {
		pressKey(t, sessionsSocket(t), tmuxconf.PopupToggleFallbackKey)
		return strings.Contains(read(t, record), "popup open")
	}) {
		t.Fatalf("recorder got %q, want a popup open invocation", read(t, record))
	}
	got := read(t, record)
	for _, want := range []string{"popup open", pane, "probe"} {
		if !strings.Contains(got, want) {
			t.Errorf("recorder got %q, want it to contain %q", got, want)
		}
	}
}

// pressKey sends key into the client attach opened on socket, the way a real
// keypress reaches it: written into the emulator's own pane rather than
// asked of the Sessions server directly, since send-keys writes straight to
// a pane's input and never goes through a key table at all — only an
// attached client's own input loop does that, and attach's nested tmux
// process is standing in for one.
func pressKey(t *testing.T, socket, key string) {
	t.Helper()
	emulator := socket + "-emulator"
	out, err := exec.Command("tmux", "-L", emulator, "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		t.Fatalf("list the emulator's session: %v", err)
	}
	target := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if out, err := exec.Command("tmux", "-L", emulator, "send-keys", "-t", target, key).CombinedOutput(); err != nil {
		t.Fatalf("send-keys %s: %v\n%s", key, err, out)
	}
}

// dockConf writes the Dock server's configuration into a throwaway directory
// and returns the path tmux is to read it from.
func dockConf(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dock.conf")
	if err := tmuxconf.WriteDockConf(path, 40); err != nil {
		t.Fatalf("WriteDockConf: %v", err)
	}
	return path
}

// plain is a status line read the way the eye reads it: tmux's own style
// directives taken back out, so a test asserts on what is drawn rather than on
// the colours it is drawn in.
func plain(line string) string {
	for {
		open := strings.Index(line, "#[")
		if open < 0 {
			return line
		}
		end := strings.Index(line[open:], "]")
		if end < 0 {
			return line
		}
		line = line[:open] + line[open+end+1:]
	}
}

// macChord is the notation a legend has to be written in, restated here rather
// than shared with the harness: a test that called the harness's own translation
// would agree with it however wrong both were.
func macChord(key string) string {
	return strings.NewReplacer("M-", "⌥", "C-", "⌃").Replace(key)
}

// attachedAt opens a client this many columns wide onto the probe session, from
// a tmux server of its own the way the emulator holds the dock, and hands back
// what that client's status line — the last row of its screen — reads at any
// moment. Reading the drawn line is the only way to test a fit: the option
// holds the format, and the fit is what tmux makes of the format at that width.
func attachedAt(t *testing.T, socket string, columns int) func() string {
	t.Helper()
	// Named short rather than after the test: a unix socket's path runs out
	// well before a Go test name does.
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(socket))
	emulator := fmt.Sprintf("gan-em-%08x-%d", sum.Sum32(), columns)
	out, err := exec.Command("tmux", "-L", emulator, "new-session", "-d",
		"-x", strconv.Itoa(columns), "-y", "12", "sh", "-c",
		"env -u TMUX tmux -L "+socket+" attach -t =probe").CombinedOutput()
	if err != nil {
		t.Fatalf("attach a %d-column client: %v\n%s", columns, err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", emulator, "kill-server").Run() })

	return func() string {
		shown, err := exec.Command("tmux", "-L", emulator, "capture-pane", "-p", "-t", ":0.0").Output()
		if err != nil {
			return ""
		}
		lines := strings.Split(strings.TrimRight(string(shown), "\n"), "\n")
		return strings.TrimRight(lines[len(lines)-1], " ")
	}
}

// drawnAt is the status line a client this many columns wide settles on.
func drawnAt(t *testing.T, socket string, columns int) string {
	t.Helper()
	showing := attachedAt(t, socket, columns)
	var last string
	if !settled(func() bool {
		last = showing()
		return strings.TrimSpace(last) != ""
	}) {
		t.Fatalf("a %d-column client never drew a status line", columns)
	}
	return last
}

// dockLegend is what the Dock's status line draws for a client wide enough for
// the whole of it.
func dockLegend(t *testing.T) string {
	t.Helper()
	tmux := tmuxWithConf(t, dockConf(t))
	if got := tmux("show-options", "-A", "-g", "-v", "status"); got != "on" {
		t.Fatalf("the Dock's status = %q, want the line the legend is drawn on", got)
	}
	return drawnAt(t, sessionsSocket(t), 240)
}

// The Dock's own status line is the one full-width row in the Dock, so it is
// where the legend goes: the complete vocabulary, for learning, beside the
// SELECTED box's applicable subset for the row you are standing on.
func TestTheDockStatusLineCarriesTheKeyLegend(t *testing.T) {
	line := dockLegend(t)

	for _, want := range []string{"↑↓ select", "⏎ jump", "w spawn", "t ticket", "o open ticket", "g repo picker"} {
		if !strings.Contains(line, want) {
			t.Errorf("the Dock's legend reads %q, want %q offered in it", line, want)
		}
	}
}

// A key the Dashboard labels differently as the row under it changes is a key
// the legend has to spell out in full: c gives a Claimed root back rather than
// claiming it. Half the meanings would be the SELECTED box's own words used to
// mean something else.
func TestTheDockLegendSpellsOutTheKeysThatChangeWithTheRow(t *testing.T) {
	line := dockLegend(t)

	for _, key := range []struct{ key, meaning string }{
		{"c", "claim"}, {"c", "release"}, {"c", "takeover"},
	} {
		if !strings.Contains(line, key.meaning) {
			t.Errorf("the legend reads %q, want %q said of %q", line, key.meaning, key.key)
		}
	}
}

// A Dock too narrow for the whole legend gives up whole keys off the tail. Cut
// wherever the last column happened to fall, the line would end in half a word
// or in a separator with nothing after it — which reads as a Dock that has
// glitched rather than one that ran out of room, and is the very thing the
// SELECTED box's own fit exists to avoid.
func TestANarrowDockDropsWholeKeysRatherThanCuttingOne(t *testing.T) {
	tmuxWithConf(t, dockConf(t))
	socket := sessionsSocket(t)
	whole := strings.Split(strings.TrimSpace(drawnAt(t, socket, 240)), " · ")

	for _, columns := range []int{60, 100, 140} {
		line := drawnAt(t, socket, columns)
		if width := len([]rune(strings.TrimRight(line, " "))); width > columns {
			t.Errorf("a %d-column Dock draws %d columns of legend: %q", columns, width, line)
		}
		offered := strings.Split(strings.TrimSpace(line), " · ")
		if len(offered) > len(whole) {
			t.Fatalf("a %d-column Dock draws more keys than there are: %q", columns, line)
		}
		for i, key := range offered {
			if key != whole[i] {
				t.Errorf("a %d-column Dock draws %q, want whole keys off the front of %q — %q is not %q", columns, line, whole, key, whole[i])
			}
		}
	}
}

// The legend is ordered so that the keys worth most survive a narrow window,
// which tmux truncates a status line from the right to fit.
func TestTheDockLegendLeadsWithTheKeysWorthMost(t *testing.T) {
	line := dockLegend(t)

	for _, pair := range [][2]string{
		{"↑↓ select", "⏎ jump"},
		{"⏎ jump", macChord(tmuxconf.FocusKey)},
		{macChord(tmuxconf.FocusKey), "w spawn"},
		{"w spawn", "o open ticket"},
	} {
		before, after := strings.Index(line, pair[0]), strings.Index(line, pair[1])
		if before < 0 || after < 0 || before > after {
			t.Errorf("the legend reads %q, want %q before %q — the tail is what a narrow window loses", line, pair[0], pair[1])
		}
	}
}

// The legend's chords are the keys the harness actually binds, written as a Mac
// user presses them. Built from the constants rather than from copies: a legend
// with its own spelling of M-g would go on offering it after a rebinding.
func TestTheDockLegendNamesTheChordsTheKeysAreBoundTo(t *testing.T) {
	line := dockLegend(t)

	for _, key := range []string{tmuxconf.FocusKey, tmuxconf.PopupToggleKey} {
		if chord := macChord(key); !strings.Contains(line, chord) {
			t.Errorf("the legend reads %q, want the chord %q for %s in it", line, chord, key)
		}
		if strings.Contains(line, key) {
			t.Errorf("the legend reads %q, want %s written as it is pressed rather than in tmux's notation", line, key)
		}
	}
}

// The Popup shell answers to two keys and the legend names one: the primary,
// which is §8's own and which the Dock now carries — see the Dock's extended
// keys, without which this entry was a promise the harness could not keep.
// The fallback stays bound for an emulator that cannot transmit the primary,
// and stays off the legend, which is a list of gestures rather than of
// bindings.
func TestTheDockLegendNamesOneChordForThePopupShell(t *testing.T) {
	line := dockLegend(t)

	if want := macChord(tmuxconf.PopupToggleKey) + " popup shell"; !strings.Contains(line, want) {
		t.Errorf("the legend reads %q, want %q in it", line, want)
	}
	if spare := macChord(tmuxconf.PopupToggleFallbackKey); strings.Contains(line, spare) {
		t.Errorf("the legend reads %q, want one key for the gesture rather than %q beside it", line, spare)
	}
}

// A legend is only worth having if it is true. The prototype's bar is shared
// boilerplate across its four variants and is partly fiction: "!" is not the
// Popup shell's key, "x" was never Takeover's key, and "q" never meant quit —
// the Dashboard answers to no quit key at all.
func TestTheDockLegendIsHonestAboutWhatTheKeysDo(t *testing.T) {
	line := dockLegend(t)

	for _, fiction := range []string{"! popup", "x takeover", "q quit"} {
		if strings.Contains(line, fiction) {
			t.Errorf("the legend reads %q, want no %q in it", line, fiction)
		}
	}
	if !strings.Contains(line, "popup shell") {
		t.Errorf("the legend reads %q, want the Popup shell on the key that opens it", line)
	}
}

// Ctrl+backtick has to survive the Dock, which is the client the emulator
// actually talks to: a terminal sends it apart from the NUL byte it otherwise
// collapses to only when the application has asked for extended keys, and the
// Sessions server asking is no use when the Dock in front of it has not. A Dock
// that never asks hands the Sessions server a key indistinguishable from
// Ctrl+Space, and the Popup shell's own toggle never fires however correctly it
// is bound behind it.
//
// Driven through all three levels the harness really runs — an emulator, the
// Dock, and the Sessions server — with the chord written in as an emulator that
// has been asked for extended keys writes it.
func TestThePopupChordSurvivesTheDock(t *testing.T) {
	layout, record := installedWithRecorder(t)
	tmuxWithConf(t, layout.UserConf)
	sessions := sessionsSocket(t)

	dock := shortSocket(sessions, "dock")
	run(t, "tmux", "-L", dock, "-f", dockConf(t), "new-session", "-d", "-s", "dock",
		"sh", "-c", "env -u TMUX tmux -L "+sessions+" attach -t =probe; sleep 60")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", dock, "kill-server").Run() })

	emulator := shortSocket(sessions, "em")
	run(t, "tmux", "-L", emulator, "new-session", "-d", "-x", "160", "-y", "45",
		"sh", "-c", "env -u TMUX tmux -L "+dock+" attach -t =dock; sleep 60")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", emulator, "kill-server").Run() })

	// Ctrl+backtick in the encoding a terminal that has been asked for extended
	// keys sends: the key's own code point and the Control modifier, rather
	// than the NUL every terminal falls back to. Written into the emulator's
	// pane, which is the Dock client's own input, the way pressKey does.
	if !settled(func() bool {
		run(t, "tmux", "-L", emulator, "send-keys", "-H", "-t", ":0.0", "1b", "5b", "39", "36", "3b", "35", "75")
		return strings.Contains(read(t, record), "popup open")
	}) {
		t.Errorf("the recorder got %q, want %s to have reached the Popup shell through the Dock", read(t, record), tmuxconf.PopupToggleKey)
	}
}

// shortSocket names a socket after what it is for rather than after the test:
// a unix socket's path runs out well before a Go test name does.
func shortSocket(after, purpose string) string {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(after))
	return fmt.Sprintf("gan-%s-%08x", purpose, sum.Sum32())
}

// run is a tmux command that has to work for the test to mean anything.
func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
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
// has never spoken to draws no counts rather than an error, and one it has
// draws them as they were written. The signature beside them is the fragment's
// own — see the test below.
func TestTheStripShowsWhatTheDashboardHasWrittenAndNothingBefore(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)

	if got := plain(tmux("display-message", "-p", "#{E:status-right}")); strings.TrimSpace(got) != "ganymede" {
		t.Errorf("a Dashboard that has said nothing leaves %q on the status line", got)
	}

	tmux("set", "-g", tmuxconf.AttentionOption, "█ 2 blocked")

	if got := tmux("display-message", "-p", "#{E:status-right}"); !strings.Contains(got, "█ 2 blocked") {
		t.Errorf("status line shows %q, want what the Dashboard wrote", got)
	}
}

// A harness window has to be tellable from a plain terminal, so the working
// client's status line signs itself — and signs itself alone when nothing is
// waiting on you, since a separator with nothing on the far side of it is
// punctuation left behind rather than a strip.
func TestTheStatusLineSignsItselfWithNoDanglingSeparator(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)
	showing := attachedAt(t, sessionsSocket(t), 120)

	if !settled(func() bool { return signedAlone(showing()) }) {
		t.Errorf("a Dashboard that has said nothing signs the status line %q, want %q with nothing hanging off it", showing(), "ganymede")
	}

	tmux("set", "-g", tmuxconf.AttentionOption, "█ 2 blocked")

	if !settled(func() bool { return strings.HasSuffix(showing(), "█ 2 blocked · ganymede") }) {
		t.Errorf("status line reads %q, want the count, the separator and the signature", showing())
	}

	// Written empty is not the same state as never written, and it is the one
	// the Dashboard leaves behind every time the working set goes quiet.
	tmux("set", "-g", tmuxconf.AttentionOption, "")

	if !settled(func() bool { return signedAlone(showing()) }) {
		t.Errorf("a working set gone quiet leaves %q on the status line, want the signature alone", showing())
	}
}

// signedAlone is the status line of a harness nothing is waiting on: signed,
// with no count and no punctuation left where one used to be.
func signedAlone(line string) bool {
	return strings.HasSuffix(line, "ganymede") && !strings.Contains(line, "·")
}

// A pane too narrow for both gives up the signature, not the count: tmux trims
// this segment from its left end, so left to itself it would eat the number the
// line exists to carry and keep the word that means least.
func TestANarrowStatusLineKeepsTheCountAndGivesUpTheSignature(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)
	tmux("set", "-g", tmuxconf.AttentionOption, "█ 2 blocked · ● 3 ready")
	showing := attachedAt(t, sessionsSocket(t), 40)

	if !settled(func() bool { return strings.HasSuffix(showing(), "█ 2 blocked · ● 3 ready") }) {
		t.Errorf("a 40-column status line reads %q, want both counts whole", showing())
	}
	if strings.Contains(showing(), "ganymede") {
		t.Errorf("a 40-column status line reads %q, want the signature given up rather than the count", showing())
	}
}

// Stock tmux draws its status line in green, which is the one loud thing in an
// otherwise dark Dock. The harness owns that line already (it puts the strip
// there), so it dresses it too.
func TestTheWorkingClientsStatusLineIsDrawnInTheHarnessesPalette(t *testing.T) {
	layout := layoutIn(t)
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	tmux := tmuxWithConf(t, layout.UserConf)

	style := tmux("show-options", "-A", "-g", "-v", "status-style")
	if strings.Contains(style, "green") {
		t.Errorf("status-style = %q, want the harness's own colours rather than tmux's default green", style)
	}
	if !strings.Contains(style, "bg=#") || !strings.Contains(style, "fg=#") {
		t.Errorf("status-style = %q, want a foreground and a background of the harness's own", style)
	}
}

// The whole path, through real tmux: a client attaches to a Session's pane,
// tmux reports the focus, and the harness is run for that pane's own process —
// which is what the Dashboard turns back into Sessions you have now seen.
func TestFocusLandingOnAPaneRunsTheHarnessForIt(t *testing.T) {
	layout, record := installedWithRecorder(t)
	tmux := tmuxWithConf(t, layout.UserConf)
	// The window.pane suffix matters: a bare session target leaves
	// display-message with no pane to read #{pane_pid} off, and prints
	// nothing rather than failing — which would make this check pass
	// whatever the recorder actually received.
	pane := tmux("display-message", "-p", "-t", "=probe:0.0", "#{pane_pid}")

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
