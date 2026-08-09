package hooks

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// backlog is how many events the receiver holds while the Dashboard is
// redrawing. Events arrive in bursts of a few — a turn ending, a prompt going
// out — and never faster than a person can work.
const backlog = 64

// handling bounds one report. A hook command writes its payload and closes;
// anything that takes longer than this is not a hook, and the receiver takes
// them one at a time, so it may not wait on any of them.
const handling = time.Second

// tooBig is the most a report may be. Hook payloads carry whole assistant
// messages, so the limit is generous, but the socket is a hole in the
// Dashboard that anything on the machine can write to.
const tooBig = 8 << 20

// Listen takes hook events off a local socket until ctx ends, closing the
// channel when it stops.
//
// The socket belongs to one Dashboard at a time. What is usually at the path
// is the socket a killed Dashboard left behind, which is cleared away rather
// than treated as an obstacle — the harness is not something you should have
// to tidy up after. But a Dashboard that answers is a Dashboard still working,
// and taking the path from it would leave it drawing states that had quietly
// stopped arriving, so this refuses instead.
func Listen(ctx context.Context, socket string) (<-chan Event, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, fmt.Errorf("create the directory for %s: %w", socket, err)
	}
	if listening(socket) {
		return nil, fmt.Errorf("a Dashboard is already listening on %s", socket)
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("clear the socket at %s: %w", socket, err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen for hook events on %s: %w", socket, err)
	}
	// Only this user's Sessions report to this Dashboard.
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("set permissions on %s: %w", socket, err)
	}
	// Which socket this is, so that shutting down removes this one and not
	// whatever has taken the path since.
	bound, err := os.Stat(socket)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("stat %s: %w", socket, err)
	}
	if unix, ok := listener.(*net.UnixListener); ok {
		// Closing would otherwise remove whatever is at the path, ours or not.
		unix.SetUnlinkOnClose(false)
	}

	events := make(chan Event, backlog)
	go accept(ctx, listener, socket, bound, events)
	return events, nil
}

// listening reports whether a Dashboard is already answering on a socket.
func listening(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, reaching)
	if err != nil {
		// Nothing there, something that is not a socket, or a socket whose
		// Dashboard is gone. None of them is anybody to take it from.
		return false
	}
	conn.Close()
	return true
}

// accept hands over every event reported until ctx ends.
func accept(ctx context.Context, listener net.Listener, socket string, bound os.FileInfo, events chan<- Event) {
	defer close(events)
	// The socket file goes when the Dashboard does, so that nothing is left
	// pointing at a receiver that has stopped listening — unless the path has
	// come to mean a different socket since, which is another Dashboard's to
	// clear away and not this one's.
	defer func() {
		if now, err := os.Stat(socket); err == nil && os.SameFile(bound, now) {
			os.Remove(socket)
		}
	}()

	// Closing the listener is what unblocks Accept; there is no deadline on it
	// to wait out.
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// The Dashboard has stopped, or the socket has gone out from under
			// it. Either way there is nothing left to accept.
			return
		}
		// One report at a time. Order is the whole point: a turn ending and
		// the jump that reads it are milliseconds apart, and handled
		// concurrently they could reach the state model the wrong way round —
		// which is an unread badge on a Session you are looking at.
		event, ok := read(conn)
		if !ok {
			continue
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return
		}
	}
}

// read takes one report off a connection, reporting whether it was an event
// the harness acts on.
func read(conn net.Conn) (Event, bool) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(handling)); err != nil {
		return Event{}, false
	}
	body, err := io.ReadAll(io.LimitReader(conn, tooBig))
	if err != nil {
		return Event{}, false
	}
	event, ok := Parse(body)
	if !ok {
		return Event{}, false
	}
	event.At = time.Now()
	return event, true
}
