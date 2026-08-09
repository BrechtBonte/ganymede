// Package sidecar is the harness's own state: the part of what the Dashboard
// shows that nothing else can tell it, and that has to survive a restart.
//
// Today that is one thing — when you were last working in each repo, which is
// what keeps a repo in the working set after its Sessions have ended. The file
// is the harness's to grow, though: root claims, ticket overrides and popup
// ownership all belong here in time. So it is read the way a format that will
// be added to has to be read — whole, keeping every field it does not
// recognise — and a harness one version behind can write it back without
// throwing away what a later one put there.
package sidecar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// granularity is how finely activity is recorded. The recency window is
// measured in days, the working set is rebuilt every time any Session moves,
// and every rebuild stamps every repo that has one — so a stamp is rounded
// until it is worth a write, and a file whose contents have not changed is
// not written at all.
const granularity = time.Minute

// State is the harness state file.
type State struct {
	// Path is the file. It is only written, never watched: the harness that
	// has it open is the only one writing it.
	Path string
	// body is the file as it was read, so that fields this version does not
	// read survive being written back.
	body map[string]json.RawMessage
	// repos is each repo's entry, kept whole for the same reason.
	repos map[string]map[string]json.RawMessage
	// active is when each repo was last worked in, rounded to granularity.
	active map[string]time.Time
}

// lastActive is the field holding a repo's last harness activity.
const lastActive = "lastActive"

// reposField is the object holding one entry per repo.
const reposField = "repos"

// Default is the harness state where the harness keeps it, beside the rest of
// its configuration.
func Default() (*State, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home directory: %w", err)
	}
	return Load(filepath.Join(config.Home(home), "ganymede", "state.json"))
}

// Load reads the harness state at path.
//
// A file that is not there is a harness that has not been used yet, and a file
// that cannot be read is one somebody has hand-edited into nonsense. Neither
// is worth refusing to open the Dashboard over: everything in here is the
// harness's memory of what you have been doing, and the worst losing it costs
// is a repo dropping out of the working set earlier than it should.
func Load(path string) (*State, error) {
	state := &State{Path: path, body: map[string]json.RawMessage{}, repos: map[string]map[string]json.RawMessage{}, active: map[string]time.Time{}}

	raw, err := os.ReadFile(path)
	if err != nil {
		return state, nil
	}
	if err := json.Unmarshal(raw, &state.body); err != nil {
		return state, nil
	}
	if err := json.Unmarshal(state.body[reposField], &state.repos); err != nil {
		state.repos = map[string]map[string]json.RawMessage{}
		return state, nil
	}
	for root, entry := range state.repos {
		var stamp time.Time
		if err := json.Unmarshal(entry[lastActive], &stamp); err == nil {
			state.active[root] = stamp
		}
	}
	return state, nil
}

// Active is when each repo was last worked in. The map is the caller's own:
// the sidecar is the one writer of this file, and a caller holding its map
// would be a second.
func (s *State) Active() map[string]time.Time {
	active := make(map[string]time.Time, len(s.active))
	for root, at := range s.active {
		active[root] = at
	}
	return active
}

// Touch records harness activity in root at at — you jumped to a Session in
// it, or asked the picker to take you there. Only the latest stamp is kept: an
// older one arriving late must never drag a repo back towards falling off the
// working set.
func (s *State) Touch(root string, at time.Time) {
	at = at.UTC().Truncate(granularity)
	if held, known := s.active[root]; known && !at.After(held) {
		return
	}
	s.active[root] = at
}

// Save writes the state back.
//
// It is safe to call on every redraw. The file is replaced only when what it
// says has actually changed — stamps are rounded before they go in, and a
// replacement identical to what is already there is not written — so the
// Dashboard can call this every time it stamps a repo without touching the
// disk every time a Session moves.
func (s *State) Save() error {
	if len(s.active) == 0 && len(s.body) == 0 {
		// Nothing recorded and nothing read: a harness that has done nothing
		// yet has no reason to leave a file behind.
		return nil
	}

	for root, at := range s.active {
		entry := s.repos[root]
		if entry == nil {
			entry = map[string]json.RawMessage{}
			s.repos[root] = entry
		}
		stamp, err := json.Marshal(at)
		if err != nil {
			return fmt.Errorf("write the activity for %s: %w", root, err)
		}
		entry[lastActive] = stamp
	}

	repos, err := json.Marshal(s.repos)
	if err != nil {
		return fmt.Errorf("write the harness state: %w", err)
	}
	s.body[reposField] = repos

	// Indented, because this is a file you may well open to find out what the
	// harness thinks you have been doing.
	body, err := json.MarshalIndent(s.body, "", "  ")
	if err != nil {
		return fmt.Errorf("write the harness state: %w", err)
	}
	if err := config.Replace(s.Path, append(body, '\n')); err != nil {
		return fmt.Errorf("save the harness state: %w", err)
	}
	return nil
}
