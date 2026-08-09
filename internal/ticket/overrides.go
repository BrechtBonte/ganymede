package ticket

import (
	"errors"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// errNowhereToKeepIt is a correction made against a harness that has no state
// file to keep it in.
var errNowhereToKeepIt = errors.New("the harness has nowhere to keep a ticket")

// section is where the overrides sit in the harness's state file.
const section = "tickets"

// Checkout is the place a Session's work is happening, and the thing a ticket
// is really about: not the Session, which ends every evening, but the branch or
// the worktree it was working in, which is still there tomorrow.
type Checkout struct {
	// Root is the Main root the work belongs to.
	Root string
	// Dir is the checkout itself — the Main root, or the worktree.
	Dir string
	// Branch is the branch it is on, and empty when it is on none.
	Branch string
}

// override is one ticket set by hand, as the state file keeps it. A branch and
// a worktree are told apart on the way back in, because they are evicted by
// different questions: whether the repo still has the branch, and whether the
// directory is still there.
type override struct {
	Root     string `json:"root"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Key      Key    `json:"key"`
}

// Overrides are the tickets you set by hand — the top of the precedence, ahead
// of anything the harness can work out for itself.
//
// They are keyed by the repo and the branch or worktree rather than by the
// Session, which is what makes a correction worth making: Sessions are ended
// and restarted all day, and the thing you were correcting the harness about is
// the checkout, which outlasts all of them.
type Overrides struct {
	state config.Sidecar
	kept  []override
}

// Load reads what the last run set by hand, forgetting the overrides whose
// branch or worktree has gone since.
//
// A state file it could not read costs the corrections in it and nothing else:
// there are always overrides to come back with, empty ones, so that a sidecar
// file somebody has been editing takes down the ticket column rather than the
// Dashboard.
func Load(state config.Sidecar) (*Overrides, error) {
	overrides := &Overrides{state: state}
	if err := state.Read(section, &overrides.kept); err != nil {
		return &Overrides{state: state}, err
	}
	overrides.kept = surviving(overrides.kept)
	return overrides, nil
}

// Of is the ticket set by hand for a checkout, and whether there was one.
func (o *Overrides) Of(at Checkout) (Key, bool) {
	if o == nil {
		return "", false
	}
	want := recordOf(at)
	for _, held := range o.kept {
		if held.Root == want.Root && held.Branch == want.Branch && held.Worktree == want.Worktree {
			return held.Key, true
		}
	}
	return "", false
}

// Set records the ticket a checkout is about, or forgets the override when key
// is empty and lets the checkout speak for itself again.
//
// Whatever has gone since the harness started is dropped on the way past. The
// state file is the harness's own, this is the only moment anything rewrites
// it, and an override for a branch that was deleted a fortnight ago is not
// worth keeping to the end of time.
func (o *Overrides) Set(at Checkout, key Key) error {
	setting := recordOf(at)
	setting.Key = key

	kept := make([]override, 0, len(o.kept)+1)
	for _, held := range o.kept {
		if held.Root == setting.Root && held.Branch == setting.Branch && held.Worktree == setting.Worktree {
			continue
		}
		kept = append(kept, held)
	}
	if key != "" {
		kept = append(kept, setting)
	}
	kept = surviving(kept)

	if err := o.state.Write(section, kept); err != nil {
		return err
	}
	o.kept = kept
	return nil
}

// recordOf is how a checkout is written down: by its branch where it has one,
// and by the directory itself where it has not. A checkout with no branch is
// still a place work happens — a Main root with a PR checked out by hash, a
// directory outside any repository — and the correction you made about it is
// worth keeping until that directory goes.
func recordOf(at Checkout) override {
	if at.Branch != "" {
		return override{Root: at.Root, Branch: at.Branch}
	}
	return override{Root: at.Root, Worktree: at.Dir}
}
