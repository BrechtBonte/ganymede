package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/hooks"
)

// installed is what the harness runs on each event, as `ganymede install`
// writes it.
var installed = hooks.Command("/Users/b/Projects/ganymede/bin/ganymede")

// theHarnessReads are the events the state model needs to tell the states
// apart, which is what Install has to put the harness on. Listed here rather
// than read out of the package, so that dropping one is a failing test.
var theHarnessReads = []string{
	// Ready, and the message that makes it worth reading.
	"Stop",
	// Blocked, with its reason, before the registry has caught up.
	"PermissionRequest",
	"Notification",
	// Seen, which clears Ready.
	"UserPromptSubmit",
	// What the harness holds about a Session, and when to let go of it.
	"SessionStart",
	"SessionEnd",
}

// settingsWith writes the user's settings.json and returns its path.
func settingsWith(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
	}
	return path
}

func install(t *testing.T, path string) {
	t.Helper()
	if err := hooks.Install(path, installed); err != nil {
		t.Fatalf("Install: %v", err)
	}
}

func body(t *testing.T, path string) string {
	t.Helper()
	read, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	return string(read)
}

// handlers returns every hook handler installed on an event, whoever put it
// there.
func handlers(t *testing.T, path, event string) []map[string]any {
	t.Helper()
	var settings struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(body(t, path)), &settings); err != nil {
		t.Fatalf("settings are not valid JSON after Install: %v\n%s", err, body(t, path))
	}
	var found []map[string]any
	for _, group := range settings.Hooks[event] {
		found = append(found, group.Hooks...)
	}
	return found
}

// harnessCommands returns the commands on an event that run the harness.
func harnessCommands(t *testing.T, path, event string) []string {
	t.Helper()
	var found []string
	for _, handler := range handlers(t, path, event) {
		command, _ := handler["command"].(string)
		if strings.Contains(command, "ganymede") {
			found = append(found, command)
		}
	}
	return found
}

// Hooks go in the user-level settings so that every repo is covered, and on
// every event the state model reads — a missing one is a state the Dashboard
// cannot show.
func TestInstallPutsTheHarnessOnEveryEventItReads(t *testing.T) {
	path := settingsWith(t, "")

	install(t, path)

	for _, event := range theHarnessReads {
		if commands := harnessCommands(t, path, event); len(commands) != 1 {
			t.Errorf("%s runs the harness %d times, want once: %v", event, len(commands), commands)
		}
	}
}

// A hook command runs inside a Session's turn. Claude Code is told not to wait
// for this one, so the permission dialog in the pane never lags behind the
// harness — and PermissionRequest stays a reporter that could not answer a
// dialog if it wanted to.
func TestInstalledHooksNeverMakeASessionWaitOnTheHarness(t *testing.T) {
	path := settingsWith(t, "")

	install(t, path)

	for _, event := range theHarnessReads {
		for _, handler := range handlers(t, path, event) {
			command, _ := handler["command"].(string)
			if !strings.Contains(command, "ganymede") {
				continue
			}
			if async, _ := handler["async"].(bool); !async {
				t.Errorf("%s waits on the harness: %v", event, handler)
			}
		}
	}
}

// settings.json is the user's file, holding their model, their permissions,
// their own hooks and their secrets. The harness adds itself to it and touches
// nothing else.
func TestInstallKeepsEverythingElseInTheUsersSettings(t *testing.T) {
	path := settingsWith(t, `{
	  "env": {"SOME_API_KEY": "sk-not-a-real-secret"},
	  "model": "opus[1m]",
	  "permissions": {"allow": ["Read(~/.claude/memory/*)"], "defaultMode": "auto"},
	  "hooks": {
	    "Stop": [
	      {"hooks": [
	        {"type": "command", "command": "osascript -e 'display notification \"done\"'"}
	      ]}
	    ],
	    "PreToolUse": [
	      {"matcher": "Bash", "hooks": [{"type": "command", "command": "node block-dangerous.js"}]}
	    ],
	    "WorktreeCreate": [
	      {"hooks": [{"type": "command", "command": "cow-sync.sh", "timeout": 180}]}
	    ]
	  }
	}`)

	install(t, path)

	after := body(t, path)
	for _, kept := range []string{
		"sk-not-a-real-secret", "opus[1m]", "Read(~/.claude/memory/*)",
		"osascript", "node block-dangerous.js", "cow-sync.sh",
	} {
		if !strings.Contains(after, kept) {
			t.Errorf("Install lost %q from the user's settings:\n%s", kept, after)
		}
	}
	// The user's own Stop hook still runs, alongside the harness's.
	if got := len(handlers(t, path, "Stop")); got != 2 {
		t.Errorf("Stop has %d handlers, want the user's and the harness's:\n%s", got, after)
	}
}

// A timeout of 180 has to still be 180 afterwards. Reading JSON into Go and
// writing it back turns every number into a float, and a large one comes back
// in exponent notation that Claude Code would read as something else entirely.
func TestInstallLeavesTheUsersNumbersExactlyAsTheyWere(t *testing.T) {
	path := settingsWith(t, `{"hooks": {"WorktreeCreate": [{"hooks": [
	  {"type": "command", "command": "cow-sync.sh", "timeout": 180}]}]},
	  "someCount": 1786272362730, "someRatio": 0.5}`)

	install(t, path)

	after := body(t, path)
	for _, want := range []string{"180", "1786272362730", "0.5"} {
		if !strings.Contains(after, want) {
			t.Errorf("the number %s did not survive Install:\n%s", want, after)
		}
	}
}

// `ganymede up` installs on every run, and a harness that appended a second
// copy of itself each time would end up reporting every event six times over.
func TestInstallingTwiceChangesNothingTheSecondTime(t *testing.T) {
	path := settingsWith(t, `{"model": "opus[1m]", "hooks": {"Stop": [{"hooks": [
	  {"type": "command", "command": "osascript -e 'display notification'"}]}]}}`)

	install(t, path)
	afterFirst := body(t, path)
	install(t, path)

	if afterSecond := body(t, path); afterSecond != afterFirst {
		t.Errorf("the second install changed the file:\n--- first ---\n%s\n--- second ---\n%s", afterFirst, afterSecond)
	}
}

// The binary moves — rebuilt somewhere else, installed properly. The old
// command has to go, or Claude Code keeps running a ganymede that is not there.
func TestInstallReplacesTheHarnessCommandFromAnOlderBuild(t *testing.T) {
	path := settingsWith(t, `{"hooks": {"Stop": [{"hooks": [
	  {"type": "command", "command": "/old/path/to/ganymede hook", "async": true}]}]}}`)

	install(t, path)

	commands := harnessCommands(t, path, "Stop")
	if len(commands) != 1 || commands[0] != installed {
		t.Errorf("Stop runs %v, want only the harness as it is now (%s)", commands, installed)
	}
}

// Settings the harness cannot read are settings it must not rewrite: a
// hand-edited file with a trailing comma is a thing to be told about, not to
// lose.
func TestInstallRefusesSettingsItCannotRead(t *testing.T) {
	broken := `{"model": "opus[1m]",}`
	path := settingsWith(t, broken)

	err := hooks.Install(path, installed)

	if err == nil {
		t.Fatal("Install rewrote settings it could not parse")
	}
	if after := body(t, path); after != broken {
		t.Errorf("Install changed settings it could not parse:\n%s", after)
	}
}

// A machine where Claude Code has never written settings still has to be able
// to run the harness.
func TestInstallCreatesSettingsThatWereNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")

	if err := hooks.Install(path, installed); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if commands := harnessCommands(t, path, "Stop"); len(commands) != 1 {
		t.Errorf("Stop runs %v, want the harness", commands)
	}
}

// The path a hook command names is written for a shell, and a harness built in
// a directory with a space in it must not turn into two words.
func TestTheHookCommandSurvivesASpaceInThePath(t *testing.T) {
	command := hooks.Command("/Users/b/My Projects/ganymede")

	if !strings.Contains(command, `'/Users/b/My Projects/ganymede'`) {
		t.Errorf("the hook command does not quote its path: %s", command)
	}
	if !strings.HasSuffix(command, " hook") {
		t.Errorf("the hook command does not forward: %s", command)
	}
}

// Hooks the harness did not write are the user's, whatever they are called.
// Installing over a wrapper script that merely has "ganymede" in its name — or
// a ganymede of an older vintage taking a different subcommand — would delete
// it from the user's settings with no backup and nothing said.
func TestInstallLeavesHooksTheHarnessDidNotWrite(t *testing.T) {
	path := settingsWith(t, `{"hooks": {"Stop": [{"hooks": [
	  {"type": "command", "command": "/Users/b/bin/ganymede-notify hook"},
	  {"type": "command", "command": "/Users/b/bin/ganymede report"},
	  {"type": "command", "command": "osascript -e 'display notification'"}
	]}]}}`)

	install(t, path)

	kept := body(t, path)
	for _, want := range []string{"ganymede-notify hook", "ganymede report", "osascript"} {
		if !strings.Contains(kept, want) {
			t.Errorf("Install deleted a hook it did not write (%q):\n%s", want, kept)
		}
	}
	if got := len(handlers(t, path, "Stop")); got != 4 {
		t.Errorf("Stop has %d handlers, want the user's three and the harness's one", got)
	}
}

// Settings shaped in a way the harness cannot read are settings it must not
// rewrite — the same rule as one it cannot parse. A hooks block that is not a
// set of events is a Claude Code this harness does not know, not an invitation
// to replace it.
func TestInstallRefusesAHooksBlockItCannotRead(t *testing.T) {
	for _, shape := range []string{
		`{"hooks": ["Stop"]}`,
		`{"hooks": {"Stop": {"command": "osascript"}}}`,
	} {
		path := settingsWith(t, shape)

		if err := hooks.Install(path, installed); err == nil {
			t.Errorf("Install rewrote settings shaped %s", shape)
		}
		if after := body(t, path); after != shape {
			t.Errorf("Install changed settings it could not read:\n%s", after)
		}
	}
}

// An explicit null is a settings file saying it has no hooks, which is a thing
// the harness understands perfectly well.
func TestInstallAddsItselfToSettingsWithNoHooksAtAll(t *testing.T) {
	path := settingsWith(t, `{"model": "opus[1m]", "hooks": null}`)

	install(t, path)

	if commands := harnessCommands(t, path, "Stop"); len(commands) != 1 {
		t.Errorf("Stop runs %v, want the harness", commands)
	}
}

// settings.json can hold an API key, and the user may well have said so by
// closing its permissions. Installing a hook is no reason to open them again.
func TestInstallLeavesTheSettingsPermissionsAlone(t *testing.T) {
	path := settingsWith(t, `{"env": {"SOME_API_KEY": "sk-not-a-real-secret"}}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("close the settings: %v", err)
	}

	install(t, path)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if mode := after.Mode().Perm(); mode != 0o600 {
		t.Errorf("settings are now %o, want the %o the user left them at", mode, 0o600)
	}
}

// Settings kept in a dotfiles repo and linked into place have to stay linked:
// replacing the link with a file of our own would leave the repo holding a
// copy that is no longer the one Claude Code reads.
func TestInstallWritesThroughASymlinkedSettingsFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles-settings.json")
	if err := os.WriteFile(real, []byte(`{"model": "opus[1m]"}`), 0o644); err != nil {
		t.Fatalf("seed the dotfiles copy: %v", err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("link the settings: %v", err)
	}

	install(t, link)

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the settings are no longer a link into the dotfiles repo (%v)", err)
	}
	if !strings.Contains(body(t, real), "ganymede") {
		t.Errorf("the dotfiles copy did not get the hooks:\n%s", body(t, real))
	}
}
