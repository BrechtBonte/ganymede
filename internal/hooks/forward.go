package hooks

import (
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// reaching is how long the forwarder will spend on a Dashboard. It runs inside
// a Session's turn, where the only acceptable cost is one you cannot feel: a
// Dashboard that is not up is answered by the kernel immediately, and one that
// is wedged is not worth waiting for.
const reaching = 250 * time.Millisecond

// Forward hands a hook payload to the Dashboard's receiver.
//
// The error is for whoever wants to know; the hook command itself has no use
// for it. A Session must not fail, stall, or print anything because the
// harness was not listening.
func Forward(socket string, body []byte) error {
	conn, err := net.DialTimeout("unix", socket, reaching)
	if err != nil {
		return fmt.Errorf("reach the Dashboard at %s: %w", socket, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(reaching)); err != nil {
		return fmt.Errorf("report to the Dashboard: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("report to the Dashboard: %w", err)
	}
	return nil
}

// DefaultSocket is where the Dashboard listens, and where every hook command
// reports. Both ends work it out from the same rule rather than being told,
// so an installed hook keeps working when the harness moves.
func DefaultSocket() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.sock"), nil
}
