package topology

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Open puts the repo at dir in front of you: the working client moves to that
// repo's Session, brought up at the Main root if nothing is running there yet.
//
// It is the repo-shaped jump. A jump reaches a Session that already exists, in
// whatever pane it happens to be running in; a repo picked out of the picker
// may have nothing running in it at all, and what you are asking for is to be
// somewhere rather than to be shown something.
func (h Harness) Open(dir string) error {
	name, err := h.sessionFor(dir)
	if err != nil {
		return err
	}
	if err := h.ensureSession(name, dir, nil); err != nil {
		return err
	}
	client, err := h.workingClient()
	if err != nil {
		return err
	}
	// The session, not a pane: what is asked for here is the repo, and which
	// window of it you were last in is the repo's business. The name is
	// addressable by construction (see WorkingSessionName) — this is a session
	// the harness named itself, unlike the ones a jump has to reach.
	if err := h.sessions().run("switch-client", "-c", client, "-t", "="+name+":"); err != nil {
		return fmt.Errorf("point the working client at %s: %w", name, err)
	}
	return nil
}

// sessionFor is the tmux session the repo at dir gets: the repo's own name, or
// one qualified by the directories above it when a different repo already
// holds that name.
//
// One session per repo, named after the repo, is what makes the harness's tmux
// sessions readable. It is also a name two repos can want: an inventory of any
// size has an "api" under more than one organisation, and the picker offers
// the whole inventory. Sending the working client to a same-named session
// rooted somewhere else would be the quietest kind of wrong — you would be in
// a repo, at a prompt, with no sign it was the other one.
func (h Harness) sessionFor(dir string) (string, error) {
	name, err := WorkingSessionName(dir)
	if err != nil {
		return "", err
	}
	// Qualifying walks up the path a directory at a time — "api", then
	// "acme-api" — so the name stays as short as it can be while still saying
	// which repo it means.
	running := h.runningSessions()
	for above := filepath.Dir(absolute(dir)); ; above = filepath.Dir(above) {
		if by, taken := running[name]; !taken || sameDir(by, dir) {
			return name, nil
		}
		if filepath.Dir(above) == above {
			// The walk has reached the filesystem root: the whole path is in
			// the name and something still answers to it. Sharing a Session is
			// worse than a name nobody can read, and better than a repo the
			// harness refuses to open at all.
			return name, nil
		}
		name = addressable(filepath.Base(above)) + "-" + name
	}
}

// runningSessions maps every tmux session on the Sessions server to the
// directory it was started in.
//
// Listing is what answers the question rather than asking after each candidate
// name: a name is only a candidate because something may already hold it, and
// tmux answers a display-message about a session that is not there with an
// empty line and no error at all. It is also one call instead of one per
// candidate. A server that is not running yet holds no names.
func (h Harness) runningSessions() map[string]string {
	out, err := exec.Command("tmux", h.sessions().args("list-sessions",
		"-F", "#{session_name}\t#{session_path}")...).Output()
	if err != nil {
		return nil
	}

	running := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, dir, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if !ok || name == "" {
			continue
		}
		running[name] = dir
	}
	return running
}

// sameDir reports whether two paths name the same directory, following the
// symlinks on the way to each: tmux reports a session's path as it was given,
// and the harness is given repo names it has resolved.
func sameDir(a, b string) bool {
	return a != "" && absolute(a) == absolute(b)
}

// absolute is the one name for a directory. A directory that is no longer
// there cannot be resolved and is taken as written.
func absolute(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}
