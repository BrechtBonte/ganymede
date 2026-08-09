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

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/ghostty"
	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/inventory"
	"github.com/BrechtBonte/ganymede/internal/reconciler"
	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/state"
	"github.com/BrechtBonte/ganymede/internal/ticket"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
	"github.com/BrechtBonte/ganymede/internal/topology"
	"github.com/BrechtBonte/ganymede/internal/workingset"
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
	// And the same for the cross-check, which is worth saying here or nowhere:
	// the Dashboard has no way to show you that it silently went without one,
	// and a claude it cannot run at all is exactly the day it would matter.
	if err := crossCheck(); err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: Claude Code cannot be asked for its Sessions, so the Dashboard runs on the registry alone: %v\n", err)
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

// runDashboard draws the working set — the registry's account, corrected by
// the reconciler's cross-check and with the hooks' laid over both — and steers
// the working client on your behalf.
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
	// The slow cross-check against the interface Claude Code documents, which
	// is what the registry watch's undocumented one is insured by. It needs
	// nothing from the harness and cannot fail to start: a machine whose
	// claude will not run still has a registry to watch.
	checked := reconciler.Reconciler{}.Watch(ctx)

	model := state.New()
	working := model.Watch(ctx, watch, checked, reported)

	// The harness is every hand the Dashboard has on tmux: it steers the
	// working client to a Session or to a repo, and it carries the counts to
	// that client's status line.
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Strip: harness,
		Seen: model.Seen, Tickets: known(),
	}
	// Where the harness looks for repos, and what it remembers about the ones
	// it has been in. Neither is worth holding the Dashboard up over: a picker
	// with nothing behind it costs you the repos you are not already working
	// in, and a state file it cannot read costs the working set its memory of
	// the quiet ones.
	if scan, err := inventory.Default(); err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: the repo picker has nothing to offer: %v\n", err)
	} else {
		hands.Inventory = scan
	}
	if remembered, err := whereYouHaveBeen(); remembered != nil {
		hands.Activity = remembered
		if err != nil {
			fmt.Fprintf(os.Stderr, "ganymede: %v\n", err)
		}
	}

	_, err = tea.NewProgram(dashboard.New(working, hands), tea.WithAltScreen()).Run()
	return err
}

// whereYouHaveBeen is the harness's memory of which repos you have been working
// in, which is what the working set holds on to after a repo's Sessions end.
//
// Like the tickets, a state file that cannot be read costs what is in it and
// nothing else: the Dashboard still shows everything running, and the repos it
// would otherwise have kept on for the week are a keystroke away in the picker.
func whereYouHaveBeen() (*workingset.Activity, error) {
	sidecar, err := config.DefaultSidecar()
	if err != nil {
		return nil, fmt.Errorf("the working set will not survive a restart: %w", err)
	}
	activity, err := workingset.Load(sidecar)
	if err != nil {
		return activity, fmt.Errorf("the repos you were working in cannot be read: %w", err)
	}
	return activity, nil
}

// known is which ticket each Session is about: what the branches and worktree
// names say, with whatever you have corrected by hand over the top.
//
// A state file that cannot be read costs the corrections in it and nothing
// else. Every ticket the harness can work out for itself still shows, and the
// Dashboard — whose job is telling you what is running — is not held up over a
// sidecar file.
func known() dashboard.Tickets {
	sidecar, err := config.DefaultSidecar()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: tickets set by hand cannot be kept: %v\n", err)
		return &ticket.Tickets{}
	}
	overrides, err := ticket.Load(sidecar)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: tickets set by hand cannot be read: %v\n", err)
	}
	return &ticket.Tickets{Overrides: overrides}
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

// crossCheck asks Claude Code for its Sessions once, to find out whether it
// can be asked at all.
func crossCheck() error {
	_, err := reconciler.Reconciler{}.Read(context.Background())
	return err
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
