package topology_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
	"github.com/BrechtBonte/ganymede/internal/topology"
)

// serving starts the Sessions server the way a live Dashboard always finds it:
// up already, since the Dashboard runs inside it. It reads no configuration of
// its own, so nothing here depends on whoever runs the test having installed
// the harness's fragment into their own tmux.conf — Ensure applies it.
func serving(t *testing.T, h topology.Harness) {
	t.Helper()
	tmuxOn(t, h.Socket, "-f", "/dev/null", "new-session", "-d", "-s", "probe", "sleep 300")
}

// wrote is the strip as the Dashboard last left it, with the colouring taken
// off so a test can read it as the eye does.
func wrote(t *testing.T, h topology.Harness) string {
	t.Helper()
	written := tmuxOn(t, h.Socket, "show-options", "-g", "-v", tmuxconf.AttentionOption)
	for {
		open := strings.Index(written, "#[")
		if open < 0 {
			return written
		}
		// The end of this style, not of some earlier one: a count that read
		// "1 blocked]" would otherwise leave every style in place and quietly
		// stop the assertions below from testing anything.
		close := strings.Index(written[open:], "]")
		if close < 0 {
			return written
		}
		written = written[:open] + written[open+close+1:]
	}
}

// The strip is the ambient count of what is waiting on you: both tiers, each
// with its own mark, and Blocked first — the same order the rail reads in.
func TestTheStripCountsBothTiersOfAttention(t *testing.T) {
	h := testHarness(t, t.TempDir())
	serving(t, h)

	if err := h.Show(session.Attention{Blocked: 2, Ready: 1}); err != nil {
		t.Fatalf("Show: %v", err)
	}

	strip := wrote(t, h)
	blocked, ready := strings.Index(strip, "2 blocked"), strings.Index(strip, "1 ready")
	if blocked < 0 || ready < 0 {
		t.Fatalf("the strip reads %q, want both counts", strip)
	}
	if blocked > ready {
		t.Errorf("the strip reads %q, want Blocked before Ready", strip)
	}
	if !strings.Contains(strip, session.Blocked.Glyph()) || !strings.Contains(strip, session.Ready.Glyph()) {
		t.Errorf("the strip reads %q, want the marks the rail uses", strip)
	}
}

// A tier with nothing in it says nothing: "0 blocked" is a count you would
// have to read to learn there is nothing to read.
func TestTheStripLeavesOutTheTierThatIsEmpty(t *testing.T) {
	h := testHarness(t, t.TempDir())
	serving(t, h)

	if err := h.Show(session.Attention{Ready: 3}); err != nil {
		t.Fatalf("Show: %v", err)
	}

	if strip := wrote(t, h); strings.Contains(strip, "blocked") {
		t.Errorf("the strip reads %q, want the empty tier left out", strip)
	}
}

// Nothing waiting is a blank strip. A status line that is always lit is one
// you stop seeing, which would cost the Blocked count its whole point.
func TestAQuietWorkingSetLeavesTheStripBlank(t *testing.T) {
	h := testHarness(t, t.TempDir())
	serving(t, h)
	if err := h.Show(session.Attention{Blocked: 1}); err != nil {
		t.Fatalf("Show: %v", err)
	}

	if err := h.Show(session.Attention{}); err != nil {
		t.Fatalf("Show: %v", err)
	}

	if strip := wrote(t, h); strings.TrimSpace(strip) != "" {
		t.Errorf("nothing is waiting on you and the strip reads %q", strip)
	}
}

// Where the strip belongs, through real tmux: the status line of the working
// client, under the eye of whoever is working in that Session — not off in the
// sidepanel, which is the redundancy the whole strip is for.
func TestTheWorkingClientsStatusLineCarriesTheStrip(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	serving(t, h)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	_ = attachEmulator(t, h, 160, 45)
	if !settles(func() bool {
		out, _ := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		return len(strings.Fields(string(out))) == 2
	}) {
		t.Fatal("clients never attached")
	}

	if err := h.Show(session.Attention{Blocked: 2, Ready: 1}); err != nil {
		t.Fatalf("Show: %v", err)
	}

	var window string
	if !settles(func() bool {
		window = screen(t)
		return strings.Contains(window, "2 blocked") && strings.Contains(window, "1 ready")
	}) {
		t.Errorf("the working client's status line does not carry the strip:\n%s", window)
	}
}

// screen is what the emulator's window is showing: the whole dock, sidepanel
// and working client side by side.
func screen(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", emulatorSocket(t), "capture-pane", "-p", "-t", ":0.0").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// The sidepanel is all Dashboard. The strip belongs to the Session you are
// working in, and a second copy of it under the rail would cost the rail a row
// to say what the rail already says.
func TestTheSidepanelKeepsEveryRowForTheDashboard(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)

	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if got := tmuxOn(t, h.Socket, "show-options", "-t", "="+topology.DashboardSession+":", "-v", "status"); got != "off" {
		t.Errorf("the sidepanel's status line is %q, want it left to the Dashboard", got)
	}
	if got := tmuxOn(t, h.Socket, "show-options", "-A", "-t", "=service-ai-assistant:", "-v", "status"); got != "on" {
		t.Errorf("the working Session's status line is %q, want the strip's line kept", got)
	}
}
