package topology_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// sessionPath is the directory a tmux session was started in.
func sessionPath(t *testing.T, socket, name string) string {
	t.Helper()
	return tmuxOn(t, socket, "display-message", "-p", "-t", "="+name+":", "#{session_path}")
}

// Picking a repo out of the picker takes you there. Unlike a jump, there may
// be nothing running in it yet — the repo's Session is what has to be brought
// up, at its Main root.
func TestOpenPointsTheWorkingClientAtTheReposSession(t *testing.T) {
	h := jumpable(t)
	billing := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))

	if err := h.Open(billing); err != nil {
		t.Fatalf("Open(%q): %v", billing, err)
	}

	session, _ := workingClientShows(t, h)
	if session != "service-billing" {
		t.Fatalf("the working client shows %q, want the repo's own Session", session)
	}
	if got := sessionPath(t, h.Socket, session); got != billing {
		t.Errorf("the Session was started in %q, want the Main root %q", got, billing)
	}
}

// The repo you are being taken to is usually one you have been in before. Its
// Session holds your shell history and whatever you left running in it, so
// opening the repo has to reach that Session rather than replace it.
func TestOpenReusesARepoSessionThatIsAlreadyRunning(t *testing.T) {
	h := jumpable(t)
	billing := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "service-billing", "-c", billing, paneCommand)
	running := panePIDInSession(t, h.Socket, "service-billing")

	if err := h.Open(billing); err != nil {
		t.Fatalf("Open(%q): %v", billing, err)
	}

	if got := panePIDInSession(t, h.Socket, "service-billing"); got != running {
		t.Errorf("the repo's Session was restarted (pane %d, was %d)", got, running)
	}
	if session, _ := workingClientShows(t, h); session != "service-billing" {
		t.Errorf("the working client shows %q, want the repo's own Session", session)
	}
}

// The picker offers the whole inventory, and an inventory of any size holds
// two repos with the same name under different organisations. A Session is
// named after its repo, so the second one to be opened has to be told apart
// from the first — otherwise picking it would quietly take you to the other.
func TestOpenTellsTwoReposOfTheSameNameApart(t *testing.T) {
	h := jumpable(t)
	root := t.TempDir()
	first := initRepo(t, filepath.Join(root, "acme", "api"))
	second := initRepo(t, filepath.Join(root, "globex", "api"))

	if err := h.Open(first); err != nil {
		t.Fatalf("Open(%q): %v", first, err)
	}
	if err := h.Open(second); err != nil {
		t.Fatalf("Open(%q): %v", second, err)
	}

	session, _ := workingClientShows(t, h)
	if got := sessionPath(t, h.Socket, session); got != second {
		t.Errorf("the working client shows %q, started in %q, want a Session in %q",
			session, got, second)
	}
}

// A Session started anywhere inside a repo is that repo's Session — running
// `ganymede up` in a subdirectory is ordinary. Opening the repo from the
// picker, which always offers the Main root, has to reach that same Session:
// a second one would leave your shell history and everything running in it
// behind, in a Session that looks identical from the outside.
func TestOpenReachesTheReposSessionStartedInASubdirectory(t *testing.T) {
	h := jumpable(t)
	billing := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	inside := filepath.Join(billing, "internal", "handlers")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	tmuxOn(t, h.Socket, "new-session", "-d", "-s", "service-billing", "-c", inside, paneCommand)
	running := panePIDInSession(t, h.Socket, "service-billing")

	if err := h.Open(billing); err != nil {
		t.Fatalf("Open(%q): %v", billing, err)
	}

	if session, _ := workingClientShows(t, h); session != "service-billing" {
		t.Errorf("the working client shows %q, want the Session already running in the repo", session)
	}
	if got := panePIDInSession(t, h.Socket, "service-billing"); got != running {
		t.Errorf("the repo's Session was replaced (pane %d, was %d)", got, running)
	}
}

// A repo whose name was taken keeps the qualified Session it was given. Handing
// it the short name back once the repo it collided with has gone would strand
// that Session under a name nothing will ever ask for again — with your shell
// history and whatever you left running in it.
func TestOpenKeepsAReposQualifiedSessionAfterTheCollisionGoes(t *testing.T) {
	h := jumpable(t)
	root := t.TempDir()
	first := initRepo(t, filepath.Join(root, "acme", "api"))
	second := initRepo(t, filepath.Join(root, "globex", "api"))
	if err := h.Open(first); err != nil {
		t.Fatalf("Open(%q): %v", first, err)
	}
	if err := h.Open(second); err != nil {
		t.Fatalf("Open(%q): %v", second, err)
	}
	qualified, _ := workingClientShows(t, h)
	if qualified == "api" {
		t.Fatalf("the second repo took the unqualified name, so there is no collision to clear")
	}

	// The repo that held the short name goes away.
	tmuxOn(t, h.Socket, "kill-session", "-t", "=api")

	if err := h.Open(second); err != nil {
		t.Fatalf("Open(%q): %v", second, err)
	}

	if got, _ := workingClientShows(t, h); got != qualified {
		t.Errorf("the working client shows %q, want the repo's own Session %q", got, qualified)
	}
}

// Opening a repo steers the working client and nothing else: the Dashboard
// goes on running in its own client, which is the whole point of the dock.
func TestOpenLeavesTheDashboardWhereItIs(t *testing.T) {
	h := jumpable(t)
	billing := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))

	if err := h.Open(billing); err != nil {
		t.Fatalf("Open(%q): %v", billing, err)
	}

	clients := tmuxOn(t, h.Socket, "list-clients", "-F", "#{client_session}")
	if !strings.Contains(clients, topology.DashboardSession) {
		t.Errorf("the Dashboard client is showing %q, want it still on the Dashboard", clients)
	}
}
