package topology_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
	"github.com/BrechtBonte/ganymede/internal/topology"
)

// testHarness returns a Harness on throwaway sockets, torn down with the test.
func testHarness(t *testing.T, workingDir string) topology.Harness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	prefix := "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-")
	h := topology.Harness{
		Socket:     prefix + "-sessions",
		DockSocket: prefix + "-dock",
		DockConf:   filepath.Join(t.TempDir(), "dock.conf"),
		Fragment:   fragmentFor(t),
		Dashboard:  []string{"sleep", "300"},
		WorkingDir: workingDir,
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", h.Socket, "kill-server").Run()
		_ = exec.Command("tmux", "-L", h.DockSocket, "kill-server").Run()
	})
	return h
}

// fragmentFor writes the tmux settings the harness installs, so Ensure has
// something real to apply to an already-running server.
func fragmentFor(t *testing.T) string {
	t.Helper()
	layout := tmuxconf.Layout{
		Fragment: filepath.Join(t.TempDir(), "ganymede", "tmux.conf"),
		UserConf: filepath.Join(t.TempDir(), "tmux.conf"),
	}
	if err := tmuxconf.Install(layout); err != nil {
		t.Fatalf("install fragment: %v", err)
	}
	return layout.Fragment
}

// tmuxOn queries one of the harness's tmux servers.
func tmuxOn(t *testing.T, socket string, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux -L %s %v: %v\n%s", socket, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Bringing up the harness leaves the Dashboard running in its own session,
// independent of the repo's Session.
func TestUpRunsTheDashboardInItsOwnSession(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)

	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	sessions := tmuxOn(t, h.Socket, "list-sessions", "-F", "#{session_name}")
	for _, want := range []string{topology.DashboardSession, "service-ai-assistant"} {
		if !strings.Contains(sessions, want) {
			t.Errorf("no %q session; got:\n%s", want, sessions)
		}
	}

	if got := tmuxOn(t, h.Socket, "list-panes", "-t", "="+topology.DashboardSession, "-F", "#{pane_current_command}"); got != "sleep" {
		t.Errorf("Dashboard session runs %q, want the Dashboard command", got)
	}
}

// attachEmulator stands in for the Ghostty window: a pty of a known size with
// the dock attached inside it.
func attachEmulator(t *testing.T, h topology.Harness, cols, rows int) (resize func(cols, rows int)) {
	t.Helper()
	socket := "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-emulator"
	// The emulator gives the dock a clean environment; ganymede does the same
	// for Ghostty when it is launched from inside tmux.
	command := "env -u TMUX " + strings.Join(h.AttachCommand(), " ")
	out, err := exec.Command("tmux", "-L", socket, "new-session", "-d",
		"-x", itoa(cols), "-y", itoa(rows), "sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("start emulator: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	return func(cols, rows int) {
		t.Helper()
		out, err := exec.Command("tmux", "-L", socket, "resize-window",
			"-x", itoa(cols), "-y", itoa(rows)).CombinedOutput()
		if err != nil {
			t.Fatalf("resize emulator: %v\n%s", err, out)
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// settles polls until cond holds, so tests do not race tmux's client setup.
func settles(cond func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// The topology of the harness: one window, the Dashboard docked at the left in
// its own client, the working client filling the rest.
func TestDockShowsTheDashboardBesideTheWorkingClient(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// A different size from the dock's own: the sidepanel must be pinned, not
	// scaled with the window.
	_ = attachEmulator(t, h, 160, 45)

	// Two tmux clients, one per session — the sidepanel and the working client.
	var attached string
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		if err != nil {
			return false
		}
		attached = strings.TrimSpace(string(out))
		return len(strings.Fields(attached)) == 2
	}) {
		t.Fatalf("want a client on each session, attached: %q", attached)
	}
	for _, want := range []string{topology.DashboardSession, "service-ai-assistant"} {
		if !strings.Contains(attached, want) {
			t.Errorf("no client attached to %q; attached: %q", want, attached)
		}
	}

	// The sidepanel keeps its width whatever the window size: a 160-column
	// window is 40 for the Dashboard, a divider, and the rest for the working
	// client. Scaled with the window instead, the sidepanel would be 32.
	var widths string
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.DockSocket, "list-panes", "-t", "=dock", "-F", "#{pane_width}").Output()
		if err != nil {
			return false
		}
		widths = strings.Join(strings.Fields(string(out)), " ")
		return widths == "40 119"
	}) {
		t.Errorf("pane widths settled at %q, want %q", widths, "40 119")
	}
}

// The Dashboard is not a child of the working client: ending the repo's
// Session leaves the sidepanel running.
func TestDashboardSurvivesTheWorkingSessionEnding(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
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

	tmuxOn(t, h.Socket, "kill-session", "-t", "=service-ai-assistant")

	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		return err == nil && strings.TrimSpace(string(out)) == topology.DashboardSession
	}) {
		t.Error("the Dashboard client did not survive the working Session ending")
	}
	if got := tmuxOn(t, h.Socket, "list-panes", "-t", "="+topology.DashboardSession, "-F", "#{pane_current_command}"); got != "sleep" {
		t.Errorf("Dashboard is running %q, want it still running", got)
	}
}

// Launching again reuses what is already up rather than stacking a second
// harness on top.
func TestBringingTheHarnessUpTwiceReusesIt(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	if got := tmuxOn(t, h.Socket, "list-sessions", "-F", "#{session_name}"); len(strings.Fields(got)) != 2 {
		t.Errorf("sessions after two launches = %v, want the Dashboard and one working Session", strings.Fields(got))
	}
	if got := tmuxOn(t, h.DockSocket, "list-panes", "-t", "=dock", "-F", "#{pane_index}"); len(strings.Fields(got)) != 2 {
		t.Errorf("dock panes after two launches = %v, want 2", strings.Fields(got))
	}
}

// Launching while a window is already showing the harness has to bring that
// window forward rather than open a second client on the dock: two clients on
// one session mirror each other and tmux shrinks both to the smaller size.
func TestHarnessKnowsWhenAWindowIsAlreadyShowingIt(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if h.Attached() {
		t.Error("no window is showing the dock yet, but Attached says one is")
	}

	_ = attachEmulator(t, h, 160, 45)
	if !settles(h.Attached) {
		t.Error("a window is showing the dock, but Attached says none is")
	}
}

// A repo directory with a space in it still has to end up with a live working
// client: the pane command runs through a shell, so the name needs quoting.
func TestWorkingClientSurvivesASpaceInTheRepoName(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "my repo"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	_ = attachEmulator(t, h, 160, 45)

	var attached string
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		if err != nil {
			return false
		}
		attached = strings.TrimSpace(string(out))
		return strings.Contains(attached, "my repo")
	}) {
		t.Errorf("no client attached to the %q session; attached: %q", "my repo", attached)
	}
}

// Bringing the harness up from a second repo has to re-point the working
// client, not leave the previous repo on show.
func TestUpFromAnotherRepoRepointsTheWorkingClient(t *testing.T) {
	root := t.TempDir()
	first := initRepo(t, filepath.Join(root, "service-ai-assistant"))
	second := initRepo(t, filepath.Join(root, "service-billing"))

	h := testHarness(t, first)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure in the first repo: %v", err)
	}
	_ = attachEmulator(t, h, 160, 45)
	if !settles(func() bool {
		out, _ := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		return strings.Contains(string(out), "service-ai-assistant")
	}) {
		t.Fatal("the first repo's working client never attached")
	}

	moved := h
	moved.WorkingDir = second
	if err := moved.Ensure(); err != nil {
		t.Fatalf("Ensure in the second repo: %v", err)
	}

	var attached string
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		if err != nil {
			return false
		}
		attached = strings.TrimSpace(string(out))
		return strings.Contains(attached, "service-billing")
	}) {
		t.Errorf("the working client still shows the first repo; attached: %q", attached)
	}
}

// The fragment has to reach a tmux server that was already running: tmux reads
// its configuration only at server start.
func TestEnsureAppliesTheFragmentToAnAlreadyRunningServer(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)

	// A server that started without the harness's configuration. It reads no
	// config at all, so the check below does not depend on whoever ran this
	// test having already installed the fragment into their own tmux.conf.
	tmuxOn(t, h.Socket, "-f", "/dev/null", "new-session", "-d", "-s", "elsewhere", "sleep 300")
	if got := tmuxOn(t, h.Socket, "show-options", "-A", "-s", "-v", "focus-events"); got == "on" {
		t.Fatalf("focus-events was already on; the test proves nothing")
	}

	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if got := tmuxOn(t, h.Socket, "show-options", "-A", "-s", "-v", "focus-events"); got != "on" {
		t.Errorf("focus-events = %q on the running server, want %q", got, "on")
	}
}

// Ghostty does not open at its final size: the window grows after the client
// has attached. tmux scales panes proportionally as it does, so the sidepanel
// has to be re-pinned once the new geometry is in place, not before.
func TestSidepanelKeepsItsWidthWhenTheWindowGrows(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	resize := attachEmulator(t, h, 80, 24)
	sidepanel := func() string {
		out, err := exec.Command("tmux", "-L", h.DockSocket, "list-panes", "-t", "=dock", "-F", "#{pane_width}").Output()
		if err != nil {
			return ""
		}
		return strings.Join(strings.Fields(string(out)), " ")
	}
	if !settles(func() bool { return sidepanel() == "40 39" }) {
		t.Fatalf("sidepanel in an 80-column window = %q, want %q", sidepanel(), "40 39")
	}

	resize(200, 50)

	if !settles(func() bool { return sidepanel() == "40 159" }) {
		t.Errorf("after the window grew to 200 columns, panes = %q, want %q", sidepanel(), "40 159")
	}
}
