package ticket_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// tickets is what the Dashboard asks, over a state file of the test's own.
func tickets(t *testing.T, sidecar config.Sidecar) *ticket.Tickets {
	t.Helper()
	return &ticket.Tickets{Overrides: loaded(t, sidecar)}
}

// A Session on a branch named after the work is the whole ordinary case, and it
// asks nothing at all of you.
func TestSessionOnATicketBranchIsAboutThatTicket(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "checkout", "-q", "-b", "feat/FIRE-2841-max-paging-numbers")

	if got := tickets(t, state(t)).Of(root, root); got != "FIRE-2841" {
		t.Errorf("Of(%s) = %q, want %q from the branch", root, got, ticket.Key("FIRE-2841"))
	}
}

// A Worktree session's directory carries the ticket too — it is what the spawn
// names it after — so a worktree whose branch has been renamed out from under
// it still knows what it is about.
func TestWorktreeSessionFallsBackToItsDirectoryName(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	worktree := filepath.Join(root, ".claude", "worktrees", "FIRE-2841-paging")
	git(t, root, "worktree", "add", "-q", "-b", "paging-work", worktree)

	if got := tickets(t, state(t)).Of(worktree, root); got != "FIRE-2841" {
		t.Errorf("Of(%s) = %q, want %q from the worktree directory", worktree, got, ticket.Key("FIRE-2841"))
	}
}

// The directory a Session is working in only speaks for a worktree, which is
// named after its ticket. A Main root is named after the repo, and a repo may be
// called anything at all — reading a ticket out of one would put a key on a row
// that nobody chose and nothing is about.
func TestMainRootIsNotReadForATicket(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "AB-12-tooling"))

	if got := tickets(t, state(t)).Of(root, root); got != "" {
		t.Errorf("Of(%s) = %q, want no ticket", root, got)
	}
}

// A Session with nothing to read a ticket off is a Session with no ticket,
// which the rail says out loud rather than filling in.
func TestSessionWithNothingToReadHasNoTicket(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))

	if got := tickets(t, state(t)).Of(root, root); got != "" {
		t.Errorf("Of(%s) = %q, want no ticket", root, got)
	}
}

// The point of being able to set one: the branch is wrong, or says nothing, and
// what you typed wins over what the harness worked out.
func TestTicketSetByHandBeatsTheBranch(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "checkout", "-q", "-b", "feat/FIRE-2841-max-paging-numbers")
	sidecar := state(t)

	known := tickets(t, sidecar)
	if err := known.Set(root, root, "CORE-119"); err != nil {
		t.Fatalf("set the ticket: %v", err)
	}
	if got := known.Of(root, root); got != "CORE-119" {
		t.Errorf("Of(%s) = %q, want the ticket that was set by hand", root, got)
	}

	// And the next run of the harness agrees, which is what makes correcting
	// the harness once worth doing.
	if got := tickets(t, sidecar).Of(root, root); got != "CORE-119" {
		t.Errorf("Of(%s) = %q after a restart, want the ticket that was set by hand", root, got)
	}
}

// Clearing the override hands the row back to the branch rather than leaving it
// blank: the correction is undone, not replaced by another one.
func TestClearingTheOverrideGivesTheBranchTheRowBack(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "checkout", "-q", "-b", "feat/FIRE-2841-max-paging-numbers")

	known := tickets(t, state(t))
	if err := known.Set(root, root, "CORE-119"); err != nil {
		t.Fatal(err)
	}
	if err := known.Set(root, root, ""); err != nil {
		t.Fatal(err)
	}

	if got := known.Of(root, root); got != "FIRE-2841" {
		t.Errorf("Of(%s) = %q, want the branch's %q back", root, got, ticket.Key("FIRE-2841"))
	}
}

// shown records the link handed to the browser, standing in for the desktop.
type shown struct{ url string }

func (s *shown) Open(url string) error {
	s.url = url
	return nil
}

// Opening a ticket is the second half of what the harness knows about one: the
// ID tells two Sessions apart, and the link is where everything else about it
// lives.
func TestOpeningATicketHandsItsLinkToTheBrowser(t *testing.T) {
	desktop := &shown{}
	known := &ticket.Tickets{Browser: desktop}

	if err := known.Open("FIRE-2841"); err != nil {
		t.Fatalf("open the ticket: %v", err)
	}
	if got, want := desktop.url, "https://teamleader.atlassian.net/browse/FIRE-2841"; got != want {
		t.Errorf("opened %q, want %q", got, want)
	}
}

// A Session about no ticket has no link, and the address that no ticket makes —
// the browse page of nothing — is a page nobody wants opened.
func TestOpeningNoTicketOpensNothing(t *testing.T) {
	desktop := &shown{}
	known := &ticket.Tickets{Browser: desktop}

	if err := known.Open(""); err == nil {
		t.Error("opened a Session's ticket when it is about none, want an error")
	}
	if desktop.url != "" {
		t.Errorf("opened %q, want nothing opened", desktop.url)
	}
}

// A state file the harness cannot read costs the corrections in it and nothing
// else. The Dashboard's whole job is showing you what is running, and refusing
// to start over a sidecar file would be the harness holding that to ransom.
func TestStateFileThatWillNotParseCostsTheOverridesOnly(t *testing.T) {
	sidecar := state(t)
	if err := os.MkdirAll(filepath.Dir(sidecar.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar.Path, []byte(`{"tickets": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "checkout", "-q", "-b", "feat/FIRE-2841-max-paging-numbers")

	overrides, err := ticket.Load(sidecar)
	if err == nil {
		t.Error("loaded a state file that will not parse without complaint, want an error")
	}
	if overrides == nil {
		t.Fatal("Load returned no overrides at all, want ones the Dashboard can still run on")
	}
	known := &ticket.Tickets{Overrides: overrides}
	if got := known.Of(root, root); got != "FIRE-2841" {
		t.Errorf("Of(%s) = %q, want the branch's %q", root, got, ticket.Key("FIRE-2841"))
	}
}
