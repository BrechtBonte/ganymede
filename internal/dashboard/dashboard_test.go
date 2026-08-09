package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// render draws the Dashboard as it would appear in a sidepanel of the given size.
func render(width, height int) string {
	var model tea.Model = dashboard.New()
	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return model.View()
}

// The Dashboard has to live in a 40-column sidepanel: anything wider wraps and
// the repo tree turns to soup.
func TestDashboardFitsTheSidepanel(t *testing.T) {
	for _, line := range strings.Split(render(topology.SidepanelWidth, 45), "\n") {
		// lipgloss.Width measures what the terminal shows, ignoring styling.
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}

// With nothing running, the Dashboard says so rather than showing an empty
// frame that looks broken.
func TestDashboardNamesItselfAndReportsAnEmptyWorkingSet(t *testing.T) {
	view := render(topology.SidepanelWidth, 45)

	if !strings.Contains(strings.ToLower(view), "ganymede") {
		t.Errorf("the Dashboard does not name itself:\n%s", view)
	}
	if !strings.Contains(view, "No sessions") {
		t.Errorf("the Dashboard does not report an empty working set:\n%s", view)
	}
}
