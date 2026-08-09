// Command ganymede is the terminal harness: a Dashboard docked beside the
// Session you are working in.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/ghostty"
	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/state"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

const usage = `ganymede — a Dashboard docked beside the Session you are working in.

Usage:
  ganymede up [directory]   Open the harness: the Dashboard docked beside a
                            working client for the repo at directory (default
                            the current one). Safe to re-run.
  ganymede dashboard        Run the Dashboard in this terminal. The harness
                            runs this for you inside the sidepanel.
  ganymede install          Install the tmux configuration and the Claude Code
                            hooks only.
  ganymede hook             Report a hook payload on stdin to the Dashboard.
                            Claude Code runs this for you; the install wires it.
  ganymede seen <pid>       Report the Sessions running inside a process as
                            seen, which clears Ready. tmux runs this for you
                            when focus lands on a pane.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch command := args[0]; command {
	case "up":
		return up(args[1:])
	case "dashboard":
		return runDashboard()
	case "install":
		return install()
	case "hook":
		return report(os.Stdin)
	case "seen":
		return seen(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}

// up brings the whole harness into view with one command.
func up(args []string) error {
	if len(args) > 1 {
		return errors.New("up takes at most one directory")
	}
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locate the current directory: %w", err)
	}
	if len(args) == 1 {
		dir = args[0]
	}

	if err := installTmux(); err != nil {
		return err
	}
	// The tmux configuration is what the harness cannot open without. The
	// hooks only make the Dashboard sharper, and settings the harness will not
	// rewrite — somebody's hand-edited JSON, a shape it does not know — are
	// worth saying out loud and nothing more. Refusing to open the window over
	// them would be the harness holding your day to ransom over a file it only
	// wanted to add a line to.
	if err := installHooks(); err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: the hooks are not installed, so Ready and Blocked reasons will be missing: %v\n", err)
	}

	harness, err := topology.Default(dir)
	if err != nil {
		return err
	}
	if err := harness.Ensure(); err != nil {
		return err
	}

	// Wide enough that the working client still has room beside the sidepanel.
	emulator := ghostty.Emulator{Width: 200, Height: 50}
	if harness.Attached() {
		return emulator.Activate()
	}
	return emulator.Open(harness.AttachCommand())
}

// runDashboard draws the working set — the registry's account with the hooks'
// laid over it — and steers the working client on your behalf.
func runDashboard() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locate the current directory: %w", err)
	}
	harness, err := topology.Default(dir)
	if err != nil {
		return err
	}
	sessions, err := registry.Default()
	if err != nil {
		return err
	}
	socket, err := hooks.DefaultSocket()
	if err != nil {
		return err
	}

	// Both watches are the Dashboard's: they end when the Dashboard does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := sessions.Watch(ctx)
	if err != nil {
		return err
	}
	reported, err := hooks.Listen(ctx, socket)
	if err != nil {
		return err
	}

	model := state.New()
	working := model.Watch(ctx, watch, reported)

	_, err = tea.NewProgram(dashboard.New(working, harness, model.Seen), tea.WithAltScreen()).Run()
	return err
}

// install puts both halves in place, and reports either one failing. Asked for
// on its own it is a thing you did on purpose, so nothing here is softened.
func install() error {
	if err := installTmux(); err != nil {
		return err
	}
	return installHooks()
}

func installTmux() error {
	layout, err := tmuxconf.DefaultLayout()
	if err != nil {
		return err
	}
	return tmuxconf.Install(layout)
}

func installHooks() error {
	settings, err := hooks.DefaultSettings()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the ganymede binary: %w", err)
	}
	return hooks.Install(settings, hooks.Command(self))
}

// report hands a hook payload to the Dashboard.
//
// It runs inside a Session's own turn, and there is nothing it could tell that
// Session that would be worth the interruption: a Dashboard that is not up, a
// payload this harness does not read, a socket that has gone — all of them end
// the same way, quietly and at once. The only thing it must never do is fail,
// print, or wait.
func report(payload io.Reader) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return nil
	}
	socket, err := hooks.DefaultSocket()
	if err != nil {
		return nil
	}
	_ = hooks.Forward(socket, body)
	return nil
}

// seen reports every Session running inside a process — the process tmux
// started in the pane focus has just landed on — as one you have now looked
// at, which is what clears Ready.
//
// Like the hook command, it is run by something that must not be held up, so
// it says nothing about what it could not do.
func seen(args []string) error {
	if len(args) != 1 {
		return errors.New("seen takes the pid of the pane focus landed on")
	}
	pane, err := strconv.Atoi(args[0])
	if err != nil {
		return nil
	}

	sessions, err := registry.Default()
	if err != nil {
		return nil
	}
	running, err := sessions.Read()
	if err != nil {
		return nil
	}
	pids := make([]int, len(running))
	for i, s := range running {
		pids[i] = s.PID
	}
	inside, err := topology.Under(pane, pids)
	if err != nil || len(inside) == 0 {
		return nil
	}

	socket, err := hooks.DefaultSocket()
	if err != nil {
		return nil
	}
	held := map[int]bool{}
	for _, pid := range inside {
		held[pid] = true
	}
	for _, s := range running {
		if held[s.PID] {
			_ = hooks.Forward(socket, hooks.SeenPayload(s.ID))
		}
	}
	return nil
}
