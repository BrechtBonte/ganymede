// Command ganymede is the terminal harness: a Dashboard docked beside the
// Session you are working in.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/ghostty"
	"github.com/BrechtBonte/ganymede/internal/registry"
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
  ganymede install          Install the tmux configuration only.
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

	if err := install(); err != nil {
		return err
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

// runDashboard draws the working set the session registry reports, and steers
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

	// The watch is the Dashboard's: it ends when the Dashboard does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := sessions.Watch(ctx)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(dashboard.New(watch, harness), tea.WithAltScreen()).Run()
	return err
}

func install() error {
	layout, err := tmuxconf.DefaultLayout()
	if err != nil {
		return err
	}
	return tmuxconf.Install(layout)
}
