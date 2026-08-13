package tile_test

import (
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/tile"
)

// Nothing Blocked is no badge at all. A tile reading 0 is a tile you stop
// looking at, and the count would lose the only thing it is for — the same
// call the strip makes when nothing is waiting on you.
func TestNothingBlockedIsNoLabel(t *testing.T) {
	if got := tile.Label(session.Attention{Ready: 3}); got != "" {
		t.Errorf("the Tile reads %q with nothing Blocked, want nothing", got)
	}
}

// The label is the Blocked count and nothing else: Ready has the rail and its
// own notification, and a number that moved for unread turns would stop
// meaning "something needs a decision".
func TestTheLabelIsTheBlockedCountAlone(t *testing.T) {
	for _, c := range []struct {
		waiting session.Attention
		want    string
	}{
		{session.Attention{Blocked: 1}, "1"},
		{session.Attention{Blocked: 2, Ready: 7}, "2"},
		{session.Attention{Blocked: 12}, "12"},
	} {
		if got := tile.Label(c.waiting); got != c.want {
			t.Errorf("%+v reads %q, want %q", c.waiting, got, c.want)
		}
	}
}
