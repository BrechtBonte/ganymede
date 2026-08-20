package release_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/release"
)

// claudeThat writes a stand-in for the Claude Code binary: one that answers
// `--version` and `doctor` with what it was given, and refuses anything else.
func claudeThat(t *testing.T, version, doctor string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	// Every run is recorded once it has answered, because how often this is
	// asked anything is itself a thing the watch is answerable for: the whole
	// cost of the feature is the processes it spawns. Recorded after rather
	// than before, so that a test waiting for a run and then moving the
	// version on cannot land inside the run it was waiting for.
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  --version) cat %q ;;
  doctor) cat %q ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
echo "$*" >> %q
`, filepath.Join(dir, "version"), filepath.Join(dir, "doctor"), filepath.Join(dir, "asked"))

	for name, says := range map[string]string{"version": version, "doctor": doctor, "claude": script} {
		mode := os.FileMode(0o644)
		if name == "claude" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(says), mode); err != nil {
			t.Fatalf("write the stand-in Claude Code's %s: %v", name, err)
		}
	}
	return path
}

// asks is how many times the stand-in Claude Code has been run.
func asks(t *testing.T, claude string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(filepath.Dir(claude), "asked"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read what the stand-in Claude Code was asked: %v", err)
	}
	return len(strings.Fields(strings.TrimSpace(string(body))))
}

// awaitAsked holds until the stand-in Claude Code has been run at least count

// awaitAsked holds until the stand-in Claude Code has been run at least count
// times, so that a test moving it on to a new version cannot get there first.
func awaitAsked(t *testing.T, claude string, count int, description string) {
	t.Helper()
	for waited := time.Now(); time.Since(waited) < 5*time.Second; time.Sleep(5 * time.Millisecond) {
		if asks(t, claude) >= count {
			return
		}
	}
	t.Fatalf("Claude Code was never asked %s", description)
}

// publishing is a stand-in for the release bucket: one version string per

// nowSaying moves the stand-in Claude Code on to a new version, as an
// auto-update would while the Dashboard was up.
func nowSaying(t *testing.T, claude, version string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(claude), "version"), []byte(version), 0o644); err != nil {
		t.Fatalf("move the stand-in Claude Code to %s: %v", version, err)
	}
}

// A notice standing on the Dashboard is confirmed against the install far more
// often than the bucket is asked, so that the update you have just installed
// takes the notice down with it. Claude Code updates itself whenever a Session

// claudeHanging is a Claude Code that never answers.
func claudeHanging(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write the stand-in Claude Code: %v", err)
	}
	return path
}

// A Claude Code that never answers must not wedge the watch. Reading the
// install is the one part of a check made outside Read — the confirm while a
// notice is standing calls it directly — so the give-up has to belong to it

// publishing is a stand-in for the release bucket: one version string per
// channel, at the path the real one serves them from.
func publishing(t *testing.T, channels map[string]string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version, published := channels[strings.TrimPrefix(r.URL.Path, "/")]
		if !published {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, version)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func read(t *testing.T, c release.Checker) release.Update {
	t.Helper()
	update, err := c.Read(t.Context())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return update
}

// TestReadReportsTheInstalledVersion takes the version out of what Claude Code

// TestReadReportsTheInstalledVersion takes the version out of what Claude Code
// prints for itself, which is a version and then its own name.
func TestReadReportsTheInstalledVersion(t *testing.T) {
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: latest\n"),
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
	}
	if installed := read(t, checker).Installed; installed != "2.1.237" {
		t.Errorf("Installed = %q, want %q", installed, "2.1.237")
	}
}

// TestReadFollowsTheAutoUpdateChannel compares against the channel this
// install actually updates from. A stable install measured against the latest
// channel would be told about an update it is never going to be given.
func TestReadFollowsTheAutoUpdateChannel(t *testing.T) {
	doctor := `Claude Code doctor

Running: native (2.1.228)
Config install method: native
Auto-updates: enabled
Auto-update channel: stable
`
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.228 (Claude Code)\n", doctor),
		Releases: publishing(t, map[string]string{"latest": "2.1.240", "stable": "2.1.228"}),
	}
	update := read(t, checker)
	if update.Channel != "stable" {
		t.Errorf("Channel = %q, want %q", update.Channel, "stable")
	}
	if update.Latest != "2.1.228" {
		t.Errorf("Latest = %q, want %q", update.Latest, "2.1.228")
	}
	if update.Behind() {
		t.Error("Behind() = true, want false: the stable install is on the stable channel's build")
	}
}

// TestReadFallsBackToLatest keeps the check working when doctor's output has
// moved. It is human-facing text with no promise about its shape, so the
// channel is best-effort: the channel the great majority of installs are on is
// a better guess than giving up the check altogether.
func TestReadFallsBackToLatest(t *testing.T) {
	for _, doctor := range []string{
		"Claude Code doctor\n\nRunning: native (2.1.237)\n",
		"",
	} {
		checker := release.Checker{
			Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", doctor),
			Releases: publishing(t, map[string]string{"latest": "2.1.240", "stable": "2.1.228"}),
		}
		update := read(t, checker)
		if update.Channel != "latest" {
			t.Errorf("doctor %q: Channel = %q, want %q", doctor, update.Channel, "latest")
		}
		if update.Latest != "2.1.240" {
			t.Errorf("doctor %q: Latest = %q, want %q", doctor, update.Latest, "2.1.240")
		}
	}
}

// TestReadFailsRatherThanHalfAnswering keeps a failed check out of the
// Dashboard. Neither half of the comparison is worth drawing on its own, and
// the Update a failed check returns is the empty one — never the half it did
// manage to read.
func TestReadFailsRatherThanHalfAnswering(t *testing.T) {
	for _, c := range []struct {
		what    string
		checker release.Checker
	}{
		{
			what: "no claude to ask",
			checker: release.Checker{
				Claude:   filepath.Join(t.TempDir(), "no-claude-here"),
				Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
			},
		},
		{
			what: "a channel nothing is published for",
			checker: release.Checker{
				Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: nightly\n"),
				Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
			},
		},
		{
			what: "a bucket serving something that is not a version",
			checker: release.Checker{
				Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: latest\n"),
				Releases: publishing(t, map[string]string{"latest": "<html>nope</html>"}),
			},
		},
	} {
		update, err := c.checker.Read(t.Context())
		if err == nil {
			t.Errorf("%s: Read() succeeded, want an error", c.what)
		}
		if update != (release.Update{}) {
			t.Errorf("%s: Read() = %+v, want the empty Update", c.what, update)
		}
	}
}

// A Claude Code that never answers must not wedge the watch. Reading the
// install is the one part of a check made outside Read — the confirm while a
// notice is standing calls it directly — so the give-up has to belong to it
// rather than to the check it is usually part of.
func TestReadingTheInstallGivesUpOnAClaudeThatHangs(t *testing.T) {
	checker := release.Checker{Claude: claudeHanging(t), Timeout: 100 * time.Millisecond}

	waited := time.Now()
	_, err := checker.Installed(t.Context())

	if err == nil {
		t.Error("a Claude Code that hung reported a version rather than an error")
	}
	if held := time.Since(waited); held > 5*time.Second {
		t.Errorf("waited %v on a Claude Code that hung, want giving up after %v", held, checker.Timeout)
	}
}

// nowSaying moves the stand-in Claude Code on to a new version, as an
