package dashboard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// caution is the mark the rail draws in front of whatever a Main root's
// checkout is carrying, as the eye reads it.
const caution = "⚠"

// git runs a git command in dir, failing the test if it does not work. The
// identity is supplied here so the test does not depend on whoever is running
// it having configured one.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.email=test@example.com", "-c", "user.name=Test"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mainRoot makes a repository with a commit in it — enough for a worktree to be
// spawned from — and returns the one name the root goes by. A root state is
// about which real checkout a Session has its hands on, so the Dashboard has to
// be shown real ones.
func mainRoot(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := exec.Command("mkdir", "-p", dir).Run(); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// worktree spawns one from root, the way `claude --worktree` does.
func worktree(t *testing.T, root, name string) string {
	t.Helper()
	at := filepath.Join(root, ".claude", "worktrees", name)
	git(t, root, "worktree", "add", "-q", "-b", name, at)
	return at
}

// strayed puts the checkout at root on a branch of its own, and leaves
// something uncommitted in it.
func strayed(t *testing.T, root, branch string) {
	t.Helper()
	git(t, root, "branch", "-M", "main")
	git(t, root, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(root, "toolbar.go"), []byte("package ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// opened is a Dashboard sized for the sidepanel, showing nothing yet.
func opened(t *testing.T) tea.Model {
	t.Helper()
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	return model
}

// rail is a Dashboard sized for the sidepanel showing sessions, with whatever
// it asked to have read off git read and handed back to it.
func rail(t *testing.T, sessions ...session.Session) tea.Model {
	t.Helper()
	model, cmd := opened(t).Update(dashboard.Sessions(sessions))
	return cautioned(t, model, cmd)
}

// cautioned runs what the Dashboard asked for and hands it back what git said,
// which is what the runtime does for it. Whatever else it asked for is left
// running: it also asks to be woken in half a minute, and no test can wait for
// that.
func cautioned(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	read := make(chan dashboard.Cautions, 1)
	var run func(tea.Cmd)
	run = func(ask tea.Cmd) {
		if ask == nil {
			return
		}
		go func() {
			switch msg := ask().(type) {
			case dashboard.Cautions:
				select {
				case read <- msg:
				default:
				}
			case tea.BatchMsg:
				for _, inner := range msg {
					run(inner)
				}
			}
		}()
	}
	run(cmd)

	select {
	case cautions := <-read:
		model, _ = model.Update(cautions)
	case <-time.After(10 * time.Second):
		t.Fatal("the Dashboard never asked git what the roots on the rail are carrying")
	}
	return model
}

// headerOf is the drawn line for a repo's header row.
func headerOf(t *testing.T, model tea.Model, root string) string {
	t.Helper()
	line, ok := lineWith(tree(model), filepath.Base(root))
	if !ok {
		t.Fatalf("no header row for %q:\n%s", root, tree(model))
	}
	return line
}

// A Session working in the Main root holds it, whatever it is doing: an Idle
// agent still has the context it built up in that checkout. The rail says so on
// the repo's own row, because a PR is checked out in the Main root and whether
// anything is in the way should never be a keystroke away.
func TestRepoHeaderMarksAMainRootASessionIsWorkingIn(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	model := sidepanel(&jumps{}, live("ai-assistant-b3", root, session.Idle))

	if line := headerOf(t, model, root); !strings.Contains(line, repo.InUse.Glyph()) {
		t.Errorf("header = %q, want the mark of a root in use by an agent", line)
	}
}

// The cautions are about the checkout rather than about who has it, so they
// show on the root's row whatever state the root is in. Free says a PR can be
// checked out here; these say what is sitting in it first.
func TestRepoHeaderCarriesWhatTheMainRootIsCarrying(t *testing.T) {
	root := mainRoot(t, "billing")
	strayed(t, root, "toolbar")

	line := headerOf(t, rail(t, live("billing-a1", root, session.Idle)), root)

	if !strings.Contains(line, "toolbar") {
		t.Errorf("header = %q, want it to caution that the root is off its default branch", line)
	}
	if !strings.Contains(line, "dirty") {
		t.Errorf("header = %q, want it to caution that the root's tree is dirty", line)
	}
}

// A marker that only ever appears is decoration. Nothing reports a branch being
// checked out or a tree being cleaned — you do both in a shell, and the Session
// sits at its prompt through it — so the rail asks git again on its own clock,
// and the markers go when the reason for them has.
//
// The root here is Free the whole time, with only a Worktree session in the
// repo: the cautions are about the checkout, not about who has it.
func TestCautionsClearWhenTheMainRootIsPutBack(t *testing.T) {
	root := mainRoot(t, "billing")
	strayed(t, root, "toolbar")
	model := rail(t, live("FIRE-2841-paging", worktree(t, root, "FIRE-2841-paging"), session.Idle))
	if line := headerOf(t, model, root); !strings.Contains(line, caution) {
		t.Fatalf("header = %q, want it to caution about the root before it is put back", line)
	}

	git(t, root, "checkout", "-q", "main")
	if err := os.Remove(filepath.Join(root, "toolbar.go")); err != nil {
		t.Fatal(err)
	}
	model, cmd := model.Update(dashboard.Tick{})

	if line := headerOf(t, cautioned(t, model, cmd), root); strings.Contains(line, caution) {
		t.Errorf("header = %q, want nothing left to caution about", line)
	}
}

// The rail is forty columns wide, and the caution is the part of a header row
// that gives way. What it gives up is the branch name — that the root is off
// its default branch at all is the caution, and the box below names the branch
// — while a dirty tree said in five columns is either there or it is not.
func TestALongBranchGivesWayRatherThanOverflowingTheRail(t *testing.T) {
	root := mainRoot(t, "service-billing")
	strayed(t, root, "fix/FIRE-2841-max-paging-numbers-in-the-invoice-list")

	model := rail(t, live("billing-a1", root, session.Idle))

	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
	if line := headerOf(t, model, root); !strings.Contains(line, "dirty") {
		t.Errorf("header = %q, want the dirty tree to survive a branch name that could not", line)
	}
}

// A repo named at length leaves no room for a branch name at all. The row keeps
// the marks it can still be read by rather than saying half a branch — or, worse,
// the punctuation between two marks with nothing left on either side of it.
func TestARowWithNoRoomForTheBranchKeepsTheMarksItCanBeReadBy(t *testing.T) {
	root := mainRoot(t, "teamleadercrm-monolith-b7")
	strayed(t, root, "fix/FIRE-2841-max-paging-numbers-in-the-invoice-list")

	line := headerOf(t, rail(t, live("billing-a1", root, session.Idle)), root)

	if !strings.Contains(line, caution+" dirty") {
		t.Errorf("header = %q, want the marks that still fit, said plainly", line)
	}
}

// A repo named at the width of the whole rail leaves room for nothing else, and
// the mark is what the row gives up last. A caution dropped for want of room
// would leave a root that is detached and dirty reading exactly like one that is
// clean, on the row you go to precisely to find out which.
func TestARowWithNoRoomAtAllKeepsTheCautionMarkAndGivesUpTheName(t *testing.T) {
	root := mainRoot(t, "teamleadercrm-monolith-and-then-some-more")
	strayed(t, root, "toolbar")

	line, ok := lineWith(tree(rail(t, live("billing-a1", root, session.Idle))), "teamleadercrm-monolith")
	if !ok {
		t.Fatalf("no header row for %q", root)
	}
	if !strings.Contains(line, caution) {
		t.Errorf("header = %q, want the caution mark kept and the name given up for it", line)
	}
}

// A word cut in half says something the harness did not mean. Where there is no
// room for the whole of a mark, the row says the marks it can and leaves the
// rest to the box.
func TestARowNeverSaysHalfOfAMark(t *testing.T) {
	root := mainRoot(t, "teamleadercrm-monolith-invoicing")
	git(t, root, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(root, "toolbar.go"), []byte("package ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	line := headerOf(t, rail(t, live("billing-a1", root, session.Idle)), root)

	if strings.Contains(line, "dir") && !strings.Contains(line, "dirty") {
		t.Errorf("header = %q, want no half-said mark on it", line)
	}
}

// Two reads of the rail can land in the order they were asked in or the other
// one, and nothing here has a say in which. What comes back is laid over what
// the Dashboard already knows rather than put in its place, so that an answer
// about one root cannot take another root's caution off the rail with it.
func TestAnAnswerAboutOneRootLeavesTheOthersAlone(t *testing.T) {
	billing := mainRoot(t, "billing")
	strayed(t, billing, "toolbar")
	assistant := mainRoot(t, "assistant")
	strayed(t, assistant, "spike")

	model := rail(t,
		live("billing-a1", billing, session.Idle),
		live("assistant-b3", assistant, session.Idle))
	// An older read, about one of the two roots only, landing last.
	model, _ = model.Update(dashboard.Cautions{billing: repo.Caution{Branch: "toolbar"}})

	if line := headerOf(t, model, assistant); !strings.Contains(line, caution) {
		t.Errorf("header = %q, want the caution the other answer had already found", line)
	}
}

// Asking git what a root is carrying is the most expensive thing the Dashboard
// does, and the working set is rebuilt several times a second while an agent is
// working. It asks once and waits for the answer rather than asking again over
// the top of a question it has not had one to.
func TestTheDashboardDoesNotAskGitAgainWhileItIsAlreadyAsking(t *testing.T) {
	set := []session.Session{live("billing-a1", mainRoot(t, "billing"), session.Idle)}
	model := opened(t)

	model, asking := model.Update(dashboard.Sessions(set))
	if asking == nil {
		t.Fatal("the Dashboard never asked git about a root it has never asked about")
	}
	if _, again := model.Update(dashboard.Sessions(set)); again != nil {
		t.Error("the Dashboard asked git again while it was already asking")
	}
}

// A repo that arrived while git was being asked about the others is asked about
// as soon as that answer lands. Waiting for the tick would leave it on the rail
// without its cautions for half a minute — and the answer in flight was never
// about it.
func TestARootThatArrivedWhileAskingIsAskedAboutNext(t *testing.T) {
	billing := mainRoot(t, "billing")
	assistant := mainRoot(t, "assistant")
	model := opened(t)

	model, _ = model.Update(dashboard.Sessions([]session.Session{live("billing-a1", billing, session.Idle)}))
	model, _ = model.Update(dashboard.Sessions([]session.Session{
		live("billing-a1", billing, session.Idle),
		live("assistant-b3", assistant, session.Idle),
	}))
	_, asking := model.Update(dashboard.Cautions{billing: repo.Caution{}})

	if asking == nil {
		t.Error("the Dashboard never asked git about the root that arrived while it was asking")
	}
}

// The box under the rail is where a row says what it had no room for. A caution
// cut down to a mark and a word is one you have to go and check for yourself,
// so the box names the branch and says what is in the tree.
func TestSelectedBoxSpellsOutWhatTheMainRootIsCarrying(t *testing.T) {
	root := mainRoot(t, "service-billing")
	strayed(t, root, "fix/toolbar-focus")

	// The selection opens on the first row, which is the repo's header.
	box := detail(rail(t, live("billing-a1", root, session.Idle)))

	if !strings.Contains(box, "fix/toolbar-focus") {
		t.Errorf("SELECTED = %q, want it to name the branch the root strayed to", box)
	}
	if !strings.Contains(box, "uncommitted") {
		t.Errorf("SELECTED = %q, want it to say the tree has uncommitted work in it", box)
	}
}

// A mark in a column is a legend you have to have learned first. The box under
// the rail is where a row says what it had no room for, so a selected repo says
// its root's state in the words the vocabulary gives it.
func TestSelectedBoxNamesTheMainRootState(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	// The selection opens on the first row, which is the repo's header.
	model := sidepanel(&jumps{}, live("ai-assistant-b3", root, session.Idle))

	if box := detail(model); !strings.Contains(box, string(repo.InUse)) {
		t.Errorf("SELECTED = %q, want it to say the Main root is %q", box, repo.InUse)
	}
}

// A repo on the rail with nothing running in it at all is the plainest Free
// there is: you were working there yesterday, the Sessions have ended, and the
// root is yours to check a PR out in.
func TestRepoHeaderMarksAMainRootWithNoSessionsFree(t *testing.T) {
	root := mainRoot(t, "service-billing")
	state := remembering(t)
	worked(t, state, root, time.Now())

	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Activity: state})

	if line := headerOf(t, model, root); !strings.Contains(line, repo.Free.Glyph()) {
		t.Errorf("header = %q, want the mark of a free root", line)
	}
}

// A Worktree session is spawned so that the Main root is left alone, and the
// rail has to agree with that or the whole flow is pointless. The worktree
// lives inside the root's own directory, so a rail that went by paths would
// call every repo with a background session in use and never let a PR be
// checked out anywhere.
func TestRepoHeaderLeavesAMainRootWithOnlyAWorktreeSessionFree(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	model := sidepanel(&jumps{}, live("FIRE-2841-paging", worktree(t, root, "FIRE-2841-paging"), session.Idle))

	if line := headerOf(t, model, root); !strings.Contains(line, repo.Free.Glyph()) {
		t.Errorf("header = %q, want the mark of a free root", line)
	}
}

// A root you have reserved and nobody is holding shows the Claimed mark,
// whatever else is or is not running in the repo.
func TestRepoHeaderMarksAClaimedRootWithItsGlyph(t *testing.T) {
	root := mainRoot(t, "billing")
	fake := &claims{}
	if err := fake.Claim(root, "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Claimer: fake})

	if line := headerOf(t, model, root); !strings.Contains(line, repo.Claimed.Glyph()) {
		t.Errorf("header = %q, want the mark of a claimed root", line)
	}
}

// A repo with nothing running in it stays on the rail while its root is
// Claimed — that is the whole point of reserving it ahead of a review that
// has not started yet.
func TestAClaimedRootWithNoSessionsStaysOnTheRail(t *testing.T) {
	root := mainRoot(t, "billing")
	fake := &claims{}
	if err := fake.Claim(root, ""); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Claimer: fake})

	if _, ok := lineWith(tree(model), "billing"); !ok {
		t.Errorf("no header row for the Claimed repo:\n%s", tree(model))
	}
}

// A live occupant always outranks a Claim: the root's row must never say
// Claimed — implying it is safe to check a PR out in — while an agent is
// actually sitting in it.
func TestALiveOccupantOutranksAClaimOnTheHeaderRow(t *testing.T) {
	root := mainRoot(t, "billing")
	fake := &claims{}
	if err := fake.Claim(root, "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Claimer: fake}, live("billing-a1", root, session.Idle))

	if line := headerOf(t, model, root); !strings.Contains(line, repo.InUse.Glyph()) {
		t.Errorf("header = %q, want the mark of a root in use by an agent even though it is Claimed", line)
	}
}

// The detail box names the note a Claim was made with — the row has no room
// for it.
func TestSelectedBoxNamesTheClaimNote(t *testing.T) {
	root := mainRoot(t, "billing")
	fake := &claims{}
	if err := fake.Claim(root, "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	box := detail(dashboardOn(dashboard.Harness{Jumper: &jumps{}, Claimer: fake}))

	if !strings.Contains(box, "reviewing PR #4123") {
		t.Errorf("SELECTED = %q, want the note the root was claimed with", box)
	}
}

// railOn is a Dashboard sized for the sidepanel, wired to harness and shown
// the working set on sessions, with whatever it asked to have read off git
// read and handed back to it — rail's own reach, but over a harness the
// caller has already put something else on (a Claimer, an Activity).
func railOn(t *testing.T, harness dashboard.Harness, sessions ...session.Session) tea.Model {
	t.Helper()
	if harness.Jumper == nil {
		harness.Jumper = &jumps{}
	}
	var model tea.Model = dashboard.New(nil, harness)
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, cmd := model.Update(dashboard.Sessions(sessions))
	return cautioned(t, model, cmd)
}

// A Claimed root back on its default branch with nothing uncommitted in it
// is exactly what a review being wrapped up looks like — the Dashboard
// nudges you to let it go rather than leaving it reserved forever by
// accident.
func TestReleaseNudgeAppearsWhenAClaimedRootIsCleanOnDefault(t *testing.T) {
	root := mainRoot(t, "billing")
	fake := &claims{}
	if err := fake.Claim(root, "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	box := detail(railOn(t, dashboard.Harness{Claimer: fake}))

	if !strings.Contains(box, "release?") {
		t.Errorf("SELECTED = %q, want the release nudge for a clean root on its default branch", box)
	}
}

// Before git has actually been asked about a Claimed root — the very first
// frame after it arrives, restart included — the release nudge must not
// fire: an unasked root and a clean one both carry the zero Caution, and
// only the first of those is a guess.
func TestNoReleaseNudgeBeforeGitHasBeenAskedAboutTheRoot(t *testing.T) {
	fake := &claims{}
	if err := fake.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// dashboardOn never runs the caution-read round trip rail/railOn do, so
	// the root's caution is exactly as unasked as it is on the first frame.
	box := detail(dashboardOn(dashboard.Harness{Jumper: &jumps{}, Claimer: fake}))

	if strings.Contains(box, "release?") {
		t.Errorf("SELECTED = %q, want no release nudge before the caution has actually been read", box)
	}
}

// A Claimed root still off its default branch, or still dirty, gets no
// release nudge — the caution already says why letting go of it now would
// be premature.
func TestReleaseNudgeDoesNotAppearWhileTheRootCarriesACaution(t *testing.T) {
	root := mainRoot(t, "billing")
	strayed(t, root, "toolbar")
	fake := &claims{}
	if err := fake.Claim(root, "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	box := detail(railOn(t, dashboard.Harness{Claimer: fake}))

	if strings.Contains(box, "release?") {
		t.Errorf("SELECTED = %q, want no release nudge while the root carries a caution", box)
	}
}
