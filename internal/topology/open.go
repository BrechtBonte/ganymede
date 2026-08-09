package topology

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/repo"
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
//
// So the repo's own Session comes first, under whatever name it ended up with,
// and only a repo that has none takes a free name. Answering with the shortest
// free name instead would rename a repo the moment the one it collided with
// went away — stranding the Session holding your shell history and everything
// running in it under a name nothing will ask for again.
func (h Harness) sessionFor(dir string) (string, error) {
	name, err := WorkingSessionName(dir)
	if err != nil {
		return "", err
	}
	running := h.runningSessions()
	root := repo.Root(dir)

	// Qualifying walks up the path a directory at a time — "api", then
	// "acme-api" — so the name stays as short as it can be while still saying
	// which repo it means. Every name this repo could ever have taken is on
	// that walk, which is what makes it the place to look for its Session.
	free := ""
	for above := filepath.Dir(repo.Absolute(dir)); ; above = filepath.Dir(above) {
		switch held, taken := running[name]; {
		case taken && held != "" && repo.Root(held) == root:
			// This repo's own Session. A Session started anywhere inside a
			// repo — `ganymede up` run in a subdirectory — belongs to that
			// repo, so the two are compared as Main roots rather than as the
			// directories they happen to have been opened at.
			return name, nil
		case !taken && free == "":
			free = name
		}
		if filepath.Dir(above) == above {
			break
		}
		name = addressable(filepath.Base(above)) + "-" + name
	}
	if free != "" {
		return free, nil
	}
	// The walk reached the filesystem root with every name on it held by some
	// other repo. Sharing a Session is worse than a name nobody can read, and
	// better than a repo the harness refuses to open at all.
	return name, nil
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
