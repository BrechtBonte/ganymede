package topology

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Jump puts the Session running as pid in front of you: the working client
// switches to the window that Session is running in, while the Dashboard stays
// where it is in its own client.
//
// It refuses rather than guesses. A Session the harness cannot place in a pane
// — started outside tmux, or gone since the registry was read — leaves the
// working client showing whatever it was showing.
func (h Harness) Jump(pid int) error {
	target, err := h.locate(pid)
	if err != nil {
		return err
	}
	client, err := h.workingClient()
	if err != nil {
		return err
	}

	// One call moves the whole way: tmux takes a pane as a switch-client
	// target and changes session, window and pane together, so there is no
	// half-done jump that leaves the working client on a window nobody asked
	// for. A pane id is also the one target that cannot be misread — the
	// Sessions on show include tmux sessions the harness did not name, and a
	// name carrying ":" or "." is split by tmux before the "=" exact-match
	// prefix is even considered (see WorkingSessionName).
	if err := h.sessions().run("switch-client", "-c", client, "-t", target); err != nil {
		return fmt.Errorf("point the working client at the Session's pane: %w", err)
	}
	return nil
}

// locate returns the id of the tmux pane a process is running in.
func (h Harness) locate(pid int) (string, error) {
	panes, _, err := h.panes()
	if err != nil {
		return "", err
	}
	parents, err := parents()
	if err != nil {
		return "", err
	}
	found, ok := paneOf(pid, panes, parents)
	if !ok {
		return "", fmt.Errorf("no tmux pane is running process %d", pid)
	}
	return found, nil
}

// paneOf walks up from pid until it reaches a process tmux started in a pane.
// The walk is the point: tmux knows the shell it started, while a Session is
// that shell's descendant, and the registry only ever names the Session.
func paneOf(pid int, panes map[int]string, parents map[int]int) (string, bool) {
	// No process is its own ancestor, so the table's size bounds the walk
	// however inconsistent a snapshot of it turns out to be.
	for range len(parents) + 1 {
		if found, ok := panes[pid]; ok {
			return found, true
		}
		parent, ok := parents[pid]
		// Reaching pid 1 means the process is running outside every pane.
		if !ok || parent <= 1 {
			return "", false
		}
		pid = parent
	}
	return "", false
}

// Under reports which of pids are running inside the process root — root
// itself, or anything below it.
//
// It is how a pane names the Sessions it holds, which is what turns focus
// landing on a pane into those Sessions having been seen. tmux knows only the
// process it started in the pane; a Session is that process's descendant,
// however many shells down.
func Under(root int, pids []int) ([]int, error) {
	parents, err := parents()
	if err != nil {
		return nil, err
	}
	found := make([]int, 0, len(pids))
	for _, pid := range pids {
		if descends(pid, root, parents) {
			found = append(found, pid)
		}
	}
	return found, nil
}

// descends walks up from pid looking for root.
func descends(pid, root int, parents map[int]int) bool {
	// No process is its own ancestor, so the table's size bounds the walk
	// however inconsistent a snapshot of it turns out to be.
	for range len(parents) + 1 {
		if pid == root {
			return true
		}
		parent, ok := parents[pid]
		// Reaching pid 1 means the walk left the pane behind long ago.
		if !ok || parent <= 1 {
			return false
		}
		pid = parent
	}
	return false
}

// workingClient is how the Sessions server knows the working client: by the
// pty it runs on, which is the dock's right-hand pane.
func (h Harness) workingClient() (string, error) {
	out, err := exec.Command("tmux", h.dock().args("display-message", "-p",
		"-t", "="+DockSession+":0.1", "#{pane_tty}")...).Output()
	if err != nil {
		return "", fmt.Errorf("find the working client: %w", err)
	}
	tty := strings.TrimSpace(string(out))
	if tty == "" {
		return "", fmt.Errorf("no window is showing the harness to jump in")
	}
	return tty, nil
}

// panes maps the process tmux started in each pane to that pane's id, and says
// which of those panes is holding a mode over its live view.
//
// Both answers come off one list-panes because they are asked of the same
// panes at the same moment: a pane whose mode was entered between two calls
// would otherwise be located by the first and judged by the second.
func (h Harness) panes() (map[int]string, map[string]bool, error) {
	out, err := exec.Command("tmux", h.sessions().args("list-panes", "-a",
		"-F", "#{pane_pid} #{pane_id} #{pane_in_mode}")...).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list the panes: %w", err)
	}

	panes := map[int]string{}
	held := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		panes[pid] = fields[1]
		held[fields[1]] = fields[2] == "1"
	}
	return panes, held, nil
}

// Frozen says which of pids are running in a pane holding a mode over its live
// view — a pane showing a held picture of the Session rather than the Session,
// and handing every keystroke to the mode instead of to Claude.
//
// It resolves each pid the way locate does, and for the same reason: tmux knows
// only the process it started in the pane, and a Session is that process's
// descendant however many shells down.
//
// A pid the harness cannot place in a pane is left out of the map rather than
// answered false. A Session running outside tmux has no pane whose view could
// be held, and saying it is not frozen would be claiming to have looked.
func (h Harness) Frozen(pids []int) (map[int]bool, error) {
	panes, held, err := h.panes()
	if err != nil {
		return nil, err
	}
	parents, err := parents()
	if err != nil {
		return nil, err
	}

	frozen := make(map[int]bool, len(pids))
	for _, pid := range pids {
		pane, ok := paneOf(pid, panes, parents)
		if !ok {
			continue
		}
		frozen[pid] = held[pane]
	}
	return frozen, nil
}

// parents maps every process to its parent.
func parents() (map[int]int, error) {
	out, err := exec.Command("ps", "-Ao", "pid=,ppid=").Output()
	if err != nil {
		return nil, fmt.Errorf("read the process table: %w", err)
	}

	parents := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		parents[pid] = parent
	}
	return parents, nil
}
