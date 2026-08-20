package release_test

import (
	"testing"

	"github.com/BrechtBonte/ganymede/internal/release"
)

// TestBehind is the comparison the notice turns on. Versions are compared
// field by field as numbers: read as text, 2.1.99 sorts above 2.1.100, and
// that is the one comparison this must never get wrong.
func TestBehind(t *testing.T) {
	for _, c := range []struct {
		installed, latest string
		behind            bool
	}{
		{installed: "2.1.237", latest: "2.1.240", behind: true},
		{installed: "2.1.99", latest: "2.1.100", behind: true},
		{installed: "1.9.9", latest: "2.0.0", behind: true},
		{installed: "2.1.240", latest: "2.1.240", behind: false},
		{installed: "2.1.240", latest: "2.1.237", behind: false},
		{installed: "2.2.0", latest: "2.1.240", behind: false},
	} {
		update := release.Update{Installed: c.installed, Latest: c.latest}
		if behind := update.Behind(); behind != c.behind {
			t.Errorf("%s against %s: Behind() = %v, want %v", c.installed, c.latest, behind, c.behind)
		}
	}
}

// TestBehindWithoutBothVersions draws nothing rather than guessing. A check
// that could not read one side of the comparison has no comparison to report,
// and a notice made up out of half an answer would be worse than none.
func TestBehindWithoutBothVersions(t *testing.T) {
	for _, c := range []release.Update{
		{Installed: "2.1.237", Latest: ""},
		{Installed: "", Latest: "2.1.240"},
		{},
		{Installed: "2.1.237", Latest: "not a version"},
		{Installed: "unreleased", Latest: "2.1.240"},
	} {
		if c.Behind() {
			t.Errorf("Update{Installed: %q, Latest: %q}: Behind() = true, want false", c.Installed, c.Latest)
		}
	}
}

// claudeThat writes a stand-in for the Claude Code binary: one that answers
