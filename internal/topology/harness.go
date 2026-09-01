package topology

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	// WatchFor is how long SpawnWatch waits for a spawned session to die on
	// startup, and WatchEvery how often it looks. Zero means the defaults in
	// spawn.go — a test shortens both rather than sitting out the real half
	// minute.
	WatchFor, WatchEvery time.Duration
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
	return append([]string{"tmux"}, h.dock().client("attach", "-t", "="+DockSession)...)
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
		return h.reattachClients(working)
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
	return h.Focus()
}

// reattachClients puts the existing dock back to its two panes: the sidepanel
// on the Dashboard, the working client on session.
//
// Both panes are respawned every time rather than inspected first, because a
// dock pane is only a client — the Dashboard and every repo Session live on
// the other server, untouched by this. That makes one call the repair for all
// of it: a pane pointed at the repo you last came from, a pane whose client
// died, and a pane that was already right cost the same.
//
// Which matters most for the sidepanel, because ctrl+c quits the Dashboard by
// design: its Session ends, the pane's client exits with it, and tmux closes
// the pane and renumbers the working client down into index 0. Aiming a
// client at :0.1 by position alone is how a restart used to make that worse —
// the respawn found no pane there, and the fallback split a second working
// client into the slot the Dashboard had left.
func (h Harness) reattachClients(working string) error {
	panes, err := h.dockPanes()
	if err != nil {
		return err
	}

	// A pane past the two the dock is — one an earlier repair left behind, or
	// one split by hand — cannot be told from the sidepanel by position, so
	// it goes before anything is aimed at a pane by position.
	for _, extra := range panes[min(len(panes), 2):] {
		if err := h.dock().run("kill-pane", "-t", extra); err != nil {
			return fmt.Errorf("clear an extra dock pane: %w", err)
		}
	}
	if len(panes) < 2 {
		// One pane left, so a client has died and taken its pane with it.
		// Split the survivor to get the pair back; which of the two it
		// becomes does not matter, since both are respawned below.
		if err := h.dock().run("split-window", "-h", "-t", panes[0]); err != nil {
			return fmt.Errorf("restore the dock's second pane: %w", err)
		}
	}

	for _, aim := range []struct{ pane, session string }{
		{"0.0", DashboardSession},
		{"0.1", working},
	} {
		respawn := append([]string{"respawn-pane", "-k", "-t", "=" + DockSession + ":" + aim.pane},
			h.clientCommand(aim.session)...)
		if err := h.dock().run(respawn...); err != nil {
			return fmt.Errorf("point the dock's %s pane at %s: %w", aim.pane, aim.session, err)
		}
	}
	return h.dock().run("resize-pane", "-t", "="+DockSession+":0.0",
		"-x", strconv.Itoa(SidepanelWidth))
}

// dockPanes is the dock window's panes, by id, in the order they sit across
// the window — the sidepanel's first.
func (h Harness) dockPanes() ([]string, error) {
	out, err := h.dock().output("list-panes", "-t", "="+DockSession+":0", "-F", "#{pane_id}")
	if err != nil {
		return nil, fmt.Errorf("list the dock's panes: %w", err)
	}
	panes := strings.Fields(out)
	if len(panes) == 0 {
		return nil, fmt.Errorf("the dock has no panes to point at %s", DashboardSession)
	}
	return panes, nil
}

// clientCommand is what a dock pane runs to become a tmux client of session.
// TMUX is cleared because the pane already belongs to the dock's own server,
// and tmux refuses to nest while it is set. The command goes through an
// explicit shell: tmux execs a pane's argv directly, so a bare `env ...` would
// never see its arguments split.
func (h Harness) clientCommand(session string) []string {
	attach := append([]string{"env", "-u", "TMUX", "tmux"}, h.sessions().client("attach", "-t", "="+session)...)
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

// client is args for the command a tmux client attaches with. The -u is what
// keeps the drawing independent of the environment ganymede was launched from:
// tmux reads LC_ALL, LC_CTYPE and LANG to decide whether the terminal a client
// draws to takes UTF-8, and writes "_" in place of every UTF-8 character on a
// client it decides against — while LaunchServices, which is what starts
// Ganymede.app from the Dock, names none of the three. Every terminal these
// clients ever draw to is Ghostty.
func (s server) client(args ...string) []string {
	return s.args(append([]string{"-u"}, args...)...)
}

func (s server) run(args ...string) error {
	out, err := exec.Command("tmux", s.args(args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// output is run for the commands asked a question rather than told to do
// something: tmux's answer on stdout, with its complaint left on stderr where
// it cannot be mistaken for one.
func (s server) output(args ...string) (string, error) {
	out, err := exec.Command("tmux", s.args(args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
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

// Docked reports whether this process is the harness's own Dashboard — the
// one running in the Dashboard Session, which the dock's sidepanel attaches a
// client to — rather than one you started by hand in a terminal of your own.
//
// It asks where the process actually is rather than being told by a flag,
// because the Dashboard is started from more than one place: Ensure creates
// the Session, and refresh.sh respawns its pane. A flag would have to be
// repeated at each of them, and whichever one forgot it would be a Dashboard
// that quits on the key it is meant to ignore.
func (h Harness) Docked() bool {
	// tmux sets TMUX_PANE for every process it starts, so its absence is a
	// Dashboard running outside tmux altogether.
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return false
	}
	out, err := h.sessions().output("display-message", "-p", "-t", pane, "#{session_name}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == DashboardSession
}
