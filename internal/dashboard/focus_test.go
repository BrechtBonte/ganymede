package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Styling only reaches the rendered string once lipgloss believes it is
// writing to a colour-capable terminal, which go test — having no tty — is
// not, by default.
func init() {
	lipgloss.SetColorProfile(termenv.ANSI)
}

// styleCodeOf is the escape sequence s opens its content with, asked of
// lipgloss itself rather than hardcoded, so a test reads the same story
// lipgloss does.
func styleCodeOf(s lipgloss.Style) string {
	rendered := s.Render("x")
	return rendered[:strings.Index(rendered, "x")]
}

// rawLineFor is the unstripped line drawing want, found by its stripped
// position: stripping never adds or removes a line, only the escape codes
// inside one.
func rawLineFor(model tea.Model, want string) (string, bool) {
	stripped := strings.Split(drawn(model), "\n")
	raw := strings.Split(model.View(), "\n")
	for i, line := range stripped {
		if strings.Contains(line, want) {
			return raw[i], true
		}
	}
	return "", false
}

var (
	reverseOnly  = lipgloss.NewStyle().Reverse(true)
	reverseFaint = lipgloss.NewStyle().Reverse(true).Faint(true)
)

// The selected row is the one place attention already knows to look, so
// dimming it once the Dashboard is not what your keystrokes reach says: still
// here, just not what you're looking at right now. This exercises a repo's
// own header row, sitting under the cursor by default.
func TestTheSelectedRepoHeaderDimsOnceTheDashboardIsBlurred(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	line, ok := rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want plainly reversed before any Blur", line)
	}

	model, _ = model.Update(tea.BlurMsg{})
	line, ok = rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo after Blur:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("header row = %q, want dimmer once the Dashboard is blurred", line)
	}

	model, _ = model.Update(tea.FocusMsg{})
	line, ok = rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo after Focus:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want plainly reversed again once the Dashboard regains focus", line)
	}
}

// The same dimming applies to a Session's own row, not just a repo header's.
func TestTheSelectedSessionRowDimsOnceTheDashboardIsBlurred(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))
	model = press(model, tea.KeyDown)

	line, ok := rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for the session:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("session row = %q, want plainly reversed before any Blur", line)
	}

	model, _ = model.Update(tea.BlurMsg{})
	line, ok = rawLineFor(model, "ganymede-78")
	if !ok {
		t.Fatalf("no row for the session after Blur:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseFaint)) {
		t.Errorf("session row = %q, want dimmer once the Dashboard is blurred", line)
	}
}

// A Dashboard that never hears a Blur at all — a terminal that does not
// forward tmux's focus-events — draws exactly as it always has: additive
// rather than a risk to every setup that predates it.
func TestTheSelectedRowStaysPlainWithoutAnyFocusReport(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Idle))

	line, ok := rawLineFor(model, "ganymede")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", drawn(model))
	}
	if !strings.HasPrefix(line, styleCodeOf(reverseOnly)) {
		t.Errorf("header row = %q, want plainly reversed by default", line)
	}
}
