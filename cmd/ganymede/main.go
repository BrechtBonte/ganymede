// Command ganymede is the terminal harness: a Dashboard docked beside the
// Session you are working in.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/ghostty"
	"github.com/BrechtBonte/ganymede/internal/hooks"
	"github.com/BrechtBonte/ganymede/internal/inventory"
	"github.com/BrechtBonte/ganymede/internal/notifier"
	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/reconciler"
	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/session"
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
  ganymede notify-click <pid>
                            Focus Ghostty and jump the Dashboard to the
                            Session a notification was about. A clicked
                            notification runs this for you.
  ganymede popup open <dir> <session> <pane>
                            Open the Popup shell over pane, started in dir —
                            or the Dashboard's own selection when session is
                            the Dashboard's. tmux's root-table toggle runs
                            this for you; closing is the popup socket's own
                            business and never reaches here.
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
	case "notify-click":
		return notifyClick(args[1:])
	case "popup":
		return popupCommand(args[1:])
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
	// The notifier's OS channel, worth saying here or nowhere for the same
	// reason: a Dashboard running fine gives no sign that attention has
	// stopped reaching you beyond it.
	if _, err := exec.LookPath("terminal-notifier"); err != nil {
		fmt.Fprintln(os.Stderr, "ganymede: terminal-notifier is not installed, so Blocked and Ready will not reach you beyond the Dashboard: brew install terminal-notifier")
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
	merged := model.Watch(ctx, watch, checked, reported)
	// The notifier watches the same working set the Dashboard draws, so it
	// can put a name and a pid to whatever the model's Alerts are about — but
	// a channel has one reader, and the Dashboard's is the tea.Program's own
	// goroutine, so each gets its own tap rather than racing for values meant
	// for both.
	dashboardSessions, notifierSessions := fanned(ctx, merged)

	// Tickets are asked about from both the Dashboard's rows and the
	// notifier's titles, and the two must read the same answer — including
	// the same corrections you have set by hand.
	tickets := known()
	runNotifier(ctx, notifierSessions, model.Alerts(), tickets)

	// The harness is every hand the Dashboard has on tmux: it steers the
	// working client to a Session or to a repo, and it carries the counts to
	// that client's status line.
	hands := dashboard.Harness{
		Jumper: harness, Opener: harness, Strip: harness, Spawner: harness, Popups: harness, Approver: harness,
		Prompter: harness, Seen: model.Seen, Tickets: tickets,
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

	_, err = tea.NewProgram(dashboard.New(dashboardSessions, hands), tea.WithAltScreen()).Run()
	return err
}

// fanned splits one stream of working sets into two, so the Dashboard and the
// notifier can each watch it on their own goroutine without racing each other
// for values meant for both.
func fanned(ctx context.Context, in <-chan []session.Session) (a, b <-chan []session.Session) {
	toA := make(chan []session.Session)
	toB := make(chan []session.Session)
	go func() {
		defer close(toA)
		defer close(toB)
		for {
			select {
			case <-ctx.Done():
				return
			case set, ok := <-in:
				if !ok {
					return
				}
				for _, out := range [](chan<- []session.Session){toA, toB} {
					select {
					case out <- set:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return toA, toB
}

// runNotifier turns the model's Alerts into notifications beyond the
// Dashboard (§9), on its own goroutine so a Session going Blocked never waits
// on the Dashboard's own redraw.
//
// Wiring it up is best effort: a notifier missing terminal-notifier still
// lets the Dashboard run, since the sidepanel already shows every state it
// would have banked a notification for.
func runNotifier(ctx context.Context, sessions <-chan []session.Session, alerts <-chan state.Alert, tickets notifier.Tickets) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ganymede: attention will not reach you beyond the Dashboard: %v\n", err)
		return
	}
	notif := notifier.Notifier{
		Send:      notifier.TerminalNotifier{},
		Frontmost: ghostty.Emulator{}.Frontmost,
		Tickets:   tickets,
		Click: func(pid int) []string {
			return []string{self, "notify-click", strconv.Itoa(pid)}
		},
	}
	go notif.Run(ctx, sessions, alerts)
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

// notifyClick is what a clicked notification runs (§9): it brings Ghostty
// forward and puts the Session the notification was about back in front of
// you, the same jump ⏎ does on the Dashboard. Like the hook and seen
// commands, it must work with no Dashboard running — terminal-notifier can
// run this long after the process that sent the notification has ended.
func notifyClick(args []string) error {
	if len(args) != 1 {
		return errors.New("notify-click takes the pid the notification was about")
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("not a pid: %s", args[0])
	}

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locate the current directory: %w", err)
	}
	harness, err := topology.Default(dir)
	if err != nil {
		return err
	}
	if err := (ghostty.Emulator{}).Activate(); err != nil {
		return err
	}
	return harness.Jump(pid)
}

// popupCommand dispatches the Popup shell's own subcommands. There is only
// one: closing is the popup socket's own root table (topology.Harness),
// which never reaches this binary at all.
func popupCommand(args []string) error {
	if len(args) == 0 || args[0] != "open" {
		return errors.New("popup takes one subcommand: open <dir> <session> <pane>")
	}
	return popupOpen(args[1:])
}

// popupOpen shows the Popup shell over pane: the pressed pane's own
// directory, unless session names the Dashboard, which has none of its own
// to offer — the rail asks the harness what the Dashboard last selected
// instead (see popup.TargetDir). tmux's root-table toggle runs this for
// you; see tmuxconf's popupHook.
func popupOpen(args []string) error {
	if len(args) != 3 {
		return errors.New("popup open takes the pane's directory, session name, and pane id")
	}
	paneDir, session, pane := args[0], args[1], args[2]

	harness, err := topology.Default(paneDir)
	if err != nil {
		return err
	}
	// Asked only when the toggle was pressed on the Dashboard's own pane:
	// every other press is the ordinary case, and TargetDir never looks at
	// this value for it, so asking anyway would be a tmux round trip paid
	// on every single popup for an answer that is always thrown away.
	selected := ""
	if session == topology.DashboardSession {
		selected = harness.SelectedDir()
	}
	dir := popup.TargetDir(session, topology.DashboardSession, paneDir, selected)
	return harness.OpenPopup(dir, pane)
}
