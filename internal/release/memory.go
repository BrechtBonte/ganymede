package release

import (
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// section is where the last check sits in the harness's state file.
const section = "release"

// Remembered is the last check that was made, kept across restarts.
//
// Only the half of the check that costs something is kept. What is installed
// is read again every time, because it moves on its own — Claude Code updates
// itself whenever a Session starts, and a Dashboard reading both halves out of
// this file would go on saying you were behind for hours after you had caught
// up.
type Remembered struct {
	// CheckedAt is when the bucket was last asked. A zero time is a harness
	// that has never asked.
	CheckedAt time.Time `json:"checkedAt"`
	// Channel is the auto-update channel that check was made against.
	Channel string `json:"channel"`
	// Latest is the version that channel was publishing.
	Latest string `json:"latest"`
}

// Memory is the last check, in the harness's state file.
type Memory struct {
	state      config.Sidecar
	remembered Remembered
}

// Load reads the last check that was made.
//
// A state file it could not read costs the harness its memory of the last
// check and nothing else: a Memory is always come back with, an empty one, so
// that a sidecar somebody has been editing costs a check made sooner than it
// had to be rather than the Dashboard.
func Load(state config.Sidecar) (*Memory, error) {
	memory := &Memory{state: state}
	if err := state.Read(section, &memory.remembered); err != nil {
		return &Memory{state: state}, err
	}
	return memory, nil
}

// Remembered is the last check. A nil Memory has none, which is a harness
// keeping no state rather than one that has never checked — both check now.
func (m *Memory) Remembered() Remembered {
	if m == nil {
		return Remembered{}
	}
	return m.remembered
}

// Remember writes down a check that was made.
func (m *Memory) Remember(check Remembered) error {
	if m == nil {
		return nil
	}
	// Written before it is held, so that a section that could not be saved is
	// tried again on the next check rather than believed.
	if err := m.state.Write(section, check); err != nil {
		return err
	}
	m.remembered = check
	return nil
}
