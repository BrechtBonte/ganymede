package topology

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
)

// popupWidth and popupHeight are the Popup shell's fixed proportions (§8):
// centred, roughly three quarters of the window in both directions.
const popupWidth, popupHeight = "75%", "75%"

// keepalivePopupSession is a session ensurePopups starts and never kills,
// purely so the popup socket has a server to bind against. tmux only starts
// a server for a command that creates something — bind on a socket with no
// session yet just errors — so without this, the very first Ensure a
// machine ever runs (or the first after any reboot, since a tmux server
// never survives one) would bind nothing, and the server OpenPopup starts
// later, lazily, would come up with no close-toggle bound on it at all.
//
// Its name deliberately does not start with popup.OwnerName's own prefix
// (see popup.IsOwnerName), so a sweep never mistakes it for a real popup
// and kills it out from under the binding.
const keepalivePopupSession = "ganymede-keepalive"

// ensurePopups binds the toggle at the popup socket's own root table to
// close whatever popup currently has focus there. It belongs on this server
// rather than the Sessions server's: once a popup has taken the pane over,
// every key you press reaches its own nested client first (see
// OpenPopup), and closing has to be that server's business alone — a Go
// process running for as long as a popup stays open would be one popup
// away from a harness that cannot close.
//
// detach-client does the closing itself: OpenPopup's -E command is an
// attach to this same session, so detaching it is what makes display-popup
// on the Sessions server return, and the session it was attached to — with
// everything running in it — is left exactly as it was.
func (h Harness) ensurePopups() {
	_ = h.popups().run("new-session", "-d", "-s", keepalivePopupSession)
	_ = h.popups().run("bind", "-n", tmuxconf.PopupToggleKey, "detach-client")
	_ = h.popups().run("bind", "-n", tmuxconf.PopupToggleFallbackKey, "detach-client")
}

// OpenPopup shows the Popup shell over pane, opened at dir (§8): a hidden
// tmux session of its own on the popup socket, created the first time and
// reattached to — with its scrollback, history and anything still running —
// every time after.
//
// It blocks for as long as the overlay is on screen, because that is what
// tmux's own display-popup does: the command line that shows a popup does
// not return until the popup closes. Callers that must not block the
// caller of theirs — the root-table toggle, in production — run it in a
// process of its own instead of asking it not to.
func (h Harness) OpenPopup(dir, pane string) error {
	owner := popup.OwnerName(dir)
	attach := append([]string{"env", "-u", "TMUX", "tmux"},
		h.popups().args("new-session", "-A", "-s", owner, "-c", dir)...)
	quoted := make([]string, len(attach))
	for i, arg := range attach {
		quoted[i] = shellQuote(arg)
	}
	if err := h.sessions().run("display-popup", "-t", pane, "-d", dir,
		"-w", popupWidth, "-h", popupHeight, "-E", strings.Join(quoted, " ")); err != nil {
		return fmt.Errorf("open the popup shell: %w", err)
	}
	return nil
}

// SelectedDir is the directory the Dashboard's cursor is currently on, which
// is where a popup opens when the toggle is pressed with focus on the rail
// rather than a Session's own pane (see popup.TargetDir). Best effort: a
// Dashboard that has never written it, or a Sessions server that cannot be
// asked, both read as nothing selected yet.
func (h Harness) SelectedDir() string {
	out, err := exec.Command("tmux", h.sessions().args("show-options", "-g", "-v", tmuxconf.PopupDirOption)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Selected records which directory the Dashboard's cursor is on, for
// SelectedDir to answer with later.
func (h Harness) Selected(dir string) error {
	if err := h.sessions().run("set", "-g", tmuxconf.PopupDirOption, dir); err != nil {
		return fmt.Errorf("record the selected repo for the popup shell: %w", err)
	}
	return nil
}

// Sweep kills every hidden popup whose directory has no live Session left in
// it — Gone, per CONTEXT.md — and reports what survives: a hidden popup
// still running a command in any of its panes is what earns its owner's row
// a busy marker (§8). Only the toggle is rebound on the popup socket (see
// ensurePopups) — the ordinary tmux prefix is left live inside a popup, so
// nothing stops a second window or a split being opened there, and a sweep
// that only ever looked at the first pane would miss a command running in
// one of the others.
//
// One list-panes call covers every popup at once, asked fresh rather than
// from a list kept of its own: the socket is the only place any of this
// survives a Dashboard restart, and a remembered list could disagree with
// it. Owners are compared by name rather than by reversing OwnerName's
// hash, which cannot be reversed at all — every live directory is hashed
// the same way instead, so the two sides speak the same language without
// either being able to read the other's — and popup.IsOwnerName is what
// keeps a session that is not one of ours, on the same socket for whatever
// reason, from being swept at all.
func (h Harness) Sweep(liveDirs []string) (map[string]popup.Status, error) {
	live := make(map[string]bool, len(liveDirs))
	for _, dir := range liveDirs {
		live[popup.OwnerName(dir)] = true
	}

	panes, err := h.popupPanes()
	if err != nil {
		return nil, err
	}

	// dirs is the directory each owner's row opens in — its first window's
	// first pane, the one OpenPopup always creates — and busy is whichever
	// of an owner's panes, if any, is running more than its own prompt.
	dirs := map[string]string{}
	busy := map[string]popup.Status{}
	killed := map[string]bool{}
	for _, p := range panes {
		if !popup.IsOwnerName(p.session) {
			continue
		}
		if !live[p.session] {
			if !killed[p.session] {
				_ = h.popups().run("kill-session", "-t", "="+p.session)
				killed[p.session] = true
			}
			continue
		}
		if p.window == "0" && p.pane == "0" {
			dirs[p.session] = p.dir
		}
		if p.command != "" && !isShell(p.command) {
			busy[p.session] = popup.Status{Command: p.command, Busy: true}
		}
	}

	statuses := make(map[string]popup.Status, len(dirs))
	for owner, dir := range dirs {
		statuses[dir] = busy[owner]
	}
	return statuses, nil
}

// popupPane is one pane on the popup socket, as list-panes reports it.
type popupPane struct {
	session, window, pane, dir, command string
}

// popupPanes is every pane currently on the popup socket. A server that has
// never come up yet — before the first Ensure, in a test that built a
// Harness by hand — holds none, which is not a failure worth reporting;
// anything else tmux says about the attempt is, since a sweep that cannot
// tell "empty" from "the socket could not be asked" would report every open
// popup as gone.
func (h Harness) popupPanes() ([]popupPane, error) {
	out, err := exec.Command("tmux", h.popups().args("list-panes", "-a",
		"-F", "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_path}\t#{pane_current_command}")...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No such file or directory") {
			return nil, nil
		}
		return nil, fmt.Errorf("list the popup panes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	panes := make([]popupPane, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 {
			continue
		}
		panes = append(panes, popupPane{session: fields[0], window: fields[1], pane: fields[2], dir: fields[3], command: fields[4]})
	}
	return panes, nil
}

// isShell says whether command is a plain shell sitting at its prompt rather
// than something the popup is busy running. A pane's own default-shell
// option is not asked instead: it names the executable new-session was
// given, which can differ in spelling — a login shell, a symlinked one — from
// what the process table actually reports running.
func isShell(command string) bool {
	switch command {
	case "sh", "bash", "zsh", "dash", "ksh", "fish", "tcsh", "csh", "nu", "elvish", "xonsh", "pwsh", "powershell":
		return true
	}
	return false
}
