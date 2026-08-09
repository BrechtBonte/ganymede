package workingset

import (
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// section is where the per-repo activity sits in the harness's state file.
const section = "activity"

// granularity is how finely activity is recorded. The window is measured in
// days, the tree is rebuilt every time any Session moves, and every rebuild
// stamps every repo that has one — so a stamp is rounded until it is worth
// writing, and one that lands in the same minute as the last costs nothing.
const granularity = time.Minute

// Activity is when you were last working in each repo.
//
// It is the only part of the working set the harness has to remember for
// itself. A live Session says where you are working now and a Claim says where
// you have reserved; where you were working yesterday is a thing only the
// harness watched, so it goes in the state file — and a Dashboard that forgot
// it on restart would have no working set worth the name, only a list of
// what happens to be running.
type Activity struct {
	state config.Sidecar
	// active is the stamps as the file has them, rounded to granularity.
	active map[string]time.Time
}

// Load reads when each repo was last worked in.
//
// A state file it could not read costs the harness its memory of where you
// have been and nothing else: there are always stamps to come back with, empty
// ones, so that a sidecar somebody has been editing costs you the quiet repos
// on the Dashboard rather than the Dashboard.
func Load(state config.Sidecar) (*Activity, error) {
	activity := &Activity{state: state, active: map[string]time.Time{}}
	if err := state.Read(section, &activity.active); err != nil {
		return &Activity{state: state, active: map[string]time.Time{}}, err
	}
	return activity, nil
}

// Active is when each repo was last worked in. The map is the caller's own:
// this is the one writer of the section, and a caller holding its map would be
// a second.
func (a *Activity) Active() map[string]time.Time {
	if a == nil {
		return nil
	}
	active := make(map[string]time.Time, len(a.active))
	for root, at := range a.active {
		active[root] = at
	}
	return active
}

// Touch records harness activity in root at at — a Session running in it, or
// the picker taking you there.
//
// Only the latest stamp is kept: an older one arriving late must never drag a
// repo back towards falling off the working set. Nothing is written when the
// stamp lands in the minute the file already says, which is what makes this
// safe to call on every redraw.
func (a *Activity) Touch(root string, at time.Time) error {
	at = at.UTC().Truncate(granularity)
	if held, known := a.active[root]; known && !at.After(held) {
		return nil
	}

	// Written before it is held, so that a section that could not be saved is
	// tried again on the next stamp rather than believed.
	moved := make(map[string]time.Time, len(a.active)+1)
	for held, when := range a.active {
		moved[held] = when
	}
	moved[root] = at
	if err := a.state.Write(section, moved); err != nil {
		return err
	}
	a.active = moved
	return nil
}
