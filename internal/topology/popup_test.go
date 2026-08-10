package topology_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
	"github.com/BrechtBonte/ganymede/internal/topology"
)

// pressPopupToggle presses key at the emulator standing in for the Ghostty
// window, the way TestPopupToggleFallbackRunsPopupOpen does for the
// Sessions server's own opening half: written into the emulator's pane
// rather than asked of a server directly, since send-keys writes straight
// into a pane's input and never goes through a key table at all — only an
// attached client's own input loop does that.
func pressPopupToggle(t *testing.T, key string) {
	t.Helper()
	emu := emulatorSocket(t)
	out, err := exec.Command("tmux", "-L", emu, "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		t.Fatalf("list the emulator's session: %v", err)
	}
	target := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if out, err := exec.Command("tmux", "-L", emu, "send-keys", "-t", target, key).CombinedOutput(); err != nil {
		t.Fatalf("send-keys %s: %v\n%s", key, err, out)
	}
}

// workingPane is the pane id of a repo's own Main root window — the pane a
// popup opened from the working client actually shows over.
func workingPane(t *testing.T, h topology.Harness, session string) string {
	t.Helper()
	return tmuxOn(t, h.Socket, "display-message", "-p", "-t", "="+session+":0.0", "#{pane_id}")
}

// dockAttached brings up an emulator and waits for both of the dock's
// clients to actually be attached — display-popup needs a real client on
// the target pane, and attachEmulator only starts one; it does not wait for
// the nested attach on the other end to have completed.
func dockAttached(t *testing.T, h topology.Harness) {
	t.Helper()
	attachEmulator(t, h, 160, 45)
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_session}").Output()
		return err == nil && len(strings.Fields(string(out))) == 2
	}) {
		t.Fatal("the dock's clients never attached")
	}
}

// openPopup opens the Popup shell for dir over pane, in a goroutine — Open
// blocks for as long as the overlay is on screen, exactly like the real
// tmux display-popup command it wraps — and returns the owner name together
// with a function that closes it and waits for Open to return.
func openPopup(t *testing.T, h topology.Harness, dir, pane string) (owner string, closeAndWait func()) {
	t.Helper()
	owner = popup.OwnerName(dir)
	done := make(chan error, 1)
	go func() { done <- h.OpenPopup(dir, pane) }()

	// Not tmuxOn: the popup socket has no server at all until the first
	// popup opens, and asking it before that is an error tmuxOn would
	// fatal the test over rather than let the poll keep waiting.
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.PopupSocket, "list-sessions", "-F", "#{session_name}").Output()
		return err == nil && strings.Contains(string(out), owner)
	}) {
		t.Fatalf("no hidden popup session ever appeared for %s", dir)
	}

	return owner, func() {
		t.Helper()
		_ = exec.Command("tmux", "-L", h.PopupSocket, "detach-client", "-s", owner).Run()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("OpenPopup: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("OpenPopup never returned once the popup was closed")
		}
	}
}

// startSleeping puts a long-running command in the popup's pane, standing in
// for something you typed at its prompt yourself. respawn-pane replaces the
// pane's process directly rather than typing the command in via send-keys:
// what these tests are after is whether the hidden session and its running
// command survive a hide and reopen, not whether keystrokes reach a real
// interactive shell — and which shell that even is, and how its own rc files
// have readline configured, is exactly the kind of thing a test must not
// depend on.
func startSleeping(t *testing.T, h topology.Harness, owner string) {
	t.Helper()
	if !settles(func() bool {
		_ = exec.Command("tmux", "-L", h.PopupSocket, "respawn-pane", "-k", "-t", "="+owner+":0.0", "sleep", "250").Run()
		return tmuxOn(t, h.PopupSocket, "display-message", "-p", "-t", "="+owner+":0.0", "#{pane_current_command}") == "sleep"
	}) {
		t.Fatal("sleep never started running in the popup")
	}
}

// The toggle has to close the very first popup this harness ever opens, on
// a popup socket that had no server running on it at Ensure time — the
// ordinary state of things right after install, and after every reboot,
// since a tmux server never survives one.
func TestTheToggleClosesThePopupItOpened(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner := popup.OwnerName(repo)
	done := make(chan error, 1)
	go func() { done <- h.OpenPopup(repo, workingPane(t, h, session)) }()

	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.PopupSocket, "list-sessions", "-F", "#{session_name}").Output()
		return err == nil && strings.Contains(string(out), owner)
	}) {
		t.Fatal("no hidden popup session ever appeared")
	}

	if !settles(func() bool {
		pressPopupToggle(t, tmuxconf.PopupToggleFallbackKey)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("OpenPopup: %v", err)
			}
			return true
		default:
			return false
		}
	}) {
		t.Fatal("the toggle never closed the popup it opened")
	}
}

// Opening puts the popup exactly where the pane it was pressed from is
// standing — the ordinary case (§8).
func TestOpenPopupOpensAtThePanesDirectory(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	defer closeAndWait()

	got := tmuxOn(t, h.PopupSocket, "display-message", "-p", "-t", "="+owner+":0.0", "#{pane_current_path}")
	if want := resolved(t, repo); got != want {
		t.Errorf("popup opened in %q, want %q", got, want)
	}
}

// Closing hides rather than kills: the hidden session, and whatever it was
// running, survives — that is the whole point of the Popup shell (§8).
func TestClosingThePopupHidesRatherThanKills(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	startSleeping(t, h, owner)

	closeAndWait()

	sessions := tmuxOn(t, h.PopupSocket, "list-sessions", "-F", "#{session_name}")
	if !strings.Contains(sessions, owner) {
		t.Fatalf("hidden session gone after closing, want it kept: %v", sessions)
	}
	if got := tmuxOn(t, h.PopupSocket, "display-message", "-p", "-t", "="+owner+":0.0", "#{pane_current_command}"); got != "sleep" {
		t.Errorf("popup's command after closing = %q, want the sleep still running", got)
	}
}

// Reopening lands on the very same hidden session — the whole reason closing
// hides at all rather than starting fresh every time.
func TestReopeningLandsOnTheSameHiddenSession(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)
	pane := workingPane(t, h, session)

	owner, closeAndWait := openPopup(t, h, repo, pane)
	startSleeping(t, h, owner)
	closeAndWait()

	_, reopen := openPopup(t, h, repo, pane)
	defer reopen()

	if got := tmuxOn(t, h.PopupSocket, "list-sessions", "-F", "#{session_name}"); strings.Count(got, owner) != 1 {
		t.Errorf("popup sessions = %q, want exactly one session for %s — reopening must not create a second", got, owner)
	}
	if got := tmuxOn(t, h.PopupSocket, "display-message", "-p", "-t", "="+owner+":0.0", "#{pane_current_command}"); got != "sleep" {
		t.Errorf("reopened popup's command = %q, want the sleep from before still running", got)
	}
}

// A hidden popup with a command running is what earns its owner's row a
// busy marker (§8) — an idle one at its own prompt earns nothing.
func TestSweepReportsBusyOnlyWhileACommandIsRunning(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	defer closeAndWait()

	if !settles(func() bool {
		statuses, err := h.Sweep([]string{repo})
		return err == nil && !statuses[resolved(t, repo)].Busy
	}) {
		t.Error("a popup sitting at its own prompt was reported busy")
	}

	startSleeping(t, h, owner)

	if !settles(func() bool {
		statuses, err := h.Sweep([]string{repo})
		return err == nil && statuses[resolved(t, repo)].Busy && strings.Contains(statuses[resolved(t, repo)].Command, "sleep")
	}) {
		statuses, _ := h.Sweep([]string{repo})
		t.Errorf("Sweep = %+v, want the running sleep reported busy", statuses)
	}
}

// The popup is killed when its owning Session goes Gone (§8): a directory
// Sweep is not told about anymore has nothing left running in it, so its
// hidden session goes with it.
func TestSweepKillsAPopupWhoseOwnerHasGone(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	closeAndWait()

	if _, err := h.Sweep(nil); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// Not tmuxOn: killing the only session left on the popup socket takes
	// the whole (otherwise empty) server down with it, which list-sessions
	// reports as an error rather than as a socket with nothing on it.
	if !settles(func() bool {
		out, err := exec.Command("tmux", "-L", h.PopupSocket, "list-sessions", "-F", "#{session_name}").Output()
		return err != nil || !strings.Contains(string(out), owner)
	}) {
		t.Errorf("hidden session %s survived a sweep with its directory not live", owner)
	}
}

// A live popup's owner survives a sweep untouched.
func TestSweepLeavesALivePopupAlone(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	defer closeAndWait()

	if _, err := h.Sweep([]string{repo}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if sessions := tmuxOn(t, h.PopupSocket, "list-sessions", "-F", "#{session_name}"); !strings.Contains(sessions, owner) {
		t.Errorf("Sweep killed a popup whose directory is still live: %v", sessions)
	}
}

// A session on the popup socket that popup.OwnerName never produced is not
// a popup at all — a stray one of the user's own, or the keepalive session
// ensurePopups starts — and a sweep must leave it alone entirely, killed or
// not, and never let it stand in for a directory it has nothing to do with.
func TestSweepNeverTouchesASessionThatIsNotOneOfOurs(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	tmuxOn(t, h.PopupSocket, "new-session", "-d", "-s", "not-a-popup", "-c", repo)

	statuses, err := h.Sweep(nil)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if sessions := tmuxOn(t, h.PopupSocket, "list-sessions", "-F", "#{session_name}"); !strings.Contains(sessions, "not-a-popup") {
		t.Errorf("Sweep killed a session that was never one of ours: %v", sessions)
	}
	if _, reported := statuses[resolved(t, repo)]; reported {
		t.Errorf("Sweep = %+v, want the stray session left out of the report", statuses)
	}
}

// The ordinary tmux prefix is never rebound on the popup socket — only the
// no-prefix toggle is (see ensurePopups) — so a second window opened inside
// a popup is a real possibility, and a command running there is exactly as
// busy as one running in the first.
func TestSweepFindsACommandBusyInAnyOfThePopupsWindows(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)

	owner, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	defer closeAndWait()
	tmuxOn(t, h.PopupSocket, "new-window", "-d", "-t", "="+owner+":")
	if err := exec.Command("tmux", "-L", h.PopupSocket, "respawn-window", "-k", "-t", "="+owner+":1", "sleep", "250").Run(); err != nil {
		t.Fatalf("respawn-window: %v", err)
	}

	if !settles(func() bool {
		statuses, err := h.Sweep([]string{repo})
		return err == nil && statuses[resolved(t, repo)].Busy
	}) {
		statuses, _ := h.Sweep([]string{repo})
		t.Errorf("Sweep = %+v, want the second window's sleep reported busy", statuses)
	}
}

// A socket that has simply never had a server on it — every machine's state
// before the first Ensure ever runs — is not a failure: there is nothing on
// it because nothing has opened a popup yet, the same as any other quiet
// repo.
func TestSweepOnAnUntouchedSocketReportsNothing(t *testing.T) {
	h := topology.Harness{PopupSocket: "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-untouched"}

	statuses, err := h.Sweep(nil)
	if err != nil {
		t.Errorf("Sweep: %v, want no error for a socket nothing has ever opened a popup on", err)
	}
	if len(statuses) != 0 {
		t.Errorf("Sweep = %+v, want nothing reported", statuses)
	}
}

// A socket that genuinely cannot be asked — as opposed to one with nothing
// on it yet — must fail loudly: a Dashboard told "no popups" the same way
// for both would wipe every busy marker it knows about the moment a real
// problem, not a quiet repo, is what it is looking at.
func TestSweepFailsRatherThanReportEveryPopupGoneWhenTheSocketIsBroken(t *testing.T) {
	socket := "ganymede-test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-broken"
	sockPath := filepath.Join("/tmp", "tmux-"+strconv.Itoa(os.Getuid()), socket)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// A plain file where tmux expects a socket answers every command with a
	// real error distinct from "no server has ever run here".
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	h := topology.Harness{PopupSocket: socket}
	if _, err := h.Sweep(nil); err == nil {
		t.Error("Sweep succeeded against a socket that is not a socket at all, want an error")
	}
}

// Nothing about opening or sweeping a popup may touch a repo's own tmux
// session — it is you at the keyboard, never an agent (§8).
func TestPopupsNeverTouchTheRepoSession(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dockAttached(t, h)
	session, _ := topology.WorkingSessionName(repo)
	before := tmuxOn(t, h.Socket, "list-sessions", "-F", "#{session_name}")

	_, closeAndWait := openPopup(t, h, repo, workingPane(t, h, session))
	if _, err := h.Sweep([]string{repo}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	closeAndWait()
	if _, err := h.Sweep(nil); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if after := tmuxOn(t, h.Socket, "list-sessions", "-F", "#{session_name}"); after != before {
		t.Errorf("Sessions server's sessions changed: before %q, after %q", before, after)
	}
}

// Nothing has told the harness where the Dashboard's cursor is until the
// Dashboard actually says — a popup opened before that has to fall back to
// the pane it was pressed from (see popup.TargetDir) rather than reading
// nothing as a directory of its own.
func TestSelectedDirWithNothingWrittenYetIsEmpty(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if got := h.SelectedDir(); got != "" {
		t.Errorf("SelectedDir = %q, want empty before the Dashboard has said anything", got)
	}
}

// Selected is what the Dashboard uses to say which repo its cursor is on,
// and SelectedDir is how the popup toggle reads it back — the two ends of
// the same option (§8).
func TestSelectedRecordsWhatSelectedDirReadsBack(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	h := testHarness(t, repo)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if err := h.Selected(repo); err != nil {
		t.Fatalf("Selected: %v", err)
	}
	if got := h.SelectedDir(); got != repo {
		t.Errorf("SelectedDir = %q, want %q", got, repo)
	}
}
