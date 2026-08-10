package topology

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
)

// DockSession is the frame holding the two clients side by side. It lives on
// its own tmux server so it can disable its prefix — letting every key through
// to the client inside the pane — without touching the Sessions themselves.
const DockSession = "dock"

// SidepanelWidth is how many columns the Dashboard occupies.
const SidepanelWidth = 40

// Harness is the docked sidepanel topology.
type Harness struct {
	// Socket carries the Dashboard and the repo Sessions. Empty means tmux's
	// default socket — but prefer naming it, so an ambient $TMUX cannot send
	// these commands to a different server than the dock's panes reach.
	Socket string
	// Fragment is the tmux config the Sessions need, re-applied to a server
	// that was already running when it was installed.
	Fragment string
	// Dashboard is the command the Dashboard session runs.
	Dashboard []string
	// WorkingDir is the directory the working client starts in; its repo names
	// the Session.
	WorkingDir string
	// DockSocket and DockConf belong to the dock — the frame holding the two
	// clients side by side.
	DockSocket string
	DockConf   string
	// PopupSocket carries the Popup shell's hidden sessions (§8), one per
	// owner directory — its own server so a popup can never take a name a
	// repo's own Session might want, and so killing it takes every popup with
	// it rather than any repo's own history.
	PopupSocket string
	// Worktree is the command a spawned Worktree session runs, given the name
	// a spawn dialog derived and the first prompt when there is one. nil
	// means WorktreeCommand — a test substitutes something else so Spawn need
	// not run claude at all.
	Worktree func(name, prompt string) []string
}

// Ensure brings the topology up, reusing whatever is already running.
func (h Harness) Ensure() error {
	// The same name the picker would reach this repo by, so that opening a
	// repo and bringing the harness up in it land on one Session rather than
	// two — and so that a repo sharing its name with another is told apart
	// wherever it is opened from.
	working, err := h.sessionFor(h.WorkingDir)
	if err != nil {
		return err
	}

	// tmux reads its configuration when the server starts, so a server that
	// was already up when the fragment was installed has never seen it. A
	// server started by the calls below picks it up from the user's tmux.conf.
	_ = h.sessions().run("source-file", "-q", h.Fragment)

	if err := h.ensureSession(DashboardSession, h.WorkingDir, h.Dashboard); err != nil {
		return err
	}
	// The sidepanel is all Dashboard. tmux's own status line under it would
	// cost the rail a row to repeat what the rail already says — the strip
	// belongs to the Session you are working in (§2.2). Best effort: a harness
	// that could not turn it off is one row shorter, not one that cannot open.
	//
	// An option's target is a pane, where the exact-match prefix only holds up
	// with the window and pane left off after it — "=ganymede" alone is read as
	// a session of that name, and there is none. Without the prefix a repo
	// called ganymede-something would answer to it.
	_ = h.sessions().run("set", "-t", "="+DashboardSession+":", "status", "off")
	if err := h.ensureSession(working, h.WorkingDir, nil); err != nil {
		return err
	}
	if err := h.ensureDock(working); err != nil {
		return err
	}
	// Best effort, like turning off the Dashboard's own status line above: a
	// harness that could not bind the popup socket's toggle still opens, with
	// the Popup shell simply not closing again until the next Ensure.
	h.ensurePopups()
	return nil
}

// AttachCommand is what the emulator runs: the whole harness is behind it.
func (h Harness) AttachCommand() []string {
	return append([]string{"tmux"}, h.dock().args("attach", "-t", "="+DockSession)...)
}

// Attached reports whether a window is already showing the harness. Opening a
// second one would not give you a second harness: both clients would mirror the
// same dock, and tmux would shrink the window to whichever is smaller.
func (h Harness) Attached() bool {
	out, err := exec.Command("tmux", h.dock().args("list-clients", "-t", "="+DockSession, "-F", "#{client_tty}")...).Output()
	if err != nil {
		// No dock server, or no dock session: nothing is showing it.
		return false
	}
	return len(strings.Fields(string(out))) > 0
}

// ensureDock creates the frame: the sidepanel client on the left, the working
// client filling the rest.
func (h Harness) ensureDock(working string) error {
	if err := tmuxconf.WriteDockConf(h.DockConf, SidepanelWidth); err != nil {
		return err
	}

	if h.dock().run("has-session", "-t", "="+DockSession) == nil {
		// A running dock has never read the config just written, and its
		// working client still shows whichever repo it was last pointed at.
		_ = h.dock().run("source-file", "-q", h.DockConf)
		return h.pointWorkingClient(working)
	}

	// Sized generously so the sidepanel is a sensible fraction of the window
	// before a client attaches and the dock's hooks pin it exactly.
	create := append([]string{"-f", h.DockConf, "new-session", "-d", "-s", DockSession,
		"-x", "200", "-y", "50"}, h.clientCommand(DashboardSession)...)
	if err := h.dock().run(create...); err != nil {
		return fmt.Errorf("create dock: %w", err)
	}
	split := append([]string{"split-window", "-h", "-t", "=" + DockSession + ":0.0"},
		h.clientCommand(working)...)
	if err := h.dock().run(split...); err != nil {
		return fmt.Errorf("split dock: %w", err)
	}
	if err := h.dock().run("resize-pane", "-t", "="+DockSession+":0.0",
		"-x", strconv.Itoa(SidepanelWidth)); err != nil {
		return fmt.Errorf("size the sidepanel: %w", err)
	}
	return h.dock().run("select-pane", "-t", "="+DockSession+":0.1")
}

// pointWorkingClient aims the existing dock's right-hand pane at session.
// Restarting the pane's client is cheap — the Session and everything running
// in it live on the other server, untouched — and it repairs a working pane
// that has died as readily as it re-points a live one.
func (h Harness) pointWorkingClient(session string) error {
	respawn := append([]string{"respawn-pane", "-k", "-t", "=" + DockSession + ":0.1"},
		h.clientCommand(session)...)
	if h.dock().run(respawn...) == nil {
		return nil
	}

	// There is no right-hand pane to respawn — put one back.
	split := append([]string{"split-window", "-h", "-t", "=" + DockSession + ":0.0"},
		h.clientCommand(session)...)
	if err := h.dock().run(split...); err != nil {
		return fmt.Errorf("restore the working client: %w", err)
	}
	return h.dock().run("resize-pane", "-t", "="+DockSession+":0.0",
		"-x", strconv.Itoa(SidepanelWidth))
}

// clientCommand is what a dock pane runs to become a tmux client of session.
// TMUX is cleared because the pane already belongs to the dock's own server,
// and tmux refuses to nest while it is set. The command goes through an
// explicit shell: tmux execs a pane's argv directly, so a bare `env ...` would
// never see its arguments split.
func (h Harness) clientCommand(session string) []string {
	attach := append([]string{"env", "-u", "TMUX", "tmux"}, h.sessions().args("attach", "-t", "="+session)...)
	quoted := make([]string, len(attach))
	for i, arg := range attach {
		quoted[i] = shellQuote(arg)
	}
	return []string{"sh", "-c", strings.Join(quoted, " ")}
}

// shellQuote wraps an argument so the shell passes it through as one word,
// whatever a repo has been named.
func shellQuote(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// broughtUp is the repo at dir's own Session, brought up at its Main root if
// nothing is running there yet — the bring-up Open and Spawn both need before
// they can do anything to that Session's tmux session.
func (h Harness) broughtUp(dir string) (string, error) {
	name, err := h.sessionFor(dir)
	if err != nil {
		return "", err
	}
	if err := h.ensureSession(name, dir, nil); err != nil {
		return "", err
	}
	return name, nil
}

// ensureSession creates a detached session unless it already exists.
func (h Harness) ensureSession(name, dir string, command []string) error {
	if h.sessions().run("has-session", "-t", "="+name) == nil {
		return nil
	}
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	args = append(args, command...)
	if err := h.sessions().run(args...); err != nil {
		return fmt.Errorf("create session %s: %w", name, err)
	}
	return nil
}

// server addresses one tmux server.
type server struct{ socket string }

func (h Harness) sessions() server { return server{h.Socket} }
func (h Harness) dock() server     { return server{h.DockSocket} }
func (h Harness) popups() server   { return server{h.PopupSocket} }

func (s server) args(args ...string) []string {
	if s.socket == "" {
		return args
	}
	return append([]string{"-L", s.socket}, args...)
}

func (s server) run(args ...string) error {
	out, err := exec.Command("tmux", s.args(args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Default is the harness as it runs day to day: the Dashboard and the repo
// Sessions on tmux's usual server, the dock on its own.
func Default(workingDir string) (Harness, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Harness{}, fmt.Errorf("locate home directory: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return Harness{}, fmt.Errorf("locate the ganymede binary: %w", err)
	}
	layout, err := tmuxconf.DefaultLayout()
	if err != nil {
		return Harness{}, err
	}
	return Harness{
		// "default" is the name of tmux's own default socket, so this is the
		// server the user's plain `tmux` reaches — named rather than implied,
		// because the dock's panes clear $TMUX and would otherwise land on a
		// different server than these commands do.
		Socket:      "default",
		Fragment:    layout.Fragment,
		Dashboard:   []string{self, "dashboard"},
		WorkingDir:  workingDir,
		DockSocket:  "ganymede-dock",
		DockConf:    filepath.Join(config.Home(home), "ganymede", "dock.conf"),
		PopupSocket: "ganymede-popup",
		Worktree:    WorktreeCommand,
	}, nil
}
