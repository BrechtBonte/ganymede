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
	"errors"
	"fmt"
	"io/fs"
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
	// moved says something has been recorded that the file does not say yet,
	// so that Save on every redraw costs nothing on the redraws where nothing
	// has happened.
	moved bool
	// unreadable says the file was there and could not be understood. It is
	// the one state Save must not write over: what it holds is the harness's
	// memory of decisions you made — and will hold root claims and their notes
	// — so a file somebody has hand-edited into nonsense is theirs to fix, not
	// the harness's to quietly replace with an empty one.
	unreadable bool
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
// It always hands back a state the harness can go on using, and says whether
// that state is the file's. A file that is not there is a harness that has not
// been used yet and is no kind of failure. A file that is there and cannot be
// read is: the Dashboard opens on an empty memory of where you have been, you
// are told why, and Save leaves the file alone until somebody has looked at
// it. Refusing to open at all would be the harness holding your day to ransom
// over a file it only wanted to keep notes in; overwriting it would be the
// harness throwing your notes away.
func Load(path string) (*State, error) {
	state := &State{Path: path, body: map[string]json.RawMessage{}, repos: map[string]map[string]json.RawMessage{}, active: map[string]time.Time{}}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		state.unreadable = true
		return state, fmt.Errorf("read the harness state at %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &state.body); err != nil {
		state.body, state.unreadable = map[string]json.RawMessage{}, true
		return state, fmt.Errorf("the harness state at %s is not readable JSON: %w", path, err)
	}
	// An absent repos field is a state file that has simply never recorded
	// one; a malformed one is the same problem as a malformed file.
	if raw, recorded := state.body[reposField]; recorded {
		if err := json.Unmarshal(raw, &state.repos); err != nil {
			state.repos, state.unreadable = map[string]map[string]json.RawMessage{}, true
			return state, fmt.Errorf("the repos in the harness state at %s cannot be read: %w", path, err)
		}
	}
	for root, entry := range state.repos {
		// One entry the harness cannot read costs that repo its place in the
		// working set and nothing else — the entry itself is kept whole and
		// written back as it was.
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
	s.active[root], s.moved = at, true
}

// Save writes the state back.
//
// It is safe to call on every redraw. Nothing is read or written unless a
// stamp has actually moved — they are rounded before they go in, so the
// Dashboard stamping every repo with a Session in it, every time any Session
// moves, comes to one write a minute at worst.
//
// A file that could not be read is refused rather than replaced. That leaves
// the working set without its memory until somebody fixes the file, which is
// the smaller loss: the alternative is the harness deleting what it could not
// understand.
func (s *State) Save() error {
	if s.unreadable {
		return fmt.Errorf("the harness state at %s could not be read, so it will not be written over", s.Path)
	}
	if !s.moved {
		// Nothing has happened since the file last said what it says — which,
		// on a harness that has done nothing at all, means no file either.
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
	s.moved = false
	return nil
}
