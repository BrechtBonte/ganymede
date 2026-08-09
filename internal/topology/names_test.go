package topology_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// initRepo makes dir a git repository and returns it.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// One tmux session per repo, named after the repo — so working anywhere inside
// a repo lands in that repo's Session.
func TestWorkingSessionIsNamedAfterTheRepo(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	sub := filepath.Join(repo, "src", "handlers")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := topology.WorkingSessionName(sub)
	if err != nil {
		t.Fatalf("WorkingSessionName: %v", err)
	}
	if want := "service-ai-assistant"; got != want {
		t.Errorf("WorkingSessionName(%q) = %q, want %q", sub, got, want)
	}
}

// The Dashboard's session name is reserved. A repo that happens to be called
// ganymede must not steer the working client onto the Dashboard itself.
func TestRepoNamedGanymedeDoesNotTakeTheDashboardSessionName(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "ganymede"))

	got, err := topology.WorkingSessionName(repo)
	if err != nil {
		t.Fatalf("WorkingSessionName: %v", err)
	}
	if got == topology.DashboardSession {
		t.Fatalf("working session took the reserved Dashboard name %q", got)
	}
	if want := "ganymede-repo"; got != want {
		t.Errorf("WorkingSessionName(%q) = %q, want %q", repo, got, want)
	}
}

// Not every directory worth opening is a repo checkout.
func TestDirectoryOutsideAnyRepoStillGetsASession(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := topology.WorkingSessionName(dir)
	if err != nil {
		t.Fatalf("WorkingSessionName: %v", err)
	}
	if want := "notes"; got != want {
		t.Errorf("WorkingSessionName(%q) = %q, want %q", dir, got, want)
	}
}

// tmux parses "." and ":" in a target as the window and pane separators, even
// under the "=" exact-match prefix, so a session carrying them can be created
// but never addressed again.
func TestSessionNamesStayAddressableByTmux(t *testing.T) {
	for _, dir := range []string{"site.com", "next.js", "repo:one"} {
		repo := initRepo(t, filepath.Join(t.TempDir(), dir))

		name, err := topology.WorkingSessionName(repo)
		if err != nil {
			t.Fatalf("WorkingSessionName(%q): %v", dir, err)
		}
		if strings.ContainsAny(name, ".:") {
			t.Errorf("WorkingSessionName(%q) = %q, which tmux cannot target", dir, name)
		}
	}
}
