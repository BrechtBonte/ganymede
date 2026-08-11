package dashboard_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	"github.com/BrechtBonte/ganymede/internal/workingset"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// opens records the repos the Dashboard asked to be taken to, standing in for
// the harness bringing a repo's Session up and steering the working client.
type opens struct {
	dirs []string
	err  error
}

func (o *opens) Open(dir string) error {
	o.dirs = append(o.dirs, dir)
	return o.err
}

// stock is an inventory of repos the picker can offer.
type stock struct {
	repos []string
	err   error
}

func (s stock) Repos() ([]string, error) { return s.repos, s.err }

// remembering is the real harness state, on a file the test owns: what the
// working set is made of has to survive a restart, and a stand-in that only
// held a map would never show that.
func remembering(t *testing.T) *workingset.Activity {
	t.Helper()
	return reading(t, config.Sidecar{Path: filepath.Join(t.TempDir(), "state.json")})
}

// reading is the activity kept in one particular state file, so that a test
// can put the Dashboard down and pick it up again over the same one.
func reading(t *testing.T, state config.Sidecar) *workingset.Activity {
	t.Helper()
	activity, err := workingset.Load(state)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return activity
}

// worked records that you were in root at at, failing the test if the harness
// could not write it down.
func worked(t *testing.T, activity *workingset.Activity, root string, at time.Time) {
	t.Helper()
	if err := activity.Touch(root, at); err != nil {
		t.Fatalf("Touch(%q): %v", root, err)
	}
}

// dashboardOn is a Dashboard sized for the sidepanel, wired to harness and
// shown one working set.
func dashboardOn(harness dashboard.Harness, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, harness)
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// typing sends each character of s to the Dashboard as a keystroke.
func typing(model tea.Model, s string) tea.Model {
	for _, r := range s {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

// picking opens the picker and lets the scan it asks for come back.
func picking(t *testing.T, model tea.Model) tea.Model {
	t.Helper()
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd == nil {
		t.Fatal("pressing g asked for no inventory scan")
	}
	model, _ = model.Update(cmd())
	return model
}

// A repo you were working in this morning is still where you are, even with
// nothing running in it — that is the whole difference between the working set
// and a list of live Sessions.
func TestRepoWorkedInRecentlyIsOnTheDashboardWithNoSessions(t *testing.T) {
	state := remembering(t)
	worked(t, state, "/repos/service-billing", time.Now().Add(-2*time.Hour))

	view := tree(dashboardOn(dashboard.Harness{Activity: state}))

	if _, ok := lineWith(view, "service-billing"); !ok {
		t.Errorf("no row for a repo worked in two hours ago:\n%s", view)
	}
}

// And a repo you have not touched in longer than the window is one the
// Dashboard stops spending a row on. It is still a keystroke away in the
// picker.
func TestRepoNotWorkedInSinceTheWindowIsNotOnTheDashboard(t *testing.T) {
	state := remembering(t)
	worked(t, state, "/repos/archived", time.Now().Add(-workingset.Window-time.Hour))

	view := tree(dashboardOn(dashboard.Harness{Activity: state}))

	if _, ok := lineWith(view, "archived"); ok {
		t.Errorf("a repo past the recency window is still on the Dashboard:\n%s", view)
	}
}

// A live Session keeps its repo on the Dashboard whatever the sidecar
// remembers, because that is where you are working now.
func TestRepoWithALiveSessionIsOnTheDashboardHoweverOldItsActivity(t *testing.T) {
	state := remembering(t)
	worked(t, state, "/repos/service-billing", time.Now().Add(-30*24*time.Hour))

	view := tree(dashboardOn(dashboard.Harness{Activity: state},
		live("service-billing-a1", "/repos/service-billing", session.Idle)))

	if _, ok := lineWith(view, "service-billing-a1"); !ok {
		t.Errorf("no row for a live Session in a long-untouched repo:\n%s", view)
	}
}

// Repos with something running in them are what the Dashboard is for. A quiet
// repo is on it because you were there, and belongs under them.
func TestQuietReposSortBelowReposWithSessions(t *testing.T) {
	state := remembering(t)
	// Named so that sorting by anything but urgency would put it first.
	worked(t, state, "/repos/aaa-quiet", time.Now())

	view := tree(dashboardOn(dashboard.Harness{Activity: state},
		live("zzz-assistant", "/repos/zzz-busy", session.Working)))

	quiet := strings.Index(view, "aaa-quiet")
	busy := strings.Index(view, "zzz-busy")
	if quiet < 0 || busy < 0 {
		t.Fatalf("both repos should be on the Dashboard:\n%s", view)
	}
	if quiet < busy {
		t.Errorf("the quiet repo sorts above the one with a Session:\n%s", view)
	}
}

// Working in a repo is what keeps it on the Dashboard after its Sessions end,
// so every working set that arrives has to stamp the repos in it.
func TestReposWithLiveSessionsAreRecordedAsWorkedIn(t *testing.T) {
	state := remembering(t)

	dashboardOn(dashboard.Harness{Activity: state},
		live("service-billing-a1", "/repos/service-billing", session.Idle))

	if _, known := state.Active()["/repos/service-billing"]; !known {
		t.Errorf("a repo with a live Session was not recorded as worked in: %v", state.Active())
	}
}

// A Dashboard you have just glanced at should be describing whatever is most
// urgent, not whichever remembered repo happens to sort first.
func TestTheSelectionStartsOnTheMostUrgentRow(t *testing.T) {
	state := remembering(t)
	// Named so that any ordering but urgency would put it first.
	worked(t, state, "/repos/aaa-quiet", time.Now())

	model := dashboardOn(dashboard.Harness{Activity: state},
		live("zzz-assistant", "/repos/zzz-busy", session.Blocked))

	if _, ok := lineWith(detail(model), "zzz-busy"); !ok {
		t.Errorf("the SELECTED box describes something other than the most urgent repo:\n%s", drawn(model))
	}
}

// Once you have put the cursor somewhere it stays on that row, however far the
// row moves as Sessions come and go.
func TestTheSelectionYouMadeSurvivesTheTreeBeingRebuilt(t *testing.T) {
	state := remembering(t)
	model := dashboardOn(dashboard.Harness{Activity: state},
		live("service-billing-a1", "/repos/service-billing", session.Idle))
	// Down twice: past the first repo's header onto its Session.
	model = press(press(model, tea.KeyDown), tea.KeyDown)
	chosen := detail(model)

	// A Session arriving in another repo that outranks it, pushing the row down.
	model, _ = model.Update(dashboard.Sessions([]session.Session{
		live("service-billing-a1", "/repos/service-billing", session.Idle),
		live("ai-assistant-b3", "/repos/aaa-assistant", session.Blocked),
	}))

	if got := detail(model); got != chosen {
		t.Errorf("the selection moved off the row it was put on:\ngot:\n%s\nwant:\n%s", got, chosen)
	}
}

// Enter on a repo's own row takes you to that repo. There may be nothing
// running in it, so this is the repo-shaped jump rather than the Session one.
func TestEnterOnARepoRowOpensTheRepo(t *testing.T) {
	state := remembering(t)
	worked(t, state, "/repos/service-billing", time.Now())
	opener := &opens{}
	model := dashboardOn(dashboard.Harness{Opener: opener, Activity: state})

	model = press(model, tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/service-billing" {
		t.Errorf("Enter on a repo row opened %v, want /repos/service-billing", opener.dirs)
	}
}

// The picker is the way to everything the Dashboard is not showing.
func TestGOffersTheWholeInventory(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/acme/api", "/repos/ganymede"}},
	}))

	view := drawn(model)
	for _, repo := range []string{"api", "ganymede"} {
		if _, ok := lineWith(view, repo); !ok {
			t.Errorf("the picker does not offer %q:\n%s", repo, view)
		}
	}
}

// Fuzzy, not prefix: the point of the picker is that a few letters from
// anywhere in a repo's name are enough to reach it.
func TestTypingNarrowsThePickerFuzzily(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/service-billing", "/repos/ganymede"}},
	}))

	view := drawn(typing(model, "gnm"))

	if _, ok := lineWith(view, "ganymede"); !ok {
		t.Errorf("\"gnm\" does not reach ganymede:\n%s", view)
	}
	if _, ok := lineWith(view, "service-billing"); ok {
		t.Errorf("\"gnm\" left an unrelated repo on show:\n%s", view)
	}
}

// A repo whose own name matches is what you meant; one that only matches
// somewhere up its path is a fallback, and must not be offered above it.
func TestNameMatchesRankAboveMatchesInThePath(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/ganymede/service-billing", "/repos/acme/ganymede"}},
	}))

	view := drawn(typing(model, "ganymede"))

	// Each repo is read by the directory it is filed under, which is the one
	// part of either row the typed query is not also sitting in.
	named := strings.Index(view, "acme")
	if named < 0 {
		t.Fatalf("the picker does not offer the repo actually called ganymede:\n%s", view)
	}
	if pathed := strings.Index(view, "service-billing"); pathed >= 0 && pathed < named {
		t.Errorf("a repo matching only in its path is offered first:\n%s", view)
	}
}

// Picking a repo does both halves of what the ticket asks for: it takes you
// there, and it puts the repo on the Dashboard.
func TestPickingARepoOpensItAndPutsItOnTheDashboard(t *testing.T) {
	state := remembering(t)
	opener := &opens{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    opener,
		Activity:  state,
		Inventory: stock{repos: []string{"/repos/service-billing", "/repos/ganymede"}},
	}))

	model = press(typing(model, "gany"), tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/ganymede" {
		t.Fatalf("picking opened %v, want /repos/ganymede", opener.dirs)
	}
	if _, known := state.Active()["/repos/ganymede"]; !known {
		t.Errorf("the picked repo was not recorded as worked in: %v", state.Active())
	}
	view := tree(model)
	if _, ok := lineWith(view, "ganymede"); !ok {
		t.Errorf("the picked repo is not on the Dashboard:\n%s", view)
	}
}

// Picking a repo is Enter too: the same Focus that follows a jump follows a
// pick, so choosing a repo out of the picker lands you in it as well.
func TestPickingARepoFocusesTheWorkingClient(t *testing.T) {
	focuser := &focuses{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    &opens{},
		Focuser:   focuser,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model = press(typing(model, "gany"), tea.KeyEnter)

	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after picking a repo", focuser.n)
	}
}

// The Dashboard is the harness's memory of where you have been, so a repo you
// picked has to still be there after a restart — the only reason the sidecar
// is a file at all.
func TestAPickedRepoIsStillOnTheDashboardAfterARestart(t *testing.T) {
	file := config.Sidecar{Path: filepath.Join(t.TempDir(), "state.json")}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    &opens{},
		Activity:  reading(t, file),
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))
	press(model, tea.KeyEnter)

	// A second Dashboard over the same state file — the harness started again.
	view := tree(dashboardOn(dashboard.Harness{Activity: reading(t, file)}))

	if _, ok := lineWith(view, "ganymede"); !ok {
		t.Errorf("the picked repo did not survive the restart:\n%s", view)
	}
}

// Closing the picker leaves the Dashboard exactly as it was — the picker is a
// way to look at the inventory, not a mode you can get stuck in.
func TestEscapeClosesThePicker(t *testing.T) {
	opener := &opens{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    opener,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}, live("service-billing-a1", "/repos/service-billing", session.Idle)))

	model = press(typing(model, "gany"), tea.KeyEsc)

	if _, ok := lineWith(tree(model), "service-billing-a1"); !ok {
		t.Errorf("the Dashboard did not come back after Escape:\n%s", drawn(model))
	}
	if len(opener.dirs) != 0 {
		t.Errorf("Escape opened %v", opener.dirs)
	}
}

// A query that reaches nothing says so, rather than showing an empty box that
// reads as a picker which has broken.
func TestPickerSaysWhenNothingMatches(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	view := strings.ToLower(drawn(typing(model, "zzzz")))

	if !strings.Contains(view, "no repo") {
		t.Errorf("the picker does not say that nothing matches:\n%s", view)
	}
}

// An inventory that cannot be read is worth saying out loud: the picker is the
// only way to the repos the Dashboard is not showing, and an empty one looks
// exactly like a machine with no repos on it.
func TestPickerSaysWhenTheInventoryCannotBeRead(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{err: errors.New("scan /Users/brecht/Projects for repositories: permission denied")},
	}))

	if !strings.Contains(drawn(model), "permission denied") {
		t.Errorf("the picker does not report why it has nothing to offer:\n%s", drawn(model))
	}
}

// A repo the harness could not take you to leaves you where you were, and says
// why — the same bargain a jump that cannot be made strikes.
func TestARepoThatCannotBeOpenedSaysSo(t *testing.T) {
	opener := &opens{err: errors.New("no window is showing the harness to jump in")}
	model := picking(t, dashboardOn(dashboard.Harness{
		Opener:    opener,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model = press(model, tea.KeyEnter)

	if !strings.Contains(drawn(model), "no window is showing the harness") {
		t.Errorf("the Dashboard does not say why it could not take you there:\n%s", drawn(model))
	}
}

// Everything Enter does is done to whatever the cursor is on, and the tree
// re-sorts itself under your hands every time a Session changes state. A
// cursor that stayed at a position rather than on a row would open a repo you
// had never looked at — the one thing a jump must never do.
func TestTheSelectionHoldsItsRowEvenBeforeYouHaveMovedIt(t *testing.T) {
	state := remembering(t)
	opener := &opens{}
	model := dashboardOn(dashboard.Harness{Opener: opener, Activity: state},
		live("acme-a1", "/repos/acme", session.Blocked))
	if _, ok := lineWith(detail(model), "acme"); !ok {
		t.Fatalf("the cursor does not start on the only repo:\n%s", drawn(model))
	}

	// A Session that has been Blocked longer arrives in another repo, which
	// sorts that repo above the one the cursor is on.
	older := session.Session{PID: 99, ID: "b", Dir: "/repos/aaa-globex", Name: "globex-b2",
		State: session.Blocked, Since: epoch.Add(-time.Hour)}
	model, _ = model.Update(dashboard.Sessions([]session.Session{
		live("acme-a1", "/repos/acme", session.Blocked), older,
	}))
	model = press(model, tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/acme" {
		t.Errorf("Enter opened %v, want the repo the cursor was left on (/repos/acme)", opener.dirs)
	}
}

// The scan lands on its own schedule. A repo cloned since the last one can
// sort above the line you are reading, so the cursor has to keep the repo it
// is on rather than the row.
func TestARescanKeepsThePickerOnTheRepoYouWereLookingAt(t *testing.T) {
	opener := &opens{}
	inventory := stock{repos: []string{"/repos/aaa", "/repos/bbb"}}
	model := picking(t, dashboardOn(dashboard.Harness{Opener: opener, Inventory: inventory}))
	model = press(model, tea.KeyDown)

	// A repo discovered since, sorting above the highlighted one.
	model, _ = model.Update(dashboard.Discovered{Repos: []string{"/repos/aaa", "/repos/abb", "/repos/bbb"}})
	model = press(model, tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/bbb" {
		t.Errorf("picking opened %v, want the repo the cursor was on (/repos/bbb)", opener.dirs)
	}
}

// Every repo under the scan roots shares the path to them. Matching a query
// against the whole path would let any letters that turn up in the home
// directory reach the entire inventory, which is the narrowing the picker is
// for.
func TestThePathTheReposShareDoesNotMatchEverything(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{
			"/Users/brecht/Projects/acme/api",
			"/Users/brecht/Projects/globex/web",
		}},
	}))

	view := strings.ToLower(drawn(typing(model, "brt")))

	for _, repo := range []string{"api", "web"} {
		if _, ok := lineWith(view, repo); ok {
			t.Errorf("a query matching only the shared path reached %q:\n%s", repo, view)
		}
	}
}

// A scan can fail transiently — a scan root on a mount that has gone away.
// Hiding a perfectly good inventory behind that would cost you the repos for
// as long as the harness is up.
func TestAFailedRescanStillOffersTheReposAlreadyFound(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model, _ = model.Update(dashboard.Discovered{Err: errors.New("scan /mnt/work: input/output error")})

	if _, ok := lineWith(drawn(model), "ganymede"); !ok {
		t.Errorf("a failed rescan hid the inventory it already had:\n%s", drawn(model))
	}
}

// The sidepanel can be dragged to any height at all, including one with no
// room for the picker's matches. That gives up the matches, not the Dashboard.
func TestThePickerSurvivesASidepanelWithNoRoomForIt(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{
		Inventory: stock{repos: []string{"/repos/ganymede", "/repos/service-billing"}},
	})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 3})
	model = picking(t, model)

	// The assertion is that this returns at all.
	if lines := strings.Split(model.View(), "\n"); len(lines) > 3 {
		t.Errorf("the picker drew %d lines into a 3-row sidepanel", len(lines))
	}
}

// The cross-check reports Sessions the registry never did, and checks only the
// process — so one can arrive with no directory at all. Asking where that
// belongs answers with the Dashboard's own checkout, which would put the
// harness itself in the working set and keep it there for a week.
func TestASessionWithNoDirectoryIsNotRecordedAsARepo(t *testing.T) {
	state := remembering(t)

	dashboardOn(dashboard.Harness{Activity: state},
		session.Session{PID: 4242, ID: "a", Name: "unplaceable", State: session.Idle})

	if got := state.Active(); len(got) != 0 {
		t.Errorf("a Session with no directory was recorded as a repo: %v", got)
	}
}

// A key that does nothing at all reads as a Dashboard that has hung.
func TestGWithNoDiscoveryConfiguredSaysSo(t *testing.T) {
	model, _ := dashboardOn(dashboard.Harness{}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	if !strings.Contains(strings.ToLower(drawn(model)), "discovery") {
		t.Errorf("g with no inventory behind it said nothing:\n%s", drawn(model))
	}
}

// The picker fills the sidepanel it lives in, whatever a repo is called or how
// deeply it is filed.
func TestPickerFitsTheSidepanel(t *testing.T) {
	model := picking(t, dashboardOn(dashboard.Harness{
		Inventory: stock{repos: []string{
			"/Users/brecht/Projects/teamleader/teamleadercrm-monolith-and-then-some",
			"/Users/brecht/Projects/日本語のプロジェクトディレクトリの名前/請求書ページング",
		}},
	}))

	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}
