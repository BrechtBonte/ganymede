// Package dashboard renders the Dashboard: the always-visible sidepanel TUI
// listing the working set of repos and their Sessions.
//
// It draws what the session registry reports and nothing it has invented, so
// the states on show are the ones the registry can tell apart — Working,
// Blocked, Idle and Shell. A Session whose row disappears is Gone.
package dashboard

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Sessions is a fresh account of the working set, as the registry watch
// reports it.
type Sessions []registry.Session

// watchEnded says the registry watch has stopped reporting. The Dashboard
// keeps showing what it last drew rather than blanking the tree.
type watchEnded struct{}

// Jumper puts a Session in front of you by steering the working client to the
// pane it is running in.
type Jumper interface {
	Jump(pid int) error
}

// Model is the Dashboard's bubbletea model.
type Model struct {
	width, height int
	sessions      <-chan []registry.Session
	jumper        Jumper
	rows          []row
	cursor        int
	// roots remembers which Main root a Session's directory belongs to.
	roots map[string]string
	// notice is the last thing the Dashboard was asked to do and could not.
	notice string
}

// New returns a Dashboard drawing the working sets that arrive on sessions and
// jumping through jumper. It is sized for the sidepanel until the terminal says
// otherwise.
func New(sessions <-chan []registry.Session, jumper Jumper) Model {
	return Model{
		width:    topology.SidepanelWidth,
		height:   45,
		sessions: sessions,
		jumper:   jumper,
	}
}

func (m Model) Init() tea.Cmd { return waitFor(m.sessions) }

// waitFor takes the next working set off the watch.
func waitFor(sessions <-chan []registry.Session) tea.Cmd {
	if sessions == nil {
		return nil
	}
	return func() tea.Msg {
		set, ok := <-sessions
		if !ok {
			return watchEnded{}
		}
		return Sessions(set)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case Sessions:
		return m.showing(msg), waitFor(m.sessions)
	case watchEnded:
		return m, nil
	case tea.KeyMsg:
		return m.pressed(msg)
	}
	return m, nil
}

// showing redraws the tree around a new working set, leaving the selection on
// the row it was on however far that row has moved.
func (m Model) showing(sessions []registry.Session) Model {
	var selected string
	if m.cursor < len(m.rows) {
		selected = m.rows[m.cursor].key()
	}

	if m.roots == nil {
		m.roots = map[string]string{}
	}
	m.rows = rowsOf(sessions, m.rootOf)
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == selected {
			m.cursor = i
			break
		}
	}
	return m
}

// rootOf is repo.Root, remembering what it answered. Working a root out costs
// a git subprocess, the tree is rebuilt every time any Session changes state,
// and a directory does not move house while the harness is up.
func (m Model) rootOf(dir string) string {
	if root, known := m.roots[dir]; known {
		return root
	}
	root := repo.Root(dir)
	m.roots[dir] = root
	return root
}

func (m Model) pressed(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Whatever the Dashboard last could not do has been read by now.
	m.notice = ""

	switch msg.Type {
	case tea.KeyCtrlC:
		// The Dashboard is meant to stay up for as long as the harness does,
		// so it answers to no quit key. Ctrl+C is left alone for the times you
		// are running it by hand.
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	case tea.KeyEnter:
		m = m.jump()
	}
	return m, nil
}

// jump puts the selected Session in front of you. A repo's header row is not a
// Session and has nowhere to go.
func (m Model) jump() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	session := m.rows[m.cursor].session
	if session == nil || m.jumper == nil {
		return m
	}
	if err := m.jumper.Jump(session.PID); err != nil {
		m.notice = err.Error()
	}
	return m
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	ruleStyle  = lipgloss.NewStyle().Faint(true)
	quietStyle = lipgloss.NewStyle().Faint(true)
	repoStyle  = lipgloss.NewStyle().Bold(true)
	// The selected row is inverted and otherwise drawn plainly: a state colour
	// nested inside the inversion fights with it.
	selectedStyle = lipgloss.NewStyle().Reverse(true)
)

// glyphs are how each state reads at a glance, one column wide, from the
// validated sidepanel mock.
var glyphs = map[registry.State]string{
	registry.Blocked: "█",
	registry.Working: "⠿",
	registry.Idle:    "○",
	registry.Shell:   "❯",
}

var stateStyles = map[registry.State]lipgloss.Style{
	registry.Blocked: lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")),
	registry.Working: lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff")),
	registry.Shell:   lipgloss.NewStyle().Foreground(lipgloss.Color("#d2a8ff")),
	registry.Idle:    lipgloss.NewStyle().Faint(true),
}

func (m Model) View() string {
	rule := ruleStyle.Render(strings.Repeat("─", m.width))
	detail := m.detail()

	// The frame the tree lives in: the title and its rule above, the detail
	// box's rule and heading below.
	space := m.height - 4 - len(detail)
	if space < 0 {
		// A sidepanel with no room for both gives up detail before it gives up
		// the tree.
		detail = detail[:max(0, len(detail)+space)]
		space = 0
	}

	lines := []string{titleStyle.Render(truncate("GANYMEDE", m.width)), rule}
	lines = append(lines, m.tree(space)...)
	lines = append(lines, rule, titleStyle.Render(truncate("SELECTED", m.width)))
	lines = append(lines, detail...)
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// tree draws the repo tree, showing as much of it as space allows and keeping
// the selection in view.
func (m Model) tree(space int) []string {
	if len(m.rows) == 0 {
		return clip(m.nothingRunning(), space)
	}

	first := 0
	if len(m.rows) > space {
		// Centre what is on show on the selection.
		first = min(max(0, m.cursor-space/2), len(m.rows)-space)
	}
	lines := make([]string, 0, space)
	for i := first; i < len(m.rows) && len(lines) < space; i++ {
		lines = append(lines, m.line(i))
	}
	return lines
}

// nothingRunning says so, rather than leaving an empty frame that reads as a
// Dashboard which has broken.
func (m Model) nothingRunning() []string {
	return []string{
		quietStyle.Render(truncate("No sessions.", m.width)),
		"",
		quietStyle.Render(truncate("Repos with a live Session, a", m.width)),
		quietStyle.Render(truncate("Claimed root, or recent activity", m.width)),
		quietStyle.Render(truncate("appear here.", m.width)),
	}
}

// line draws one row of the tree.
func (m Model) line(i int) string {
	r := m.rows[i]
	if r.session == nil {
		name := truncate(r.label(), m.width)
		if i == m.cursor {
			return selectedStyle.Width(m.width).Render(name)
		}
		return repoStyle.Render(name)
	}

	// Two columns of indent put a Session under its repo; then the state
	// glyph, which is what the eye runs down.
	const indent = "  "
	glyph := glyphs[r.session.State]
	name := truncate(r.session.Name, m.width-lipgloss.Width(indent+glyph+" "))
	if i == m.cursor {
		return selectedStyle.Width(m.width).Render(indent + glyph + " " + name)
	}
	return indent + stateStyles[r.session.State].Render(glyph) + " " + name
}

// detail is the SELECTED box: what the highlighted row has no room to say.
func (m Model) detail() []string {
	lines := m.selected()
	if m.notice != "" {
		lines = append(lines, stateStyles[registry.Blocked].Render(truncate(m.notice, m.width)))
	}
	return lines
}

func (m Model) selected() []string {
	if m.cursor >= len(m.rows) {
		return []string{quietStyle.Render("—")}
	}

	r := m.rows[m.cursor]
	if r.session == nil {
		return []string{
			repoStyle.Render(truncate(r.label(), m.width)),
			quietStyle.Render(shorten(r.root, m.width)),
		}
	}

	state := stateStyles[r.session.State]
	lines := []string{
		state.Render(glyphs[r.session.State]) + " " +
			truncate(string(r.session.State)+" · "+r.session.Name, m.width-2),
	}
	if r.session.Reason != "" {
		// Blocked is always displayed with its reason.
		lines = append(lines, state.Render(truncate(r.session.Reason, m.width)))
	}
	return append(lines,
		quietStyle.Render(shorten(r.session.Dir, m.width)),
		quietStyle.Render(truncate("⏎ jump", m.width)),
	)
}

// clip keeps at most space lines.
func clip(lines []string, space int) []string {
	if len(lines) <= space {
		return lines
	}
	return lines[:max(0, space)]
}

// truncate keeps a line inside the sidepanel rather than letting it wrap.
// Width is counted in terminal columns, not characters: a Session named in a
// script whose characters are two columns wide would otherwise wrap and push
// every row below it out of step with the selection.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "")
}

// shorten fits a path into width by keeping its tail, which is the end that
// says which checkout you are looking at.
func shorten(path string, width int) string {
	path = underHome(path)
	if width <= 0 {
		return ""
	}
	if over := ansi.StringWidth(path) - width; over >= 0 {
		// One column of the budget goes to saying something was cut off.
		return ansi.TruncateLeft(path, over+1, "…")
	}
	return path
}

// underHome writes a path under the home directory the way you would say it.
// The separator has to be there: /Users/you-archive is not inside /Users/you.
func underHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
