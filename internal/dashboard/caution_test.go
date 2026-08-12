package dashboard_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// cautionHang is the one column a caution line is indented by, as the eye reads
// it. A Session row's own indent is two, so the two kinds of line differ in
// their very first character — which is what a test tells them apart by, and
// what stops a caution from reading as another row under the header.
const cautionHang = " "

// railSized is a Dashboard of the given size showing sessions, handed what git
// said about their roots. The sidepanel is forty columns by default and can be
// dragged to anything at all, which is what the caution line's own room is
// measured against.
func railSized(width, height int, cautions dashboard.Cautions, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model, _ = model.Update(dashboard.Sessions(sessions))
	model, _ = model.Update(cautions)
	return model
}

// railWith is railSized at the sidepanel's own size. It stands in for
// roots_test.go's round trip through a real checkout where the test is about how
// a caution is drawn rather than about reading one off git.
func railWith(cautions dashboard.Cautions, sessions ...session.Session) tea.Model {
	return railSized(topology.SidepanelWidth, 45, cautions, sessions...)
}

// cautionUnder is the caution line the tree drew beneath the header of the repo
// at root — where what its checkout is carrying reads, now that the header's own
// line is the name and the mark.
func cautionUnder(t *testing.T, model tea.Model, root string) string {
	t.Helper()
	return under(t, tree(model), filepath.Base(root))
}

// under is the line the tree drew directly beneath the one carrying want.
func under(t *testing.T, view, want string) string {
	t.Helper()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if !strings.Contains(line, want) {
			continue
		}
		if i+1 >= len(lines) {
			t.Fatalf("%q was the last line drawn, with nothing under it:\n%s", want, view)
		}
		return lines[i+1]
	}
	t.Fatalf("no line carrying %q:\n%s", want, view)
	return ""
}

// Eight of eleven repos on a real working set carry a caution, so a caution
// fighting the repo name for one line is the normal case rather than the edge —
// and both lose it. The caution goes on a line of its own beneath the header,
// indented under it, where the header keeps the whole of its own width.
func TestARepoHeadersCautionGoesOnALineOfItsOwn(t *testing.T) {
	root := "/repos/focus-frontend"
	view := tree(railWith(
		dashboard.Cautions{root: repo.Caution{Branch: "FIRE-2910-followup-sorting"}},
		live("focus-frontend-a1", root, session.Idle)))

	header, ok := lineWith(view, "focus-frontend")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", view)
	}
	if strings.Contains(header, caution) {
		t.Errorf("header = %q, want the caution off the header's own line", header)
	}
	warning := under(t, view, "focus-frontend")
	if !strings.HasPrefix(warning, cautionHang+caution+" ") {
		t.Errorf("the line under the header = %q, want the caution indented beneath it", warning)
	}
	if !strings.Contains(warning, "FIRE-2910-followup-sorting") {
		t.Errorf("the caution line = %q, want the whole branch the root strayed to", warning)
	}
}

// The header's own line is the name's and the Main-root mark's, whatever the
// checkout under it is carrying: a repo whose name gave up its suffix to make
// room for a caution is a repo you cannot tell from the one beside it, on the
// panel you are reading to find out which.
func TestACautionCostsTheHeaderNothing(t *testing.T) {
	root := "/repos/focus-service-ai-credit-usage"
	set := []session.Session{live("credit-usage-a1", root, session.Idle)}

	clean := railWith(dashboard.Cautions{root: repo.Caution{}}, set...)
	carrying := railWith(dashboard.Cautions{
		root: repo.Caution{Branch: "FIRE-2923/account-allowances", Dirty: true},
	}, set...)

	was, _ := lineWith(tree(clean), "focus-service-ai-credit-usage")
	now, ok := lineWith(tree(carrying), "focus-service-ai-credit-usage")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", tree(carrying))
	}
	if now != was {
		t.Errorf("header = %q with a caution under it and %q without, want the caution to cost it nothing", now, was)
	}
}

// What the line says, for each thing a Main root's checkout can be carrying. A
// root carrying nothing gets no line at all: an indented blank under every clean
// repo would double the tree's height to say nothing.
func TestTheCautionLineSaysWhatTheCheckoutIsCarrying(t *testing.T) {
	for _, c := range []struct {
		what    string
		caution repo.Caution
		want    string
	}{
		{"a branch", repo.Caution{Branch: "FIRE-2910-followup-sorting"}, " ⚠ FIRE-2910-followup-sorting"},
		{"a branch and work in the tree", repo.Caution{Branch: "FIRE-2923/allowances", Dirty: true}, " ⚠ FIRE-2923/allowances · dirty"},
		{"nothing but work in the tree", repo.Caution{Dirty: true}, " ⚠ dirty"},
		{"a commit checked out by hash", repo.Caution{Detached: true}, " ⚠ detached"},
		{"a hash with work over it", repo.Caution{Detached: true, Dirty: true}, " ⚠ detached · dirty"},
		{"nothing at all", repo.Caution{}, ""},
	} {
		root := "/repos/plans"
		view := tree(railWith(dashboard.Cautions{root: c.caution}, live("plans-bf", root, session.Idle)))

		line := strings.TrimRight(under(t, view, "plans"), " ")
		if c.want == "" {
			// A clean root's header carries straight on to its Session row.
			if strings.HasPrefix(line, cautionHang+caution) {
				t.Errorf("with %s under it the tree drew %q, want no caution line at all", c.what, line)
			}
			continue
		}
		if line != c.want {
			t.Errorf("with %s the caution line = %q, want %q", c.what, line, c.want)
		}
	}
}

// The ladder the caution comes down as the room for it shrinks: the whole of it,
// the branch elided, the marks without the branch, and at the very least the
// mark. It is the ladder the inline caution had, climbed against the whole
// sidepanel now rather than the ten-odd columns left beside a repo name — so
// reaching its lower rungs takes a sidepanel dragged narrow, which the harness
// cannot stop you doing.
func TestTheCautionLineComesDownItsLadderAsTheRoomShrinks(t *testing.T) {
	root := "/repos/plans"
	for _, c := range []struct {
		width int
		want  string
	}{
		{40, " ⚠ FIRE-2923/account-allowances · dirty"},
		{20, " ⚠ FIRE-292… · dirty"},
		{14, " ⚠ dirty"},
		{6, " ⚠"},
	} {
		model := railSized(c.width, 45,
			dashboard.Cautions{root: repo.Caution{Branch: "FIRE-2923/account-allowances", Dirty: true}},
			live("plans-bf", root, session.Idle))

		line, ok := lineWith(tree(model), caution)
		if !ok {
			t.Errorf("at %d columns the tree cautioned about nothing at all:\n%s", c.width, tree(model))
			continue
		}
		if got := strings.TrimRight(line, " "); got != c.want {
			t.Errorf("at %d columns the caution line = %q, want %q", c.width, got, c.want)
		}
	}
}

// cautionAmber is the loud amber a caution reads in, as the validated mock has
// it. It is spelled out here rather than reached for across the package boundary,
// the way this file's own reverseOnly is: the colour is part of what the panel
// promises, so a retuned palette is meant to be read here and moved deliberately
// rather than to slip through green.
var cautionAmber = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))

// The inversion marks the row the cursor is on, and the row here is the header:
// it covers the header's line and stops there. A caution swallowed by the
// inversion would be one you have to move the cursor off the repo to read, which
// is the reading this whole change is undoing.
func TestTheInversionOnASelectedHeaderStopsAtItsCautionLine(t *testing.T) {
	root := "/repos/focus-frontend"
	// The selection opens on the first row, which is the repo's header.
	model := railWith(
		dashboard.Cautions{root: repo.Caution{Branch: "FIRE-2910-followup-sorting", Dirty: true}},
		live("focus-frontend-a1", root, session.Idle))

	stripped, raw := panelLines(model)
	for i, line := range stripped {
		if !strings.Contains(line, "focus-frontend") {
			continue
		}
		if !strings.HasPrefix(raw[i], styleCodeOf(reverseOnly)) {
			t.Fatalf("header row = %q, want the cursor's own inversion on it", raw[i])
		}
		// Anywhere on the line, not just at the front of it: the indent is
		// drawn outside the styling, so an inversion that swallowed this line
		// would open one column in rather than at its first character.
		warning := raw[i+1]
		if strings.Contains(warning, styleCodeOf(reverseOnly)) {
			t.Errorf("caution line = %q, want the inversion to stop at the header's own line", warning)
		}
		if !strings.Contains(warning, styleCodeOf(cautionAmber)) {
			t.Errorf("caution line = %q, want it drawn in the caution's own amber", warning)
		}
		return
	}
	t.Fatalf("no header row for the repo:\n%s", drawn(model))
}

// ↑↓ goes on stepping repo header to Session row. A caution is the header's
// second line rather than a row of its own: there is nothing on it to act on,
// and a cursor that stopped there would spend a keystroke on a line that says
// nothing except about the header above it.
func TestTheCursorStepsOverCautionLines(t *testing.T) {
	api, plans := "/repos/api-internal", "/repos/plans"
	model := railWith(
		dashboard.Cautions{
			api:   repo.Caution{Branch: "FIRE-2914/assistant-streaming"},
			plans: repo.Caution{Dirty: true},
		},
		live("api-internal-a1", api, session.Idle),
		live("plans-bf", plans, session.Idle))

	// A header and a Session each, in root order, and nothing else to stop on.
	for _, want := range []struct {
		what    string
		session bool
	}{
		{"api-internal's header", false},
		{"api-internal's Session", true},
		{"plans' header", false},
		{"plans' Session", true},
	} {
		line, ok := selectedRow(model)
		if !ok {
			t.Fatalf("nothing was selected where %s should be:\n%s", want.what, tree(model))
		}
		if strings.HasPrefix(line, cautionHang+caution) {
			t.Fatalf("the cursor landed on the caution line %q, want it stepped over to %s", line, want.what)
		}
		if isSessionRow(line) != want.session {
			t.Errorf("the cursor is on %q, want %s", line, want.what)
		}
		model = press(model, tea.KeyDown)
	}
	// The walk really ran out of rows rather than stopping short of the foot:
	// the last of them is the last repo's Session, which is what its box names.
	if box := detail(model); !strings.Contains(box, "plans") {
		t.Errorf("SELECTED = %q, want the walk to have ended on the last repo's Session", box)
	}
}

// A tree whose repo headers each draw two lines is longer than the panel sooner
// than one whose rows draw one, and the window around the cursor is a budget in
// lines: the selection has to stay drawn all the way down a working set where
// every repo is carrying something — which is what a real working set looks
// like.
func TestTheCursorStaysInViewOnAWorkingSetFullOfCautions(t *testing.T) {
	many := workingSet(10)
	carrying := dashboard.Cautions{}
	for _, s := range many {
		carrying[s.Dir] = repo.Caution{Branch: "FIRE-2923/account-allowances", Dirty: true}
	}

	const height = 20
	model := railSized(topology.SidepanelWidth, height, carrying, many...)

	// A header and a Session per repo, walked to the last of them.
	for row := range 2 * len(many) {
		if row > 0 {
			model = press(model, tea.KeyDown)
		}
		if _, ok := selectedRow(model); !ok {
			t.Fatalf("row %d scrolled out of view:\n%s", row, drawn(model))
		}
		if lines := strings.Split(drawn(model), "\n"); len(lines) != height {
			t.Fatalf("with the cursor on row %d the Dashboard drew %d lines into a %d-line sidepanel:\n%s",
				row, len(lines), height, drawn(model))
		}
		// Whole rows only: a header drawn without the caution line belonging to
		// it is a header saying its root is carrying nothing.
		lines := strings.Split(tree(model), "\n")
		for i, line := range lines {
			if isSessionRow(line) || strings.HasPrefix(line, cautionHang+caution) || strings.TrimSpace(line) == "" {
				continue
			}
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], cautionHang+caution) {
				t.Fatalf("with the cursor on row %d the header %q was drawn without its caution:\n%s", row, line, tree(model))
			}
		}
	}
	if box := detail(model); !strings.Contains(box, "r09") {
		t.Errorf("SELECTED = %q, want the walk to have ended on the last repo's Session", box)
	}
}

// A caution line wider than the sidepanel wraps, and a wrapped line puts every
// row under it out of step with the selection — which is the one thing the panel
// can never do, at any width it is dragged to. A caution the panel simply stopped
// drawing would satisfy that and nothing else, so the sweep asks for both: the
// line is always there, and it always fits.
func TestTheCautionLineFitsEveryWidthTheSidepanelIsDraggedTo(t *testing.T) {
	root := "/repos/plans"
	// From two columns, which is the narrowest the mark and its indent fit in.
	// At one there is no room for either, and the line says the mark anyway —
	// overrunning by the same column the header row above it already overruns by.
	for width := 2; width <= topology.SidepanelWidth; width++ {
		model := railSized(width, 45,
			dashboard.Cautions{root: repo.Caution{Branch: "FIRE-2923/account-allowances", Dirty: true}},
			live("plans-bf", root, session.Idle))

		// The caution's own line, rather than every line of the panel: a
		// sidepanel dragged down to a column or two has other rows of its own
		// that predate this one.
		said, drew := lineWith(tree(model), caution)
		if !drew {
			t.Errorf("at %d columns the tree cautioned about nothing at all:\n%s", width, tree(model))
			continue
		}
		if drawn := lipgloss.Width(said); drawn > width {
			t.Errorf("at %d columns the caution line is %d wide: %q", width, drawn, said)
		}
	}
}

// A repo named past the sidepanel's own width is the case that used to cost the
// caution everything: the header gave up the branch, then the marks, then the
// name itself. The name is now truncated by nothing but the Main-root mark, and
// the caution beneath it is untouched by how long the name is.
func TestARepoNamedPastTheSidepanelKeepsItsCautionWhole(t *testing.T) {
	root := "/repos/teamleadercrm-monolith-and-then-some-more"
	set := []session.Session{live("monolith-b7", root, session.Idle)}
	branch := "FIRE-2923/account-allowances"

	carrying := railWith(dashboard.Cautions{root: repo.Caution{Branch: branch, Dirty: true}}, set...)
	clean := railWith(dashboard.Cautions{root: repo.Caution{}}, set...)

	// The name overruns the panel, so the header is found by the part of it that
	// survived rather than by the whole.
	header, ok := lineWith(tree(carrying), "teamleadercrm-monolith")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", tree(carrying))
	}
	if was, _ := lineWith(tree(clean), "teamleadercrm-monolith"); header != was {
		t.Errorf("header = %q with a caution under it and %q without, want the name truncated by the mark alone", header, was)
	}
	if warning := under(t, tree(carrying), "teamleadercrm-monolith"); !strings.Contains(warning, branch+" · dirty") {
		t.Errorf("caution line = %q, want the whole of %q under a name that long", warning, branch+" · dirty")
	}
}
