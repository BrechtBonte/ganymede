package tile_test

import (
	"errors"
	"io"
	"strings"
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

// pipe is the tile process's stdin, as a test can read it back.
type pipe struct {
	written strings.Builder
	closed  bool
	err     error
}

func (p *pipe) Write(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	return p.written.Write(b)
}

func (p *pipe) Close() error {
	p.closed = true
	return nil
}

// spawning is a Tile whose process is the pipe handed back here, counting how
// many times it was asked for one.
func spawning(p *pipe, err error) (*tile.Tile, *int) {
	starts := 0
	return &tile.Tile{Start: func() (io.WriteCloser, error) {
		starts++
		return p, err
	}}, &starts
}

// The first Badge is what puts the Tile on screen, even with nothing Blocked:
// the harness being up is itself worth showing, and the icon appearing only
// once something blocked would leave nothing to click the rest of the time.
func TestTheFirstBadgeStartsTheTileWithNothingBlocked(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	if err := tl.Badge(session.Attention{}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
	if p.written.String() != "\n" {
		t.Errorf("the Tile was sent %q, want an empty label", p.written.String())
	}
}

// One line per change, so the tile process can read a whole label or nothing.
func TestBadgeSendsTheLabelAsALine(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)

	if err := tl.Badge(session.Attention{Blocked: 2}); err != nil {
		t.Fatalf("Badge: %v", err)
	}

	if p.written.String() != "2\n" {
		t.Errorf("the Tile was sent %q, want %q", p.written.String(), "2\n")
	}
}

// The working set is rebuilt whenever anything at all moves, and almost none
// of it is about the Blocked count. A label that has not changed is not worth
// a write, and Ready moving is not the Tile's business at all.
func TestALabelThatHasNotMovedIsNotSentAgain(t *testing.T) {
	p := &pipe{}
	tl, starts := spawning(p, nil)

	for _, waiting := range []session.Attention{
		{Blocked: 1},
		{Blocked: 1},
		{Blocked: 1, Ready: 4},
	} {
		if err := tl.Badge(waiting); err != nil {
			t.Fatalf("Badge: %v", err)
		}
	}

	if p.written.String() != "1\n" {
		t.Errorf("the Tile was sent %q, want the label once", p.written.String())
	}
	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// A pipe to a child process does not fail transiently: it fails because the
// process is gone, which is what quitting the tile from its own Dock menu
// does. That gesture is respected until the Dashboard is next started, rather
// than answered with a fresh tile by the next Session that blocks.
func TestAWriteThatFailedRetiresTheTile(t *testing.T) {
	p := &pipe{err: errors.New("write |1: broken pipe")}
	tl, starts := spawning(p, nil)

	if err := tl.Badge(session.Attention{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a pipe that is gone")
	}
	if err := tl.Badge(session.Attention{Blocked: 2}); err != nil {
		t.Errorf("a retired Tile complained again: %v", err)
	}

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want the retired one left alone", *starts)
	}
}

// A tile that could not be started is retired the same way, so a Dashboard
// does not try to spawn a process it has already failed to spawn on every
// Session that blocks for the rest of the day.
func TestATileThatCouldNotBeStartedIsNotStartedAgain(t *testing.T) {
	tl, starts := spawning(&pipe{}, errors.New("fork/exec: no such file or directory"))

	if err := tl.Badge(session.Attention{Blocked: 1}); err == nil {
		t.Fatal("Badge said nothing about a tile that could not be started")
	}
	_ = tl.Badge(session.Attention{Blocked: 2})

	if *starts != 1 {
		t.Errorf("the Tile was started %d times, want once", *starts)
	}
}

// A harness whose launcher was never installed has no Tile, which is not a
// failure and not worth an error: everything else about the Dashboard works
// exactly as it did.
func TestATileWithNoAppIsSilent(t *testing.T) {
	tl := &tile.Tile{}

	if err := tl.Badge(session.Attention{Blocked: 1}); err != nil {
		t.Errorf("a harness with no launcher installed reported %v", err)
	}
}

// Closing is what a Dashboard quit by hand does on the way out. EOF would
// take the tile down anyway once the process ends, but the pipe closing is
// what makes the icon go at the moment you quit.
func TestCloseClosesThePipe(t *testing.T) {
	p := &pipe{}
	tl, _ := spawning(p, nil)
	_ = tl.Badge(session.Attention{Blocked: 1})

	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !p.closed {
		t.Error("Close left the pipe open")
	}
}

// Closing a Tile that never started is not an error either — a Dashboard
// quits the same way whether or not anything ever blocked.
func TestClosingATileThatNeverStartedIsFine(t *testing.T) {
	tl, _ := spawning(&pipe{}, nil)

	if err := tl.Close(); err != nil {
		t.Errorf("Close on an unstarted Tile: %v", err)
	}
}
