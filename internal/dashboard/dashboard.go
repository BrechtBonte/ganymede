// Package dashboard renders the Dashboard: the always-visible sidepanel TUI
// listing the working set of repos and their Sessions.
//
// At this stage it is a placeholder. It draws the sidepanel's frame — title,
// repo tree, and SELECTED detail box — over an empty working set; reading
// Claude Code's per-session registry files comes later.
package dashboard

import (
	"strings"

	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the Dashboard's bubbletea model.
type Model struct {
	width  int
	height int
}

// New returns a Dashboard sized for the sidepanel until the terminal says
// otherwise.
func New() Model {
	return Model{width: topology.SidepanelWidth, height: 45}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		// The Dashboard is meant to stay up for as long as the harness does,
		// so it answers to no quit key. Ctrl+C still ends it, for the times
		// you are running it by hand.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	ruleStyle  = lipgloss.NewStyle().Faint(true)
	quietStyle = lipgloss.NewStyle().Faint(true)
)

func (m Model) View() string {
	rule := ruleStyle.Render(strings.Repeat("─", m.width))

	var b strings.Builder
	b.WriteString(titleStyle.Render(truncate("GANYMEDE", m.width)) + "\n")
	b.WriteString(rule + "\n")
	b.WriteString(quietStyle.Render(truncate("No sessions.", m.width)) + "\n")
	b.WriteString("\n")
	b.WriteString(quietStyle.Render(truncate("Repos with a live Session, a", m.width)) + "\n")
	b.WriteString(quietStyle.Render(truncate("Claimed root, or recent activity", m.width)) + "\n")
	b.WriteString(quietStyle.Render(truncate("appear here.", m.width)) + "\n")
	b.WriteString(rule + "\n")
	b.WriteString(titleStyle.Render(truncate("SELECTED", m.width)) + "\n")
	b.WriteString(quietStyle.Render(truncate("—", m.width)))
	return b.String()
}

// truncate keeps a line inside the sidepanel rather than letting it wrap.
func truncate(s string, width int) string {
	runes := []rune(s)
	if width <= 0 || len(runes) <= width {
		return s
	}
	return string(runes[:width])
}
