package dashboard_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

// workingSet is n repos with one Session each, each repo named so that its
// header carries a token no other row's does.
func workingSet(n int) []session.Session {
	var many []session.Session
	for i := range n {
		id := fmt.Sprintf("%02d", i)
		many = append(many, live("s"+id, "/repos/r"+id, session.Idle))
	}
	return many
}

// fromFoot is how many lines up from the panel's last line want was drawn, and
// -1 when it was not drawn at all.
func fromFoot(view, want string) int {
	lines := strings.Split(view, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], want) {
			return len(lines) - 1 - i
		}
	}
	return -1
}

// The SELECTED box is the one place on the panel your eye should have to learn,
// so it sits on the last lines whatever the working set is doing — rather than
// ending wherever the tree happens to end and walking up and down the panel as
// Sessions start and end.
func TestTheSelectedBoxLandsOnTheSidepanelsLastLines(t *testing.T) {
	for _, set := range []struct {
		what  string
		repos int
		// box is how many lines the SELECTED label has under it — the box
		// itself, which is what has to end the panel.
		box int
	}{
		{"nothing running", 0, 1},
		{"one repo and its Session", 1, 4},
		{"twenty repos and theirs", 20, 4},
	} {
		view := drawn(sidepanel(&jumps{}, workingSet(set.repos)...))
		lines := strings.Split(view, "\n")

		if len(lines) != 45 {
			t.Errorf("with %s the Dashboard drew %d lines into a 45-line sidepanel:\n%s", set.what, len(lines), view)
		}
		// The box's own last line — the keys the selected row offers — is the
		// panel's last line: nothing dead is left under the box.
		if last := lines[len(lines)-1]; strings.TrimSpace(last) == "" {
			t.Errorf("with %s the panel's last line is blank, so the box is not at its foot:\n%s", set.what, view)
		}
		// A tree of two rows and a tree of forty put the label in the same
		// place, which is what says the box no longer walks up and down.
		if up := fromFoot(view, "SELECTED"); up != set.box {
			t.Errorf("with %s SELECTED was drawn %d lines off the foot, want %d:\n%s", set.what, up, set.box, view)
		}
	}
}

// The slack a short working set leaves goes into the tree's own block, above
// the rule. That is what pins everything below it: the tree grows and shrinks
// into the room it is given, and the box does not move.
func TestTheTreeAbsorbsTheSlackAboveTheSelectedBox(t *testing.T) {
	view := drawn(sidepanel(&jumps{}, workingSet(1)...))
	lines := strings.Split(view, "\n")

	blank := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blank++
		}
	}
	if blank == 0 {
		t.Fatalf("a one-repo working set left no slack on a 45-line sidepanel:\n%s", view)
	}
	// Every blank line is inside the tree: above the rule that closes it, and
	// none of them under the box, whose own four lines end the panel.
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			continue
		}
		if up := len(lines) - 1 - i; up < 5 {
			t.Errorf("line %d of the panel is blank, %d off the foot — the slack belongs in the tree:\n%s", i, up, view)
		}
	}
}

// The sidepanel can be dragged to any height at all, including one with no
// room for the tree and the box both. Filling the tree's block must not be
// what makes such a panel overflow.
func TestAShortSidepanelStillFitsWhatItCanDraw(t *testing.T) {
	for _, height := range []int{1, 3, 5, 8, 12} {
		var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
		model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: height})
		model, _ = model.Update(dashboard.Sessions(workingSet(4)))
		view := drawn(model)

		if lines := strings.Split(view, "\n"); len(lines) > height {
			t.Errorf("the Dashboard drew %d lines into a %d-line sidepanel:\n%s", len(lines), height, view)
		}
	}
}

// The tree scrolls around the cursor, so a selection stepped past the foot of
// the block stays in view. The cursor counts rows; the block it has to stay
// inside is a budget in lines.
func TestTheTreeKeepsTheSelectionInViewAsTheCursorWalksPastTheFoot(t *testing.T) {
	many := workingSet(10)

	// A panel tall enough for the whole tree says how many rows there are to
	// walk: a header and a Session each.
	var rows int
	for _, line := range strings.Split(tree(sidepanel(&jumps{}, many...)), "\n") {
		if strings.TrimSpace(line) != "" {
			rows++
		}
	}
	if rows != 2*len(many) {
		t.Fatalf("read %d rows off a full-height sidepanel, want one per repo and per Session (%d)", rows, 2*len(many))
	}

	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 20})
	model, _ = model.Update(dashboard.Sessions(many))

	// A Session row carries the checkout it is working in rather than a name
	// of its own, so what a test follows down the tree is the inversion the
	// cursor draws: the selection is in view exactly when its row is drawn.
	for i := range rows {
		if i > 0 {
			model = press(model, tea.KeyDown)
		}
		if _, ok := selectedRow(model); !ok {
			t.Fatalf("row %d scrolled out of view:\n%s", i, drawn(model))
		}
	}
	// And the walk really reached the foot rather than stopping short: the
	// last row is the last repo's Session, which is what its box names.
	if box := detail(model); !strings.Contains(box, "r09") {
		t.Errorf("SELECTED = %q, want the walk to have ended on the last repo's Session", box)
	}
}

// selectedRow is the drawn row the cursor is on: the one the panel inverts,
// which is how a test finds the selection now that rows carry no token of
// their own.
func selectedRow(model tea.Model) (string, bool) {
	stripped, raw := panelLines(model)
	for i, line := range raw {
		if strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
			return stripped[i], true
		}
	}
	return "", false
}
