package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
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

// The keys the box offers are found by their key character, not read as a
// sentence: the character stands in the panel's normal foreground and the label
// saying what it does stays quiet behind it.
func TestASessionRowsOfferingStandsItsKeysOutFromTheirLabels(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))
	model = press(model, tea.KeyDown) // onto the Session's own row

	line, ok := rawLineFor(model, "⏎ jump")
	if !ok {
		t.Fatalf("the box offers the selected Session nothing:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, "⏎") {
		t.Errorf("offering = %q, want the key character in the panel's normal foreground", line)
	}
	if !strings.Contains(line, styleCodeOf(quiet)+"jump") {
		t.Errorf("offering = %q, want the key's label left quiet behind it", line)
	}
	// A key further along the line reads the same way: the one before it
	// finishing quietly is not what makes the next one findable.
	if !strings.Contains(line, "t "+styleCodeOf(quiet)+"ticket") {
		t.Errorf("offering = %q, want every key character standing out, not only the first", line)
	}
}

// A repo header's own offering reads the same way — and says the same words: `⏎
// go to repo` is doing honest work that `⏎ jump` would blur, so the labels are
// left exactly as they were and only their weight changes.
func TestARepoHeadersOfferingStandsItsKeysOutFromTheirLabels(t *testing.T) {
	// The selection opens on the first row, which is the repo's header.
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	line, ok := rawLineFor(model, "⏎ go to repo")
	if !ok {
		t.Fatalf("the box offers the selected repo nothing:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, "⏎") {
		t.Errorf("offering = %q, want the key character in the panel's normal foreground", line)
	}
	if !strings.Contains(line, styleCodeOf(quiet)+"go to repo") {
		t.Errorf("offering = %q, want the header's own label kept as it was, and left quiet", line)
	}
	if !strings.Contains(line, "w "+styleCodeOf(quiet)+"spawn") {
		t.Errorf("offering = %q, want every key character standing out, not only the first", line)
	}
}
