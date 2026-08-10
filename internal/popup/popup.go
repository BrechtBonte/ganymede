// Package popup is the domain of the Popup shell (CONTEXT.md, SPEC.md §8):
// the toggleable overlay shell belonging to the session or window in focus.
//
// Nothing in here runs tmux. It is the small vocabulary topology.Harness
// orchestrates tmux around — which hidden session an owner's popup lives in,
// and which directory a popup opens in — kept apart from the tmux mechanics
// the same way session.State is kept apart from the registry that reads it.
package popup

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Status is what a hidden popup shell is doing when the Dashboard asks: the
// command running in it, and whether that is more than sitting at its prompt.
// A hidden popup with a running command is what earns a busy marker on its
// owner's row (§8).
type Status struct {
	// Command is the popup's foreground process, empty when it is idle at its
	// prompt.
	Command string
	// Busy says the command is more than the prompt itself.
	Busy bool
}

// prefix names every hidden session this harness owns, so a sweep of the
// popup socket can never mistake a session of the user's own for one of ours.
const prefix = "ganymede-popup-"

// OwnerName is the hidden tmux session a directory's popup shell lives in:
// stable across toggles, so hiding and reopening lands on the same session
// and everything it was carrying — scrollback, history, a running command —
// is still there.
//
// It is a hash rather than the directory itself because a tmux session name
// can hold neither "." nor ":" and stay addressable (see
// topology.WorkingSessionName), and a path is under no obligation to avoid
// either.
//
// dir is resolved before it is hashed, because the two things that ever
// supply one do not agree on spelling: tmux reports a pane's path with every
// symlink followed, while a directory read off the registry or the
// inventory scan may still be carrying one. A repo whose root — or whose
// machine's own temp or home prefix — sits behind a symlink would otherwise
// answer to two different owners depending on which route found it.
func OwnerName(dir string) string {
	sum := sha256.Sum256([]byte(resolved(dir)))
	return prefix + hex.EncodeToString(sum[:])[:16]
}

// resolved is dir with every symlink followed, and dir itself when the OS
// cannot say — already gone, or never touched by one — which is the only
// sensible answer for a directory a live pane's own cwd already proves
// exists.
func resolved(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

// IsOwnerName reports whether name is a hidden session OwnerName could have
// produced, which is how a sweep of the popup socket tells its own sessions
// apart from anything else that might end up sharing it.
func IsOwnerName(name string) bool {
	return strings.HasPrefix(name, prefix)
}

// TargetDir decides which directory a popup opens in (§8): the pane it was
// pressed from, or — when that pane is the Dashboard's own, which has no
// checkout of its own to open a shell in — whichever repo the Dashboard has
// selected. paneSession is the tmux session the toggle was pressed in, and
// dashboardSession is the name reserved for the Dashboard's own
// (topology.DashboardSession); the two are compared here, rather than a bare
// bool decided by the caller, so a caller that forgets which session that is
// cannot get this wrong silently.
//
// A Dashboard that has not said anything yet — no Session has ever been
// selected, or a state file it could not write to — leaves selectedDir
// empty, and an empty directory is not a place a shell can open: the
// Dashboard's own pane is what is left to fall back to.
func TargetDir(paneSession, dashboardSession, paneDir, selectedDir string) string {
	if paneSession != dashboardSession {
		return paneDir
	}
	if selectedDir == "" {
		return paneDir
	}
	return selectedDir
}
