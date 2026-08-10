// Package claim is the Main root Claim (CONTEXT.md, SPEC.md §4.2): a root
// explicitly reserved by you rather than held by an agent — typically for a
// PR review — released explicitly once you are done with it.
//
// A Claim is kept by root alone, unlike a ticket override: a repository's
// Main root is one directory for as long as the repository exists, so there
// is no branch or worktree to key it by and no eviction question to ask of
// git. It survives a restart the same way the ticket overrides and the
// working set's own activity do, in the harness's own state file.
package claim

import (
	"errors"
	"sync"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// section is where Claims sit in the harness's state file.
const section = "claims"

// errNowhereToKeepIt is a Claim made against a harness that has no state
// file to keep it in — the same word ticket.Set gets from a nil Overrides.
var errNowhereToKeepIt = errors.New("no root claiming is configured")

// Claims is which Main roots you have reserved, and the note each was
// claimed with.
//
// A Takeover claims a root from the guarded End's own goroutine (§7.2's "the
// send runs off the main loop"), while every other caller — the Claim
// dialog, a release, a rebuild reading Claimed — runs on the bubbletea main
// loop. mu is what keeps kept one map either of them can trust, rather than
// two goroutines racing the same read-copy-write.
type Claims struct {
	state config.Sidecar
	mu    sync.Mutex
	kept  map[string]string
}

// Load reads what the last run had claimed.
//
// A state file it could not read costs the Claims in it and nothing else:
// there are always Claims to come back with, empty ones, so that a sidecar
// somebody has been editing costs you the roots you had reserved rather than
// the Dashboard.
func Load(state config.Sidecar) (*Claims, error) {
	claims := &Claims{state: state, kept: map[string]string{}}
	if err := state.Read(section, &claims.kept); err != nil {
		return &Claims{state: state, kept: map[string]string{}}, err
	}
	return claims, nil
}

// Claimed is every root reserved right now, by root, with the note it was
// claimed with — empty when it was claimed with none. The map is the
// caller's own: this is the one writer of the section, and a caller holding
// its map would be a second.
func (c *Claims) Claimed() map[string]string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make(map[string]string, len(c.kept))
	for root, note := range c.kept {
		kept[root] = note
	}
	return kept
}

// NoteOf is the note root was claimed with, and whether it is claimed at
// all. A nil Claims is a harness with none, which reports every root
// unclaimed rather than panicking over a Dashboard that has not wired one
// up.
func (c *Claims) NoteOf(root string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	note, held := c.kept[root]
	return note, held
}

// Claim reserves root, with note when there is one. Claiming again over your
// own Claim corrects the note rather than stacking a second one.
//
// A nil Claims refuses rather than panics — the same failing-soft its own
// read methods already promise, for a Dashboard that boxed one up with
// nothing behind it.
func (c *Claims) Claim(root, note string) error {
	if c == nil {
		return errNowhereToKeepIt
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make(map[string]string, len(c.kept)+1)
	for r, n := range c.kept {
		kept[r] = n
	}
	kept[root] = note
	if err := c.state.Write(section, kept); err != nil {
		return err
	}
	c.kept = kept
	return nil
}

// Release undoes the Claim on root. Releasing a root nobody has claimed
// costs nothing: it is not an error to already have what you were asking
// for — which is also what a nil Claims reports, since it has never claimed
// anything either.
func (c *Claims) Release(root string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.kept[root]; !held {
		return nil
	}
	kept := make(map[string]string, len(c.kept))
	for r, n := range c.kept {
		if r == root {
			continue
		}
		kept[r] = n
	}
	if err := c.state.Write(section, kept); err != nil {
		return err
	}
	c.kept = kept
	return nil
}
