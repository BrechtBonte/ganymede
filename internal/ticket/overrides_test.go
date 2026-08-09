package ticket_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// git runs a git command in dir, failing the test if it does not work.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	// The identity is supplied here so the test does not depend on whoever is
	// running it having configured one.
	full := append([]string{"-C", dir, "-c", "user.email=test@example.com", "-c", "user.name=Test"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mainRoot makes dir a repository with a commit in it — enough for a branch or
// a worktree to be made from — and returns it.
func mainRoot(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// state is a harness state file in a directory of the test's own.
func state(t *testing.T) config.Sidecar {
	t.Helper()
	return config.Sidecar{Path: filepath.Join(t.TempDir(), "ganymede", "state.json")}
}

// loaded is the overrides the state file holds.
func loaded(t *testing.T, sidecar config.Sidecar) *ticket.Overrides {
	t.Helper()
	overrides, err := ticket.Load(sidecar)
	if err != nil {
		t.Fatalf("load the overrides: %v", err)
	}
	return overrides
}

// set records an override, failing the test if it could not be kept.
func set(t *testing.T, overrides *ticket.Overrides, at ticket.Checkout, key ticket.Key) {
	t.Helper()
	if err := overrides.Set(at, key); err != nil {
		t.Fatalf("set the ticket: %v", err)
	}
}

// on is the checkout a Session on branch in root is working in.
func on(root, branch string) ticket.Checkout {
	return ticket.Checkout{Root: root, Dir: root, Branch: branch}
}

// The reason the override is kept in harness state at all rather than against
// the Session: a Session ends every day, and the correction you made about the
// branch it was working on is about the branch.
func TestTicketSetByHandOutlivesTheSession(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	sidecar := state(t)

	set(t, loaded(t, sidecar), on(root, "main"), "FIRE-2841")

	// The next run of the harness, reading the same state file.
	got, ok := loaded(t, sidecar).Of(on(root, "main"))
	if !ok || got != "FIRE-2841" {
		t.Errorf("Of(main) = %q, %v; want %q, true", got, ok, ticket.Key("FIRE-2841"))
	}
}

// Correcting a ticket is the same gesture as setting one, and clearing it is
// setting it to nothing: the branch goes back to speaking for itself.
func TestTicketSetByHandCanBeCorrectedAndCleared(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	sidecar := state(t)
	overrides := loaded(t, sidecar)

	set(t, overrides, on(root, "main"), "FIRE-2841")
	set(t, overrides, on(root, "main"), "CORE-119")
	if got, ok := overrides.Of(on(root, "main")); !ok || got != "CORE-119" {
		t.Errorf("Of(main) = %q, %v; want the correction %q", got, ok, ticket.Key("CORE-119"))
	}

	set(t, overrides, on(root, "main"), "")
	if got, ok := loaded(t, sidecar).Of(on(root, "main")); ok {
		t.Errorf("Of(main) = %q, true; want the branch to speak for itself again", got)
	}
}

// An override belongs to its branch, so it goes when the branch does — merged
// and deleted, or a worktree cleaned up on the way out. Nothing else would ever
// clear it: the branch it was keyed by is not coming back to be corrected.
func TestOverrideGoesWithTheBranchItWasSetOn(t *testing.T) {
	root := mainRoot(t, filepath.Join(t.TempDir(), "service-ai-assistant"))
	git(t, root, "branch", "feat/paging")
	sidecar := state(t)

	set(t, loaded(t, sidecar), on(root, "feat/paging"), "FIRE-2841")
	git(t, root, "branch", "-D", "feat/paging")

	if got, ok := loaded(t, sidecar).Of(on(root, "feat/paging")); ok {
		t.Errorf("Of(feat/paging) = %q, true; want the override gone with its branch", got)
	}
}

// A Session working somewhere with no branch to key an override by — a Main
// root with a PR checked out by hash, a directory outside any repository — is
// keyed by the directory instead, and forgotten when that goes.
func TestOverrideGoesWithTheDirectoryWhenThereIsNoBranch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar := state(t)
	detached := ticket.Checkout{Root: dir, Dir: dir}

	set(t, loaded(t, sidecar), detached, "FIRE-2841")
	if got, ok := loaded(t, sidecar).Of(detached); !ok || got != "FIRE-2841" {
		t.Fatalf("Of(%s) = %q, %v; want %q while the directory is there", dir, got, ok, ticket.Key("FIRE-2841"))
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if got, ok := loaded(t, sidecar).Of(detached); ok {
		t.Errorf("Of(%s) = %q, true; want the override gone with its directory", dir, got)
	}
}

// Eviction only ever acts on what the harness can see for itself. A repo it
// cannot ask — one that is not a checkout at the moment it looks, one whose git
// will not run — has not told it the branch is gone, and throwing away a
// correction on that would be the harness guessing with somebody's work.
func TestOverrideSurvivesARepoTheHarnessCannotAsk(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-ai-assistant")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar := state(t)

	set(t, loaded(t, sidecar), on(root, "feat/paging"), "FIRE-2841")

	if got, ok := loaded(t, sidecar).Of(on(root, "feat/paging")); !ok || got != "FIRE-2841" {
		t.Errorf("Of(feat/paging) = %q, %v; want %q kept", got, ok, ticket.Key("FIRE-2841"))
	}
}

// The key is the repo and the branch together. Every repo has a main, and one
// override on it would otherwise be an override on all of them.
func TestOverridesAreKeptPerRepoAndBranch(t *testing.T) {
	dir := t.TempDir()
	billing := mainRoot(t, filepath.Join(dir, "service-billing"))
	assistant := mainRoot(t, filepath.Join(dir, "service-ai-assistant"))
	git(t, assistant, "checkout", "-q", "-b", "spike")
	sidecar := state(t)

	overrides := loaded(t, sidecar)
	set(t, overrides, on(billing, "main"), "FIRE-2841")
	set(t, overrides, on(assistant, "main"), "CORE-119")
	set(t, overrides, on(assistant, "spike"), "CORE-120")

	for _, c := range []struct {
		at   ticket.Checkout
		want ticket.Key
	}{
		{on(billing, "main"), "FIRE-2841"},
		{on(assistant, "main"), "CORE-119"},
		{on(assistant, "spike"), "CORE-120"},
	} {
		if got, ok := overrides.Of(c.at); !ok || got != c.want {
			t.Errorf("Of(%s on %s) = %q, %v; want %q", c.at.Branch, filepath.Base(c.at.Root), got, ok, c.want)
		}
	}
}
