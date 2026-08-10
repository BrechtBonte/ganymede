package popup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/popup"
)

// The same directory always finds its way back to the same hidden session —
// that persistence is the whole point of the Popup shell (§8): closing hides
// rather than kills, and reopening has to land on the session that was hidden.
func TestOwnerNameIsStableForTheSameDirectory(t *testing.T) {
	first := popup.OwnerName("/repos/service-billing")
	second := popup.OwnerName("/repos/service-billing")
	if first != second {
		t.Errorf("OwnerName = %q then %q, want the same name both times", first, second)
	}
}

// Two different directories must never collide on one hidden session — that
// would hand one owner's scrollback and running command to another's popup.
func TestOwnerNameDiffersAcrossDirectories(t *testing.T) {
	billing := popup.OwnerName("/repos/service-billing")
	assistant := popup.OwnerName("/repos/service-ai-assistant")
	if billing == assistant {
		t.Errorf("OwnerName gave both repos %q", billing)
	}
}

// tmux splits a target on "." and ":" before it ever reaches the exact-match
// prefix (see topology.WorkingSessionName), and a name carrying either could
// be created and then never addressed again.
func TestOwnerNameIsAddressable(t *testing.T) {
	for _, dir := range []string{
		"/repos/service-billing",
		"/repos/my repo",
		"/repos/.worktrees/FIRE-2841:paging",
	} {
		name := popup.OwnerName(dir)
		for _, forbidden := range []byte{'.', ':'} {
			for i := 0; i < len(name); i++ {
				if name[i] == forbidden {
					t.Errorf("OwnerName(%q) = %q, want no %q in it", dir, name, string(forbidden))
				}
			}
		}
	}
}

// A window on a repo's own Session opens the popup in the pane it was pressed
// from — the ordinary case, and the only one that matters off the rail.
func TestTargetDirOffTheRailIsThePanesOwnDirectory(t *testing.T) {
	got := popup.TargetDir("service-billing", "ganymede", "/repos/service-billing", "/repos/service-ai-assistant")
	if got != "/repos/service-billing" {
		t.Errorf("TargetDir = %q, want the pane's own directory", got)
	}
}

// Focus on the rail has no Session of its own under the cursor — the popup
// belongs to whichever repo the Dashboard has selected instead (§8).
func TestTargetDirOnTheRailIsTheDashboardsSelection(t *testing.T) {
	got := popup.TargetDir("ganymede", "ganymede", "/home/ganymede-checkout", "/repos/service-ai-assistant")
	if got != "/repos/service-ai-assistant" {
		t.Errorf("TargetDir = %q, want the Dashboard's selected repo", got)
	}
}

// The Dashboard has not always said anything yet — an option nothing has
// written is empty, not a directory — so the rail falls back to its own pane
// rather than opening a popup in nothing.
func TestTargetDirOnTheRailWithNoSelectionYetFallsBackToThePane(t *testing.T) {
	got := popup.TargetDir("ganymede", "ganymede", "/home/ganymede-checkout", "")
	if got != "/home/ganymede-checkout" {
		t.Errorf("TargetDir = %q, want the Dashboard's own pane with nothing selected yet", got)
	}
}

// A directory reached through a symlink and the same directory reached by
// its real path are one owner, not two — tmux always reports a pane's path
// resolved, while a caller that never asked the OS may still be holding the
// symlinked spelling.
func TestOwnerNameIsTheSameThroughASymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if got, want := popup.OwnerName(link), popup.OwnerName(real); got != want {
		t.Errorf("OwnerName(%q) = %q, want the same owner as OwnerName(%q) = %q", link, got, real, want)
	}
}

// IsOwnerName is how a sweep tells its own hidden sessions apart from
// anything else that might end up sharing the popup socket.
func TestIsOwnerNameAcceptsWhatOwnerNameProduces(t *testing.T) {
	if !popup.IsOwnerName(popup.OwnerName("/repos/service-billing")) {
		t.Error("IsOwnerName rejected a name OwnerName itself produced")
	}
}

// A name popup.OwnerName never produced belongs to someone else, whatever
// it is called.
func TestIsOwnerNameRejectsAnythingElse(t *testing.T) {
	for _, name := range []string{"service-billing", "ganymede", "debug-session", "ganymede-popupsomething"} {
		if popup.IsOwnerName(name) {
			t.Errorf("IsOwnerName(%q) = true, want false", name)
		}
	}
}
