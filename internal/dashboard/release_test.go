package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/release"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// told is the panel with a check reported to it.
func told(model tea.Model, update release.Update) tea.Model {
	model, _ = model.Update(dashboard.Release(update))
	return model
}

// behind is a check that found the install behind what is published.
var behind = release.Update{Installed: "2.1.237", Latest: "2.1.240", Channel: "latest"}

// The notice goes directly under the header's rule, above the tree: it is
// about the harness rather than about a repo, and the header is where the
// panel says those things.
func TestTheUpdateNoticeSitsUnderTheHeadersRule(t *testing.T) {
	model := told(sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working)), behind)

	lines, _ := panelLines(model)

	if len(lines) < 3 {
		t.Fatalf("the panel drew %d lines:\n%s", len(lines), drawn(model))
	}
	if !strings.Contains(lines[2], "2.1.240") {
		t.Errorf("the line under the rule is %q, want the update notice", lines[2])
	}
	if !strings.Contains(lines[2], "2.1.237") {
		t.Errorf("the notice is %q, want the version installed here on it too", lines[2])
	}
}

// An install level with what is published draws nothing. A notice that is
// always there is one you stop seeing, which is the same reason the Attention
// counts are absent from a quiet working set.
func TestThereIsNoUpdateNoticeWhileTheInstallIsCurrent(t *testing.T) {
	for _, c := range []struct {
		what   string
		update release.Update
	}{
		{"an install level with the bucket", release.Update{Installed: "2.1.240", Latest: "2.1.240", Channel: "latest"}},
		{"an install ahead of it", release.Update{Installed: "2.1.241", Latest: "2.1.240", Channel: "latest"}},
		{"a check that was never made", release.Update{}},
	} {
		model := told(sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working)), c.update)

		if view := drawn(model); strings.Contains(view, "Claude Code") {
			t.Errorf("with %s the panel drew a notice:\n%s", c.what, view)
		}
	}
}

// The line the notice costs comes out of the tree, not off the foot. The
// SELECTED box is the one thing on the panel that is always in the same place,
// and a notice that shunted it down a line would take that away on exactly the
// days there was something else to read.
func TestTheUpdateNoticeCostsTheTreeALineRatherThanTheSelectedBox(t *testing.T) {
	sessions := []session.Session{
		live("ganymede-78", "/repos/ganymede", session.Working),
		live("billing-12", "/repos/service-billing", session.Blocked),
	}
	quiet := sidepanel(&jumps{}, sessions...)
	noticed := told(sidepanel(&jumps{}, sessions...), behind)

	before, _ := panelLines(quiet)
	after, _ := panelLines(noticed)

	if len(before) != len(after) {
		t.Fatalf("the panel drew %d lines with a notice and %d without", len(after), len(before))
	}
	if selectedFromFoot(t, before) != selectedFromFoot(t, after) {
		t.Errorf("SELECTED sits %d lines off the foot with a notice and %d without",
			selectedFromFoot(t, after), selectedFromFoot(t, before))
	}
}

// selectedFromFoot is how many lines up from the panel's last line the
// SELECTED label sits.
func selectedFromFoot(t *testing.T, lines []string) int {
	t.Helper()
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "SELECTED") {
			return len(lines) - 1 - i
		}
	}
	t.Fatalf("no SELECTED label on the panel:\n%s", strings.Join(lines, "\n"))
	return 0
}

// A sidepanel narrower than the notice loses the end of it rather than
// wrapping onto a second line, which would push the tree down by a line the
// frame never budgeted for.
func TestTheUpdateNoticeIsTruncatedRatherThanWrapped(t *testing.T) {
	model := told(railSized(18, 45, nil, live("ganymede-78", "/repos/ganymede", session.Working)), behind)

	lines, _ := panelLines(model)

	for _, line := range lines {
		if len([]rune(line)) > 18 {
			t.Errorf("line %q is %d columns wide, want at most 18", line, len([]rune(line)))
		}
	}
	if !strings.HasPrefix(lines[2], available+" Claude Code") {
		t.Errorf("the notice reads %q, want the mark and the name it had room for", lines[2])
	}
}

// available is the mark the notice is read by, as the panel draws it.
const available = "⇡"

// The mark and the version being published are what the notice is for, so they
// carry the harness's caution colour; what you are on is context, and reads in
// the panel's own quiet.
func TestTheUpdateNoticeIsDrawnInCautionOverQuiet(t *testing.T) {
	model := told(sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working)), behind)

	_, raw := panelLines(model)

	if !strings.HasPrefix(raw[2], styleCodeOf(cautionAmber)) {
		t.Errorf("the notice is %q, want it opening in the harness's caution colour", raw[2])
	}
	if !strings.Contains(raw[2], styleCodeOf(quiet)+" · on 2.1.237") {
		t.Errorf("the notice is %q, want the version installed here drawn quietly", raw[2])
	}
}
