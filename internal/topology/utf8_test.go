package topology_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// tmux reads LC_ALL, LC_CTYPE and LANG to decide whether the terminal a client
// draws to can take UTF-8, and writes "_" in place of every UTF-8 character on
// a client it decides cannot. LaunchServices hands Ganymede.app the login
// session's environment, which names none of the three — so a harness brought
// up from the Dock rather than a terminal drew the whole rail as underscores.
func TestTheRailKeepsItsGlyphsWithNoLocaleInTheEnvironment(t *testing.T) {
	withNoLocale(t)

	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	glyph := session.Ready.Glyph()
	h.Dashboard = []string{"sh", "-c", "printf '" + glyph + "\\n'; sleep 300"}

	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	attachEmulator(t, h, 200, 50)

	// What the window actually shows, two nested clients down: the sidepanel
	// client writes into the dock's pane, and the dock's own client writes that
	// into the emulator's. Either one deciding against UTF-8 loses the glyph.
	if !settles(func() bool { return strings.Contains(emulatorPane(t), glyph) }) {
		t.Errorf("the sidepanel never drew %q; the window holds:\n%s", glyph, emulatorPane(t))
	}
}

// emulatorPane is what the window standing in for Ghostty is showing.
func emulatorPane(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", emulatorSocket(t), "capture-pane", "-p", "-t", ":0.0").Output()
	if err != nil {
		t.Fatalf("capture the emulator's pane: %v", err)
	}
	return string(out)
}

// withNoLocale takes the three variables out of this process's environment for
// the length of the test, which is what every tmux server and client the
// helpers start below inherits.
func withNoLocale(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		was, set := os.LookupEnv(name)
		t.Cleanup(func() {
			if set {
				_ = os.Setenv(name, was)
				return
			}
			_ = os.Unsetenv(name)
		})
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}
