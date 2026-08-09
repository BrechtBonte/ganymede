package ticket

import (
	"errors"
	"path/filepath"

	"github.com/BrechtBonte/ganymede/internal/browser"
	"github.com/BrechtBonte/ganymede/internal/repo"
)

// errNoTicket is a Session about no ticket being asked to show it: there is no
// page at the browse address that no ticket makes.
var errNoTicket = errors.New("this Session is about no ticket")

// Tickets is which ticket each Session's work is about, and where a correction
// goes when you make one.
//
// The precedence is the whole of it: what you set by hand, then the branch, then
// the worktree's directory name, then nothing. Everything but the first is read
// off names the work already had — which is why a Session spawned from the
// Dashboard needs nothing extra to arrive with its ticket on it.
type Tickets struct {
	// Overrides are the tickets set by hand. A nil Overrides is a harness with
	// none, which still reads every branch it is shown.
	Overrides *Overrides
	// Branch reads the branch a checkout is on. Nil means ask git.
	Branch func(dir string) string
	// Browser is where a ticket is read. Nil means the desktop's.
	Browser Browser
}

// Browser shows a link.
type Browser interface {
	Open(url string) error
}

// Open shows the ticket in the browser, which is where everything this harness
// deliberately does not know about it lives.
func (t *Tickets) Open(key Key) error {
	if key == "" {
		return errNoTicket
	}
	show := t.Browser
	if show == nil {
		show = browser.Browser{}
	}
	return show.Open(key.URL())
}

// Of is the ticket the Session working in dir under Main root root is about,
// and the empty Key when it is about none — which the Dashboard says out loud
// rather than filling in.
func (t *Tickets) Of(dir, root string) Key {
	at := t.at(dir, root)
	if set, ok := t.Overrides.Of(at); ok {
		return set
	}
	if named, ok := In(at.Branch); ok {
		return named
	}
	// Only a worktree's directory speaks. A Main root is named after the repo,
	// and repos are named by people who were not thinking about this.
	if at.Dir != at.Root {
		if named, ok := In(filepath.Base(at.Dir)); ok {
			return named
		}
	}
	return ""
}

// Set records the ticket the Session working in dir is about, or clears the
// correction when key is empty and gives the branch the row back.
func (t *Tickets) Set(dir, root string, key Key) error {
	if t.Overrides == nil {
		return errNowhereToKeepIt
	}
	return t.Overrides.Set(t.at(dir, root), key)
}

// at is the checkout a Session working in dir belongs to.
//
// The two paths are compared as the filesystem really has them, because they
// arrive from different places: the root is git's own answer, while the
// directory is whatever Claude Code wrote in the registry. On a machine where
// /var is a link to /private/var, the same directory reaches here spelled two
// ways, and a Main root that failed to recognise itself would start reading the
// repo's own name for a ticket.
func (t *Tickets) at(dir, root string) Checkout {
	branch := t.Branch
	if branch == nil {
		branch = repo.Branch
	}
	return Checkout{Root: root, Dir: sameAs(dir, root), Branch: branch(dir)}
}

// sameAs is dir written the way root is, when they are the same directory.
func sameAs(dir, root string) string {
	if dir == root {
		return root
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}
