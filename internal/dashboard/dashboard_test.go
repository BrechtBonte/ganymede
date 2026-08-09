package dashboard_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// jumps records where the Dashboard asked to be taken, standing in for the
// harness steering the working client.
type jumps struct {
	pids []int
	err  error
}

func (j *jumps) Jump(pid int) error {
	j.pids = append(j.pids, pid)
	return j.err
}

// sidepanel is a Dashboard sized for the sidepanel, showing sessions.
func sidepanel(jumper dashboard.Jumper, sessions ...registry.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, jumper)
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// drawn is what the sidepanel shows, with the styling taken off so a test can
// read it as the eye does.
func drawn(model tea.Model) string { return ansi.Strip(model.View()) }

// tree is just the repo tree: the drawn lines between the rule under the title
// and the rule above the detail box. The detail box repeats what the selected
// row says, so a test about the tree has to look only at the tree.
func tree(model tea.Model) string {
	lines := strings.Split(drawn(model), "\n")
	var rules []int
	for i, line := range lines {
		if line != "" && strings.Trim(line, "─") == "" {
			rules = append(rules, i)
		}
	}
	if len(rules) != 2 {
		return drawn(model)
	}
	return strings.Join(lines[rules[0]+1:rules[1]], "\n")
}

// press sends a keystroke to the Dashboard.
func press(model tea.Model, key tea.KeyType) tea.Model {
	model, _ = model.Update(tea.KeyMsg{Type: key})
	return model
}

// session is a Session as the registry would describe it.
func session(name, dir string, state registry.State) registry.Session {
	return registry.Session{
		PID:   len(name) + len(dir),
		ID:    name + "-id",
		Dir:   dir,
		Name:  name,
		State: state,
		Since: epoch,
	}
}

var epoch = time.UnixMilli(1786272000000)

// lineWith returns the drawn line containing want, and whether there was one.
func lineWith(view, want string) (string, bool) {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return line, true
		}
	}
	return "", false
}

// The Dashboard has to live in a 40-column sidepanel: anything wider wraps and
// the repo tree turns to soup.
func TestDashboardFitsTheSidepanel(t *testing.T) {
	model := sidepanel(&jumps{},
		session("teamleadercrm-monolith-billing-b7", "/repos/teamleadercrm-monolith-and-then-some", registry.Working),
		session("FIRE-2841-max-paging-numbers", "/repos/teamleadercrm-monolith-and-then-some", registry.Blocked),
	)

	for _, line := range strings.Split(model.View(), "\n") {
		// lipgloss.Width measures what the terminal shows, ignoring styling.
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}

// A Session can be named anything `claude -n` accepts. Characters two columns
// wide have to be counted in columns: measured as characters, a name fits the
// sidepanel on paper, wraps onto a second line in the terminal, and puts every
// row below it out of step with the selection.
func TestDashboardFitsTheSidepanelWhateverASessionIsNamed(t *testing.T) {
	// Each of these is well inside 40 characters and well outside 40 columns.
	model := sidepanel(&jumps{},
		session("請求書ページングの最大値を直す作業を続けています", "/repos/service-billing", registry.Blocked),
		session("ai-assistant-b3", "/repos/日本語のプロジェクトディレクトリの名前", registry.Working),
	)

	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}

// With nothing running, the Dashboard says so rather than showing an empty
// frame that looks broken.
func TestDashboardNamesItselfAndReportsAnEmptyWorkingSet(t *testing.T) {
	view := drawn(sidepanel(&jumps{}))

	if !strings.Contains(strings.ToLower(view), "ganymede") {
		t.Errorf("the Dashboard does not name itself:\n%s", view)
	}
	if !strings.Contains(view, "No sessions") {
		t.Errorf("the Dashboard does not report an empty working set:\n%s", view)
	}
}

// Sessions sit under their repo, indented, so the sidepanel reads as a tree of
// repos rather than a flat list of names.
func TestSessionsAreGroupedUnderTheirRepo(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		session("service-billing-a1", "/repos/service-billing", registry.Idle),
		session("ai-assistant-b3", "/repos/service-ai-assistant", registry.Idle),
		session("FIRE-2841-paging", "/repos/service-ai-assistant", registry.Idle),
	))

	if got := strings.Count(view, "service-ai-assistant"); got != 1 {
		t.Errorf("service-ai-assistant appears on %d rows, want one header for the repo:\n%s", got, view)
	}
	for _, name := range []string{"ai-assistant-b3", "FIRE-2841-paging"} {
		line, ok := lineWith(view, name)
		if !ok {
			t.Fatalf("no row for the Session %q:\n%s", name, view)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("the row for %q is not indented under its repo: %q", name, line)
		}
	}
	if _, ok := lineWith(view, "service-billing"); !ok {
		t.Errorf("no header for the second repo:\n%s", view)
	}
}

// Every state the registry can tell apart reads differently at a glance.
func TestEachSessionStateIsDrawnDistinctly(t *testing.T) {
	seen := map[string]registry.State{}
	for _, state := range []registry.State{registry.Working, registry.Blocked, registry.Idle, registry.Shell} {
		view := tree(sidepanel(&jumps{}, session("ganymede-78", "/repos/ganymede", state)))
		line, ok := lineWith(view, "ganymede-78")
		if !ok {
			t.Fatalf("no row for a %s Session:\n%s", state, view)
		}
		glyph := strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(line, " "), "ganymede-78"))
		if glyph == "" {
			t.Errorf("a %s Session's row carries no state glyph: %q", state, line)
		}
		if other, taken := seen[glyph]; taken {
			t.Errorf("%s and %s are both drawn %q", state, other, glyph)
		}
		seen[glyph] = state
	}
}

// Attention comes first: a Session that cannot continue without you outranks
// one that is getting on with its work, whichever repo each is in. The names
// here run the other way round, so only the state can be putting them in this
// order.
func TestBlockedSessionsRiseToTheTop(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		session("ai-assistant-b3", "/repos/service-ai-assistant", registry.Working),
		session("ganymede-78", "/repos/ganymede", registry.Idle),
		session("service-billing-a1", "/repos/service-billing", registry.Blocked),
	))

	blocked := strings.Index(view, "service-billing-a1")
	for _, later := range []string{"ai-assistant-b3", "ganymede-78"} {
		if at := strings.Index(view, later); at < blocked {
			t.Errorf("%q is drawn above the Blocked Session:\n%s", later, view)
		}
	}
}

// Within Attention, the Session that has been waiting on you longest is the
// one to answer first.
func TestTheLongestBlockedSessionIsTheTopOfAttention(t *testing.T) {
	recent := session("aaa-just-blocked", "/repos/service-ai-assistant", registry.Blocked)
	recent.Since = epoch.Add(time.Hour)
	waiting := session("zzz-blocked-since-lunch", "/repos/service-billing", registry.Blocked)
	waiting.Since = epoch

	view := tree(sidepanel(&jumps{}, recent, waiting))

	if strings.Index(view, "zzz-blocked-since-lunch") > strings.Index(view, "aaa-just-blocked") {
		t.Errorf("the Session blocked longest is not at the top of Attention:\n%s", view)
	}
}

// Below Attention the tree reads by recency instead: what you were last at is
// what you are most likely coming back to.
func TestSessionsAskingNothingReadMostRecentFirst(t *testing.T) {
	stale := session("aaa-idle-since-monday", "/repos/service-ai-assistant", registry.Idle)
	stale.Since = epoch
	recent := session("zzz-idle-just-now", "/repos/service-billing", registry.Idle)
	recent.Since = epoch.Add(time.Hour)

	view := tree(sidepanel(&jumps{}, stale, recent))

	if strings.Index(view, "zzz-idle-just-now") > strings.Index(view, "aaa-idle-since-monday") {
		t.Errorf("the Session that moved most recently is not at the top:\n%s", view)
	}
}

// Blocked is always displayed with its reason, and the row has no room for it:
// the detail box is where it goes.
func TestSelectedShowsWhyASessionIsBlocked(t *testing.T) {
	blocked := session("FIRE-2841-paging", "/repos/service-billing", registry.Blocked)
	blocked.Reason = "permission: Bash"
	model := sidepanel(&jumps{}, blocked)

	model = press(model, tea.KeyDown)

	if !strings.Contains(drawn(model), "permission: Bash") {
		t.Errorf("the Dashboard does not say what the Session is waiting for:\n%s", drawn(model))
	}
}

// The detail box says where a Session is working, and a path you cannot paste
// somewhere else is no use: a directory that merely starts with the home
// directory's name is not inside it.
func TestSelectedAbbreviatesOnlyPathsActuallyUnderHome(t *testing.T) {
	t.Setenv("HOME", "/Users/brechtbonte")

	for _, c := range []struct{ dir, want string }{
		{"/Users/brechtbonte/Projects/ganymede", "~/Projects/ganymede"},
		{"/Users/brechtbonte-old/billing", "/Users/brechtbonte-old/billing"},
	} {
		model := sidepanel(&jumps{}, session("ganymede-78", c.dir, registry.Idle))
		model = press(model, tea.KeyDown)

		if !strings.Contains(drawn(model), c.want) {
			t.Errorf("a Session working in %s is not shown as %q:\n%s", c.dir, c.want, drawn(model))
		}
	}
}

// A Session that has gone loses its row without the Dashboard being restarted.
func TestASessionThatIsGoneLosesItsRow(t *testing.T) {
	staying := session("ganymede-78", "/repos/ganymede", registry.Idle)
	model := sidepanel(&jumps{}, staying, session("service-billing-a1", "/repos/service-billing", registry.Idle))

	model, _ = model.Update(dashboard.Sessions{staying})

	view := tree(model)
	if strings.Contains(view, "service-billing-a1") {
		t.Errorf("the row for a Session that is Gone is still drawn:\n%s", view)
	}
	if !strings.Contains(view, "ganymede-78") {
		t.Errorf("the Session that is still running lost its row:\n%s", view)
	}
}

// Enter is the honest fallback for everything the Dashboard cannot do itself:
// it puts the selected Session in front of you.
func TestEnterJumpsToTheSelectedSession(t *testing.T) {
	jumper := &jumps{}
	only := session("ganymede-78", "/repos/ganymede", registry.Idle)
	model := sidepanel(jumper, only)

	// Down from the repo's header row onto its one Session.
	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 1 || jumper.pids[0] != only.PID {
		t.Errorf("jumped to %v, want the selected Session's process %d", jumper.pids, only.PID)
	}
}

// A repo's header row is not a Session; Enter on it has nowhere to go.
func TestEnterOnARepoHeaderJumpsNowhere(t *testing.T) {
	jumper := &jumps{}
	model := sidepanel(jumper, session("ganymede-78", "/repos/ganymede", registry.Idle))

	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v from a repo's header row", jumper.pids)
	}
}

// A jump the harness cannot make — a Session running outside tmux, or one that
// has ended since the registry was read — is reported rather than swallowed.
func TestAJumpThatCannotBeMadeIsReported(t *testing.T) {
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	model := sidepanel(jumper, session("ganymede-78", "/repos/ganymede", registry.Idle))

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if !strings.Contains(drawn(model), "no tmux pane") {
		t.Errorf("the Dashboard swallowed a jump it could not make:\n%s", drawn(model))
	}
}

// The working set changes under your hands. The selection has to stay on the
// Session you put it on, not on whatever row inherits that position.
func TestSelectionStaysWithItsSessionAsTheWorkingSetChanges(t *testing.T) {
	jumper := &jumps{}
	chosen := session("ganymede-78", "/repos/ganymede", registry.Idle)
	model := sidepanel(jumper, chosen)
	model = press(model, tea.KeyDown)

	// A Blocked Session in another repo arrives and takes the top of the tree.
	model, _ = model.Update(dashboard.Sessions{
		chosen,
		session("FIRE-2841-paging", "/repos/service-billing", registry.Blocked),
	})
	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 1 || jumper.pids[0] != chosen.PID {
		t.Errorf("jumped to %v, want the Session the selection was on (%d)", jumper.pids, chosen.PID)
	}
}

// The registry is undocumented, and a Session's id is a field the harness has
// never checked. Two Sessions arriving without one must not make the selection
// slide onto the wrong Session — the jump that follows would land in the wrong
// repo.
func TestSelectionSurvivesSessionsWithoutARegistryID(t *testing.T) {
	jumper := &jumps{}
	first := registry.Session{PID: 11, Dir: "/repos/service-ai-assistant", Name: "ai-assistant-b3", State: registry.Idle, Since: epoch}
	second := registry.Session{PID: 22, Dir: "/repos/service-billing", Name: "service-billing-a1", State: registry.Idle, Since: epoch}
	model := sidepanel(jumper, first, second)

	// Onto the second repo's Session: header, Session, header, Session.
	for range 3 {
		model = press(model, tea.KeyDown)
	}
	model, _ = model.Update(dashboard.Sessions{first, second})
	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 1 || jumper.pids[0] != second.PID {
		t.Errorf("jumped to %v, want the Session the selection was on (%d)", jumper.pids, second.PID)
	}
}

// However long the working set gets, the detail box stays on the sidepanel —
// it is where the Blocked reason and the action keys live.
func TestTheDetailBoxSurvivesAWorkingSetTallerThanTheSidepanel(t *testing.T) {
	var many []registry.Session
	for _, name := range []string{"a1", "b2", "c3", "d4", "e5", "f6", "g7", "h8", "i9", "j10", "k11", "l12"} {
		many = append(many, session("session-"+name, "/repos/repo-"+name, registry.Idle))
	}
	var model tea.Model = dashboard.New(nil, &jumps{})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 12})
	model, _ = model.Update(dashboard.Sessions(many))

	// All the way down, to the last Session in the tree.
	for range len(many) * 2 {
		model = press(model, tea.KeyDown)
	}
	view := drawn(model)

	if lines := strings.Count(view, "\n") + 1; lines > 12 {
		t.Errorf("the Dashboard drew %d lines in a 12-line sidepanel:\n%s", lines, view)
	}
	if !strings.Contains(view, "SELECTED") {
		t.Errorf("the detail box was pushed off the sidepanel:\n%s", view)
	}
	if !strings.Contains(tree(model), "session-l12") {
		t.Errorf("the selected Session is not on the tree:\n%s", view)
	}
}
