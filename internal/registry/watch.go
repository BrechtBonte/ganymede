package registry

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultPoll is how often the registry is re-read when nothing in the
// directory has changed. It only has to catch a killed Session — one that
// never got to remove its own file — so it can afford to be slow.
const defaultPoll = 2 * time.Second

// settle is how long the watch waits for writing to stop before it reads.
// Claude Code touches a registry file several times in a burst, and reading
// between two of those writes finds half a file — which would take that
// Session off the Dashboard until the next read put it back.
const settle = 50 * time.Millisecond

// Watch reports the working set: once as it finds it, and again whenever it
// changes, until ctx ends. The channel is closed when the watch stops.
//
// Two things drive it. The directory itself, watched for the files Claude Code
// writes as Sessions start, change state, and end; and a slow poll, because a
// Session that is killed outright leaves its file behind and nothing about the
// directory changes at all.
//
// The first read happens here rather than in the background, so that a
// registry the harness cannot read at all is an error you are told about
// instead of a Dashboard that quietly reports no Sessions.
func (r Registry) Watch(ctx context.Context) (<-chan []Session, error) {
	first, err := r.Read()
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch the session registry: %w", err)
	}

	snapshots := make(chan []Session)
	go r.watch(ctx, watcher, first, snapshots)
	return snapshots, nil
}

func (r Registry) watch(ctx context.Context, watcher *fsnotify.Watcher, first []Session, snapshots chan<- []Session) {
	defer close(snapshots)
	defer watcher.Close()

	poll := r.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	settling := time.NewTimer(settle)
	settling.Stop()
	defer settling.Stop()

	// A registry directory that does not exist yet is not a failure — Claude
	// Code creates it with the first session — and neither is a watch that
	// later loses its grip on it. Either way the poll keeps the Dashboard
	// honest while every tick tries to put the watch back, so a harness
	// started before Claude Code ever ran does not stay two seconds behind
	// for the rest of the day.
	watching := watcher.Add(r.Dir) == nil

	last := first
	reported := false
	// report sends the working set unless it is the one already on show, so
	// that the constant rewriting of these files does not redraw it. It
	// returns false once the watch is over.
	report := func(sessions []Session) bool {
		if reported && slices.Equal(sessions, last) {
			return true
		}
		reported, last = true, sessions
		select {
		case snapshots <- sessions:
			return true
		case <-ctx.Done():
			return false
		}
	}
	// reread reports what the registry says now. A read that fails is worth
	// neither an empty Dashboard nor an error it has nowhere to put: keep
	// what is on show and try again on the next tick.
	reread := func() bool {
		sessions, err := r.Read()
		if err != nil {
			return true
		}
		return report(sessions)
	}

	// Reading is always put off until the writing has been quiet for a while,
	// whether a directory event or the poll asked for it. Stop before Reset is
	// safe to write plainly: since Go 1.23 a stopped timer can no longer
	// deliver a stale tick.
	pending := false
	readSoon := func() {
		settling.Stop()
		settling.Reset(settle)
		pending = true
	}

	if !report(first) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !watching {
				watching = watcher.Add(r.Dir) == nil
			}
			// A tick asks for a read but does not push one already coming
			// further away, which a poll faster than the settle delay would
			// otherwise do forever.
			if !pending {
				readSoon()
			}
		case <-settling.C:
			pending = false
			if !reread() {
				return
			}
		case _, ok := <-watcher.Events:
			if !ok {
				return
			}
			// One read after the writing stops, however many events the burst
			// was.
			readSoon()
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// The watch has lost its grip on the directory — dropped events,
			// or a directory that went away. The poll covers it until a tick
			// gets the watch back.
			watching = false
		}
	}
}
