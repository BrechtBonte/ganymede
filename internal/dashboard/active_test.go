package dashboard_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

// Browsing the cursor away from a Session you just jumped to must not erase
// the one mark saying what the working client is actually showing right
// now — only the cursor's own highlight should move on.
func TestTheJumpedToSessionStaysMarkedAfterBrowsingAway(t *testing.T) {
	jumper := &jumps{}
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks it active
	model = press(model, tea.KeyUp)    // back to the repo header row

	line := rawSessionRow(t, model)
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("session row = %q, want it still marked dim-reverse as the active row", line)
	}

	header, ok := rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", drawn(model))
	}
	if !strings.HasPrefix(header, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want the cursor's own plain reverse", header)
	}
}

// A jump that fails outright — the harness could not reach the pane at
// all, as against Gone's "the process itself has ended" — leaves the
// working client showing whatever it already was. The row it failed on
// must not be marked active over that.
func TestAJumpThatFailsDoesNotMarkItsRowActive(t *testing.T) {
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	// One in the Main root and one in a worktree of it: the checkout each row
	// is labelled with is what tells the two apart, and their directories'
	// differing lengths keep live()'s PID (len(name) + len(dir)) distinct.
	root := mainRoot(t, "ganymede")
	a := live("aaa-idle", root, session.Idle)
	b := live("bbb-idle", worktree(t, root, "paging"), session.Idle)
	model := sidepanel(jumper, a, b)

	model = press(model, tea.KeyDown)  // onto a's row, which sorts first by name
	model = press(model, tea.KeyEnter) // jump fails
	model = press(model, tea.KeyDown)  // onto b's row

	line := rawSessionRowFor(t, model, "main")
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) || strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("aaa-idle row = %q, want no mark left behind by a jump that failed", line)
	}
}

// Opening a bare repo (Enter on a repo header with nothing running, or the
// picker's Enter) points the working client at a directory with no Session
// in it at all, so no row should keep reading as active once that happens.
func TestOpeningABareRepoClearsTheActiveRow(t *testing.T) {
	jumper := &jumps{}
	opener := &opens{}
	state := remembering(t)
	worked(t, state, "/repos/other-repo", time.Now())
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := dashboardOn(dashboard.Harness{Jumper: jumper, Opener: opener, Activity: state}, only)

	model = press(model, tea.KeyDown)  // ganymede's header row onto its Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active
	model = press(model, tea.KeyDown)  // onto the bare other-repo's header row
	model = press(model, tea.KeyEnter) // goTo: opens it

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/other-repo" {
		t.Fatalf("opened %v, want [/repos/other-repo]", opener.dirs)
	}

	line := rawSessionRow(t, model)
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("the Session's row = %q, want the active mark cleared once a bare repo is opened", line)
	}
}

// A pid Jump has confirmed Gone can be handed by the OS to an unrelated
// process later; the registry then reports that as a brand new Session
// that was never jumped to, and it must not inherit the old one's mark.
func TestForgettingAGoneSessionClearsTheActiveMarkForWhoeverReusesItsPid(t *testing.T) {
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	jumper := &jumps{}
	model := sidepanel(jumper, only)

	model = press(model, tea.KeyDown)  // onto the Session row
	model = press(model, tea.KeyEnter) // jump: marks ganymede-78 active

	jumper.err = topology.GoneError{PID: only.PID}
	model = press(model, tea.KeyEnter) // jump fails Gone: forgets and drops the row

	// The registry catches up and stops reporting the pid at all — the same
	// step TestAForgottenPidIsPrunedOnceItsSourceMovesOn uses. Without it,
	// withoutForgotten would suppress the reused Session below as a stale
	// re-report of the same dead one, and this test would never reach the
	// row it is actually about.
	model, _ = model.Update(dashboard.Sessions{})

	reused := session.Session{PID: only.PID, ID: "different-id", Dir: "/repos/ganymede",
		Name: "ganymede-new", State: session.Idle, Since: epoch}
	model, _ = model.Update(dashboard.Sessions{reused})

	line := rawSessionRow(t, model)
	if strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("the reused pid's row = %q, want a forgotten pid's old active mark not carried onto whoever reuses it", line)
	}
}
