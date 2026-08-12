package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/charmbracelet/lipgloss"
)

// brandBlue is the mock's blue, and bold with it: the harness's own mark. It is
// spelled out here rather than reached for across the package boundary, the way
// caution_test.go's cautionAmber is — the colour is part of what the panel
// promises, so a retuned palette is meant to be read here and moved deliberately
// rather than to slip through green.
//
// It is the same triplet as Working's today and is nobody's state colour all the
// same: the brand has to be free to move without dragging a Session state with
// it, which is why the panel declares it apart.
var brandBlue = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff"))

// quiet is the panel's own quiet — what it keeps for everything that is not
// asking anything of you.
var quiet = lipgloss.NewStyle().Faint(true)

// The panel's name is the harness's mark rather than another bold row: drawn
// like the section label under it, neither reads as what it is.
func TestTheDashboardsNameReadsAsTheHarnesssOwnMark(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	line, ok := rawLineFor(model, "GANYMEDE")
	if !ok {
		t.Fatalf("the panel does not name itself:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(brandBlue)) {
		t.Errorf("the name = %q, want it bold in the mock's own blue", line)
	}
}

// SELECTED is a label for the box under it rather than content of its own, so
// it drops to the panel's quiet: a section label carrying the same weight as
// what it labels is one more bold row to read past.
func TestTheSelectedLabelIsDrawnAsQuietlyAsItReads(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	line, ok := rawLineFor(model, "SELECTED")
	if !ok {
		t.Fatalf("the panel has no SELECTED label:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(quiet)) {
		t.Errorf("the section label = %q, want the panel's own quiet", line)
	}
}
