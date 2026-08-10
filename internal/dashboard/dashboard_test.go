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

// focuses records every time the Dashboard moved keyboard focus into the
// working client, standing in for the dock's own select-pane.
type focuses struct {
	n   int
	err error
}

func (f *focuses) Focus() error {
	f.n++
	return f.err
}

// sidepanel is a Dashboard sized for the sidepanel, showing sessions.
func sidepanel(jumper dashboard.Jumper, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// strips records the counts the Dashboard put out, standing in for the working
// client's status line.
type strips struct {
	shown []session.Attention
	err   error
}

func (s *strips) Show(waiting session.Attention) error {
	s.shown = append(s.shown, waiting)
	return s.err
}

// showing runs one working set after another past a Dashboard wired to strip.
func showing(strip dashboard.Strip, sets ...[]session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Strip: strip})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	for _, set := range sets {
		model, _ = model.Update(dashboard.Sessions(set))
	}
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

// live is a Session as the registry would describe it.
func live(name, dir string, state session.State) session.Session {
	return session.Session{
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
		live("teamleadercrm-monolith-billing-b7", "/repos/teamleadercrm-monolith-and-then-some", session.Working),
		live("FIRE-2841-max-paging-numbers", "/repos/teamleadercrm-monolith-and-then-some", session.Blocked),
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
		live("請求書ページングの最大値を直す作業を続けています", "/repos/service-billing", session.Blocked),
		live("ai-assistant-b3", "/repos/日本語のプロジェクトディレクトリの名前", session.Working),
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
		live("service-billing-a1", "/repos/service-billing", session.Idle),
		live("ai-assistant-b3", "/repos/service-ai-assistant", session.Idle),
		live("FIRE-2841-paging", "/repos/service-ai-assistant", session.Idle),
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

// Attention comes first: a Session that cannot continue without you outranks
// one that is getting on with its work, whichever repo each is in. The names
// here run the other way round, so only the state can be putting them in this
// order.
func TestBlockedSessionsRiseToTheTop(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		live("ai-assistant-b3", "/repos/service-ai-assistant", session.Working),
		live("ganymede-78", "/repos/ganymede", session.Idle),
		live("service-billing-a1", "/repos/service-billing", session.Blocked),
	))

	blocked := strings.Index(view, "service-billing-a1")
	for _, later := range []string{"ai-assistant-b3", "ganymede-78"} {
		if at := strings.Index(view, later); at < blocked {
			t.Errorf("%q is drawn above the Blocked Session:\n%s", later, view)
		}
	}
}

// A repo is as urgent as the most urgent Session in it: one Session that
// cannot continue takes its whole repo to the top, over a repo whose Sessions
// are all getting on with their work.
func TestARepoIsAsUrgentAsItsMostUrgentSession(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		live("aaa-working", "/repos/service-ai-assistant", session.Working),
		live("bbb-working", "/repos/service-ai-assistant", session.Working),
		live("zzz-idle", "/repos/service-billing", session.Idle),
		live("zzz-blocked", "/repos/service-billing", session.Blocked),
	))

	if strings.Index(view, "service-billing") > strings.Index(view, "service-ai-assistant") {
		t.Errorf("the repo holding the Blocked Session is drawn second:\n%s", view)
	}
	// And the Session it was promoted for leads it, rather than sitting under
	// whichever of its Sessions the registry happened to name first.
	if strings.Index(view, "zzz-blocked") > strings.Index(view, "zzz-idle") {
		t.Errorf("the Blocked Session is not at the top of its repo:\n%s", view)
	}
}

// Within Attention, the Session that has been waiting on you longest is the
// one to answer first.
func TestTheLongestBlockedSessionIsTheTopOfAttention(t *testing.T) {
	recent := live("aaa-just-blocked", "/repos/service-ai-assistant", session.Blocked)
	recent.Since = epoch.Add(time.Hour)
	waiting := live("zzz-blocked-since-lunch", "/repos/service-billing", session.Blocked)
	waiting.Since = epoch

	view := tree(sidepanel(&jumps{}, recent, waiting))

	if strings.Index(view, "zzz-blocked-since-lunch") > strings.Index(view, "aaa-just-blocked") {
		t.Errorf("the Session blocked longest is not at the top of Attention:\n%s", view)
	}
}

// Below Attention the tree reads by recency instead: what you were last at is
// what you are most likely coming back to.
func TestSessionsAskingNothingReadMostRecentFirst(t *testing.T) {
	stale := live("aaa-idle-since-monday", "/repos/service-ai-assistant", session.Idle)
	stale.Since = epoch
	recent := live("zzz-idle-just-now", "/repos/service-billing", session.Idle)
	recent.Since = epoch.Add(time.Hour)

	view := tree(sidepanel(&jumps{}, stale, recent))

	if strings.Index(view, "zzz-idle-just-now") > strings.Index(view, "aaa-idle-since-monday") {
		t.Errorf("the Session that moved most recently is not at the top:\n%s", view)
	}
}

// Blocked is always displayed with its reason, and the row has no room for it:
// the detail box is where it goes.
func TestSelectedShowsWhyASessionIsBlocked(t *testing.T) {
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
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
		model := sidepanel(&jumps{}, live("ganymede-78", c.dir, session.Idle))
		model = press(model, tea.KeyDown)

		if !strings.Contains(drawn(model), c.want) {
			t.Errorf("a Session working in %s is not shown as %q:\n%s", c.dir, c.want, drawn(model))
		}
	}
}

// A Session that has gone loses its row without the Dashboard being restarted.
func TestASessionThatIsGoneLosesItsRow(t *testing.T) {
	staying := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(&jumps{}, staying, live("service-billing-a1", "/repos/service-billing", session.Idle))

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
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, only)

	// Down from the repo's header row onto its one Session.
	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 1 || jumper.pids[0] != only.PID {
		t.Errorf("jumped to %v, want the selected Session's process %d", jumper.pids, only.PID)
	}
}

// Enter takes you all the way in: once the working client is pointed at the
// Session, the keyboard follows it there — no separate alt+g.
func TestEnterOnASessionRowFocusesTheWorkingClient(t *testing.T) {
	jumper := &jumps{}
	focuser := &focuses{}
	only := live("ganymede-78", "/repos/ganymede", session.Idle)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Focuser: focuser})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{only})

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after Enter on a Session row", focuser.n)
	}
}

// Enter on a repo row with nothing running yet makes the same promise: once
// Open has brought the repo's Session up, the keyboard follows it there too.
func TestEnterOnARepoRowFocusesTheWorkingClient(t *testing.T) {
	opener := &opens{}
	focuser := &focuses{}
	model := onARepo(t, dashboard.Harness{Opener: opener, Focuser: focuser}, "/repos/service-billing")

	model = press(model, tea.KeyEnter)

	if len(opener.dirs) != 1 || opener.dirs[0] != "/repos/service-billing" {
		t.Fatalf("opened %v, want /repos/service-billing", opener.dirs)
	}
	if focuser.n != 1 {
		t.Errorf("Focus called %d times, want exactly one after Enter on a repo row", focuser.n)
	}
}

// A repo's header row is not a Session; Enter on it has nowhere to go.
func TestEnterOnARepoHeaderJumpsNowhere(t *testing.T) {
	jumper := &jumps{}
	model := sidepanel(jumper, live("ganymede-78", "/repos/ganymede", session.Idle))

	model = press(model, tea.KeyEnter)

	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v from a repo's header row", jumper.pids)
	}
}

// A jump the harness cannot make — a Session running outside tmux, or one that
// has ended since the registry was read — is reported rather than swallowed.
func TestAJumpThatCannotBeMadeIsReported(t *testing.T) {
	jumper := &jumps{err: errors.New("no tmux pane is running process 4242")}
	model := sidepanel(jumper, live("ganymede-78", "/repos/ganymede", session.Idle))

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
	chosen := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := sidepanel(jumper, chosen)
	model = press(model, tea.KeyDown)

	// A Blocked Session in another repo arrives and takes the top of the tree.
	model, _ = model.Update(dashboard.Sessions{
		chosen,
		live("FIRE-2841-paging", "/repos/service-billing", session.Blocked),
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
	first := session.Session{PID: 11, Dir: "/repos/service-ai-assistant", Name: "ai-assistant-b3", State: session.Idle, Since: epoch}
	second := session.Session{PID: 22, Dir: "/repos/service-billing", Name: "service-billing-a1", State: session.Idle, Since: epoch}
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
	var many []session.Session
	for _, name := range []string{"a1", "b2", "c3", "d4", "e5", "f6", "g7", "h8", "i9", "j10", "k11", "l12"} {
		many = append(many, live("session-"+name, "/repos/repo-"+name, session.Idle))
	}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
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

// seeing records which Sessions the Dashboard reported you had looked at.
type seeing struct{ ids []string }

func (s *seeing) Seen(id string) { s.ids = append(s.ids, id) }

// Ready is a state of its own on the tree, not a shade of Idle: it is the
// unread badge, and it has to read as one at a glance.
func TestReadyIsDrawnDistinctlyFromIdle(t *testing.T) {
	glyphs := map[string]session.State{}
	for _, state := range []session.State{session.Working, session.Blocked, session.Ready, session.Idle, session.Shell} {
		view := tree(sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", state)))
		line, ok := lineWith(view, "ganymede-78")
		if !ok {
			t.Fatalf("no row for a %s Session:\n%s", state, view)
		}
		// The mark is the first thing on the row after the indent, and the row
		// carries a name and a wait age after it.
		glyph, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if glyph == "" || strings.Contains(glyph, "ganymede-78") {
			t.Errorf("a %s Session's row carries no state glyph: %q", state, line)
		}
		for drawn, other := range glyphs {
			if drawn == glyph {
				t.Errorf("%s and %s are both drawn %q", state, other, glyph)
			}
		}
		glyphs[glyph] = state
	}
}

// Attention is Blocked and Ready together, and it sorts above everything that
// is asking nothing of you. The names run the other way round, so only the
// states can be putting these rows in this order.
func TestAnUnreadTurnRisesAboveTheSessionsAskingNothing(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		live("aaa-working", "/repos/service-ai-assistant", session.Working),
		live("bbb-idle", "/repos/ganymede", session.Idle),
		live("zzz-ready", "/repos/service-billing", session.Ready),
	))

	ready := strings.Index(view, "zzz-ready")
	for _, later := range []string{"aaa-working", "bbb-idle"} {
		if at := strings.Index(view, later); at < ready {
			t.Errorf("%q is drawn above the Ready Session:\n%s", later, view)
		}
	}
}

// Within Attention, Blocked outranks Ready: a Session that cannot continue at
// all comes before one that is only waiting to be read.
func TestBlockedOutranksReadyOnTheTree(t *testing.T) {
	view := tree(sidepanel(&jumps{},
		live("aaa-ready", "/repos/service-ai-assistant", session.Ready),
		live("zzz-blocked", "/repos/service-billing", session.Blocked),
	))

	if strings.Index(view, "zzz-blocked") > strings.Index(view, "aaa-ready") {
		t.Errorf("Ready is drawn above Blocked:\n%s", view)
	}
}

// Within Attention the Session waiting longest is the one to get to first,
// whether it is Blocked or Ready.
func TestTheLongestUnreadTurnIsTheTopOfReady(t *testing.T) {
	recent := live("aaa-just-finished", "/repos/service-ai-assistant", session.Ready)
	recent.Since = epoch.Add(time.Hour)
	waiting := live("zzz-ready-since-lunch", "/repos/service-billing", session.Ready)
	waiting.Since = epoch

	view := tree(sidepanel(&jumps{}, recent, waiting))

	if strings.Index(view, "zzz-ready-since-lunch") > strings.Index(view, "aaa-just-finished") {
		t.Errorf("the turn that has been unread longest is not at the top of Attention:\n%s", view)
	}
}

// An unread badge you cannot read anything of is only half a badge: the detail
// box says what the turn ended on.
func TestSelectedShowsWhatAReadySessionLastSaid(t *testing.T) {
	ready := live("FIRE-2841-paging", "/repos/service-billing", session.Ready)
	ready.Snippet = "Fix ready on fix/max-paging-numbers, 42 files, asking to push."
	model := sidepanel(&jumps{}, ready)

	model = press(model, tea.KeyDown)

	if !strings.Contains(drawn(model), "Fix ready on fix/max-paging") {
		t.Errorf("the Dashboard does not say what the Session last said:\n%s", drawn(model))
	}
}

// The strip is the same Attention the rail is sorted by, carried to the status
// line of the Session you are working in — one working set, counted once, so
// the two surfaces cannot disagree.
func TestTheStripCarriesTheCountsTheRailIsSortedBy(t *testing.T) {
	strip := &strips{}

	showing(strip, []session.Session{
		live("aaa-blocked", "/repos/service-billing", session.Blocked),
		live("bbb-ready", "/repos/service-ai-assistant", session.Ready),
		live("ccc-ready", "/repos/ganymede", session.Ready),
		live("ddd-working", "/repos/ganymede", session.Working),
	})

	if len(strip.shown) == 0 {
		t.Fatal("the Dashboard never put the counts out")
	}
	if last, want := strip.shown[len(strip.shown)-1], (session.Attention{Blocked: 1, Ready: 2}); last != want {
		t.Errorf("the strip reads %+v, want %+v", last, want)
	}
}

// The counts follow the working set: a Session answered is a Blocked count
// that has to come down of its own accord, without you having looked at the
// strip or anything else.
func TestTheStripFollowsTheStatesAsTheyChange(t *testing.T) {
	strip := &strips{}
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
	answered := blocked
	answered.State = session.Working

	showing(strip, []session.Session{blocked}, []session.Session{answered})

	if last, want := strip.shown[len(strip.shown)-1], (session.Attention{}); last != want {
		t.Errorf("the strip still reads %+v after the Session was answered, want %+v", last, want)
	}
}

// Writing the strip redraws every client on the Sessions server, and the
// working set is rebuilt whenever anything at all moves. A Dashboard that
// wrote the same counts again on every registry event would flicker the
// Session you are typing in for no news.
func TestTheStripIsLeftAloneWhenTheCountsHaveNotChanged(t *testing.T) {
	strip := &strips{}
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
	elsewhere := live("ganymede-78", "/repos/ganymede", session.Idle)
	working := elsewhere
	working.State = session.Working

	showing(strip,
		[]session.Session{blocked, elsewhere},
		[]session.Session{blocked, working},
		[]session.Session{blocked, elsewhere},
	)

	if len(strip.shown) != 1 {
		t.Errorf("the Dashboard wrote the strip %d times for one set of counts: %+v", len(strip.shown), strip.shown)
	}
}

// The strip is redundancy, and redundancy that fails is not worth a word: the
// rail still has everything the strip was going to say.
func TestAStripThatCannotBeWrittenLeavesTheRailAlone(t *testing.T) {
	strip := &strips{err: errors.New("no server running on /private/tmp/tmux-501/default")}

	model := showing(strip, []session.Session{live("ganymede-78", "/repos/ganymede", session.Blocked)})

	if view := drawn(model); !strings.Contains(view, "ganymede-78") || strings.Contains(view, "no server running") {
		t.Errorf("a strip that could not be written cost the rail its tree:\n%s", view)
	}
}

// A write that did not land has not been said. Counts the Dashboard only
// tried to put out must be tried again, or a status line that went blank on a
// tmux the Dashboard could not reach for a moment stays blank for as long as
// nothing else moves.
func TestCountsThatCouldNotBeWrittenAreTriedAgain(t *testing.T) {
	strip := &strips{err: errors.New("no server running on /private/tmp/tmux-501/default")}
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)

	showing(strip, []session.Session{blocked}, []session.Session{blocked})

	if len(strip.shown) != 2 {
		t.Errorf("the Dashboard wrote the strip %d times, want it tried again after the write that failed", len(strip.shown))
	}
}

// A Dashboard that has gone takes its counts with it: a strip nobody is left
// to keep up to date is a strip that will be wrong by morning.
func TestAClosedDashboardTakesItsCountsWithIt(t *testing.T) {
	strip := &strips{}
	model := showing(strip, []session.Session{live("ganymede-78", "/repos/ganymede", session.Blocked)})

	press(model, tea.KeyCtrlC)

	if last := strip.shown[len(strip.shown)-1]; last.Any() {
		t.Errorf("a closed Dashboard left %+v on the status line", last)
	}
}

// The rail's own header carries the counts too — the tree scrolls, and what is
// waiting on you has to be readable without it.
func TestTheDashboardHeaderCountsWhatIsWaitingOnYou(t *testing.T) {
	view := drawn(sidepanel(&jumps{},
		live("aaa-blocked", "/repos/service-billing", session.Blocked),
		live("bbb-ready", "/repos/service-ai-assistant", session.Ready),
		live("ccc-ready", "/repos/ganymede", session.Ready),
	))

	header, _, _ := strings.Cut(view, "\n")
	if !strings.Contains(header, session.Blocked.Glyph()+" 1") {
		t.Errorf("the header does not count the Blocked Session: %q", header)
	}
	if !strings.Contains(header, session.Ready.Glyph()+" 2") {
		t.Errorf("the header does not count the unread turns: %q", header)
	}
}

// A working set asking nothing of you leaves the header as quiet as the strip.
func TestTheHeaderCountsNothingWhenNothingIsWaiting(t *testing.T) {
	view := drawn(sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working)))

	header, _, _ := strings.Cut(view, "\n")
	if strings.ContainsAny(header, "0123456789") {
		t.Errorf("the header counts something with nothing waiting on you: %q", header)
	}
}

// Longest-waiting first is an order you can only check if the rail says how
// long: every row carries the age of the state it is in.
func TestEveryRowSaysHowLongItHasBeenInItsState(t *testing.T) {
	waiting := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
	waiting.Since = time.Now().Add(-4 * time.Minute)
	since := live("ganymede-78", "/repos/ganymede", session.Idle)
	since.Since = time.Now().Add(-3 * time.Hour)

	view := tree(sidepanel(&jumps{}, waiting, since))

	if line, _ := lineWith(view, "FIRE-2841-paging"); !strings.HasSuffix(strings.TrimRight(line, " "), "4m") {
		t.Errorf("the Blocked row does not say it has been waiting four minutes: %q", line)
	}
	if line, _ := lineWith(view, "ganymede-78"); !strings.HasSuffix(strings.TrimRight(line, " "), "3h") {
		t.Errorf("the Idle row does not say how long it has been there: %q", line)
	}
}

// A Session that has just moved is drawn as having just moved, rather than as
// having waited nought minutes.
func TestARowThatHasJustMovedSaysSo(t *testing.T) {
	just := live("ganymede-78", "/repos/ganymede", session.Ready)
	just.Since = time.Now()

	view := tree(sidepanel(&jumps{}, just))

	if line, _ := lineWith(view, "ganymede-78"); !strings.Contains(line, "now") {
		t.Errorf("a Session that has just moved reads %q", line)
	}
}

// The detail box is where you decide what to do about the row, and how long it
// has been waiting is half of that decision.
func TestTheDetailBoxSaysHowLongTheSessionHasBeenWaiting(t *testing.T) {
	blocked := live("FIRE-2841-paging", "/repos/service-billing", session.Blocked)
	blocked.Since = time.Now().Add(-90 * time.Minute)
	blocked.Reason = "permission: Bash"

	model := press(sidepanel(&jumps{}, blocked), tea.KeyDown)

	box := detail(model)
	if !strings.Contains(box, string(session.Blocked)) || !strings.Contains(box, "1h") {
		t.Errorf("the detail box does not say how long the Session has been Blocked:\n%s", box)
	}
}

// The rail truncates a Session's name to fit; the detail box is where the whole
// of it goes, because it is what you would type to find the thing again.
func TestTheDetailBoxNamesTheSelectedSessionInFull(t *testing.T) {
	long := live("FIRE-2841-max-paging-numbers", "/repos/service-billing", session.Ready)

	model := press(sidepanel(&jumps{}, long), tea.KeyDown)

	if box := detail(model); !strings.Contains(box, long.Name) {
		t.Errorf("the detail box does not name the Session in full:\n%s", box)
	}
}

// detail is just the SELECTED box: everything below the second rule.
func detail(model tea.Model) string {
	lines := strings.Split(drawn(model), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != "" && strings.Trim(lines[i], "─") == "" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return drawn(model)
}

// Jumping to a Session is seeing it, and seeing it is what clears Ready. The
// Dashboard says so itself rather than waiting for tmux to report the focus,
// because it is the one that moved you.
func TestJumpingToASessionReportsItSeen(t *testing.T) {
	seen := &seeing{}
	ready := live("ganymede-78", "/repos/ganymede", session.Ready)
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Seen: seen.Seen})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{ready})

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if len(seen.ids) != 1 || seen.ids[0] != ready.ID {
		t.Errorf("the Dashboard reported %v seen, want the Session it jumped to (%s)", seen.ids, ready.ID)
	}
}

// A jump the harness could not make left you where you were, so the Session
// has not been seen and its badge has to stay.
func TestAJumpThatCouldNotBeMadeLeavesTheBadgeAlone(t *testing.T) {
	seen := &seeing{}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{err: errors.New("no tmux pane is running process 4242")}, Seen: seen.Seen})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions{live("ganymede-78", "/repos/ganymede", session.Ready)})

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)

	if len(seen.ids) != 0 {
		t.Errorf("the Dashboard reported %v seen after a jump it could not make", seen.ids)
	}
}
