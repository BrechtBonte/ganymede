package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// stateFile is the harness's own state, kept beside its configuration.
const stateFile = "state.json"

// Sidecar is the harness's state file: one JSON object whose keys each belong
// to a different part of the harness — the manual ticket overrides, the root
// claims and their notes, what the popup shells own.
//
// It is read and written a section at a time, and everything the reader did not
// ask about is carried through untouched. That is what lets one part of the
// harness keep state without knowing the whole file's shape, and it is what
// keeps a harness rolled back to an older build from throwing away the sections
// only the newer one wrote.
type Sidecar struct {
	// Path is the state file. Empty is not a Sidecar; use DefaultSidecar.
	Path string
}

// DefaultSidecar is the state file where the harness keeps it.
func DefaultSidecar() (Sidecar, error) {
	dir, err := Dir()
	if err != nil {
		return Sidecar{}, err
	}
	return Sidecar{Path: filepath.Join(dir, stateFile)}, nil
}

// Read unmarshals one section into value.
//
// A harness that has never written anything has no state file, and a part of it
// that has never written anything has no section — neither is a failure, and
// both leave value exactly as the caller had it. A state file that will not
// parse is a different matter: it is the harness's own, and something is in it.
func (s Sidecar) Read(section string, value any) error {
	sections, err := s.sections()
	if err != nil {
		return err
	}
	body, held := sections[section]
	if !held {
		return nil
	}
	if err := json.Unmarshal(body, value); err != nil {
		return fmt.Errorf("read the %s in %s: %w", section, s.Path, err)
	}
	return nil
}

// Write replaces one section, leaving the rest of the file as it was.
//
// The one thing it cannot carry through is a file it could not parse, which it
// refuses to overwrite: a state file damaged by a half-finished write or a hand
// edit still has everything in it, and the harness replacing it with one good
// section would be the moment that stopped being true.
func (s Sidecar) Write(section string, value any) error {
	sections, err := s.sections()
	if err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("write the %s for %s: %w", section, s.Path, err)
	}
	sections[section] = body

	// Indented, because this is a file somebody may well end up reading — and,
	// once in a blue moon, correcting by hand.
	whole, err := json.MarshalIndent(sections, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", s.Path, err)
	}
	return Replace(s.Path, append(whole, '\n'))
}

// sections is the state file read as what it is: a set of keys this harness
// only partly recognises.
func (s Sidecar) sections() (map[string]json.RawMessage, error) {
	body, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.Path, err)
	}
	sections := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &sections); err != nil {
		return nil, fmt.Errorf("read %s: %w", s.Path, err)
	}
	return sections, nil
}
