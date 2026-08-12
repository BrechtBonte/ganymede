package dashboard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/ticket"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A Session holding its repo's Main root says so, rather than repeating the
// repo header immediately above it: Claude Code names such a Session
// <repo>-<xx>, and the row's widest column was spending itself on the repo
// name it had just been read under, truncated before the two characters that
// were the only new thing in it.
func TestASessionHoldingTheMainRootReadsMain(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")

	model := sidepanel(&jumps{}, live("service-ai-assistant-c9", root, session.Idle))

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "main") {
		t.Errorf("row = %q, want the Session holding the Main root to read main", row)
	}
	if strings.Contains(row, "service-ai-assistant-c9") {
		t.Errorf("row = %q, want the Session's own name off the row", row)
	}
}

// A Worktree session says which worktree, which is the whole question the row
// is answering: the Main root is free for a PR, and this is where the work is.
func TestAWorktreeSessionReadsItsWorktreeName(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "max-paging")

	model := sidepanel(&jumps{}, live("max-paging", at, session.Working))

	if row := sessionRow(t, tree(model)); !strings.Contains(row, "wt·max-paging") {
		t.Errorf("row = %q, want the worktree the Session has its hands on", row)
	}
}

// The label is the checkout's, not the Session's directory's. A Session
// working a few directories down inside a worktree is still working in that
// worktree, and a row named after the subdirectory would say something else.
func TestASessionInsideAWorktreeIsLabelledAfterTheWorktree(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := filepath.Join(worktree(t, root, "max-paging"), "internal", "dashboard")
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}

	model := sidepanel(&jumps{}, live("max-paging", at, session.Working))

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "wt·max-paging") {
		t.Errorf("row = %q, want the worktree rather than the directory the Session is standing in", row)
	}
	if strings.Contains(row, "dashboard") {
		t.Errorf("row = %q, want the subdirectory it happens to be in left off", row)
	}
}

// A worktree is usually named after the ticket it is for, and the row is
// already showing that ticket in its own column. The label drops it, so the
// part you can actually tell one worktree from another by is the part that
// survives the column.
func TestAWorktreeNamedAfterItsTicketLeavesTheTicketToTheTicketColumn(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "FIRE-2841-paging")

	model := knowing(
		&known{of: map[string]ticket.Key{at: "FIRE-2841"}},
		live("FIRE-2841-paging", at, session.Working),
	)

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "wt·paging") {
		t.Errorf("row = %q, want the ticket dropped from the label", row)
	}
	if !strings.Contains(row, "F-2841") {
		t.Errorf("row = %q, want the ticket still in its own column", row)
	}
}

// Dropping the key is only worth doing while something is left to read. A
// worktree named for its ticket and nothing else keeps the whole name, since
// wt· on its own says nothing at all.
func TestAWorktreeNamedOnlyForItsTicketKeepsIt(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "FIRE-2841")

	model := knowing(
		&known{of: map[string]ticket.Key{at: "FIRE-2841"}},
		live("FIRE-2841", at, session.Working),
	)

	if row := sessionRow(t, tree(model)); !strings.Contains(row, "wt·FIRE-2841") {
		t.Errorf("row = %q, want the whole name where dropping the key would leave nothing", row)
	}
}

// A label with no room left is cut at the end and says it was cut, the way
// every other name on the panel is: the head is where a worktree says what it
// is about.
func TestALabelTooLongForItsColumnIsElidedKeepingItsHead(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "account-allowances-for-the-credit-usage-service")

	model := knowing(
		&known{of: map[string]ticket.Key{at: "FIRE-2923"}},
		live("account-allowances", at, session.Working),
	)

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "wt·account") {
		t.Errorf("row = %q, want the head of the label kept", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("row = %q, want a label that did not fit to say it was cut", row)
	}
	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}

// A Session the working set never adopted — running somewhere that is no
// repository at all — must not be the one row that reads differently. It gets
// a label like every other row.
func TestASessionOutsideEveryScanRootStillGetsALabel(t *testing.T) {
	model := sidepanel(&jumps{}, live("stray-a1", t.TempDir(), session.Idle))

	row := sessionRow(t, tree(model))
	// Everything after the indent and the state glyph, which is what the
	// label column is.
	if label := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(row), session.Idle.Glyph())); label == "" {
		t.Errorf("row = %q, want a Session outside every scan root labelled too", row)
	}
}

// The cross-check checks the process and takes the rest as it finds it, so a
// Session can be reported with no directory at all. Asking git about nothing
// answers with the Dashboard's own checkout, and the row must not be labelled
// after a checkout its Session has never been in.
func TestASessionReportedWithoutADirectoryIsNotLabelledAfterOurOwn(t *testing.T) {
	stray := session.Session{PID: 4242, ID: "stray-id", Name: "stray-a1", State: session.Idle, Since: epoch}

	model := sidepanel(&jumps{}, stray)

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "stray-a1") {
		t.Errorf("row = %q, want a Session with no directory called by its own name", row)
	}
	if strings.Contains(row, "wt·") {
		t.Errorf("row = %q, want it not read as a worktree of the Dashboard's own checkout", row)
	}

	// And the box under it says the same thing: with no directory there is no
	// repo to name, and a lone "." would identify nothing at all.
	model = press(model, tea.KeyDown)
	if box := detail(model); !strings.Contains(box, "stray-a1") {
		t.Errorf("SELECTED = %q, want the Session named there too", box)
	}
}

// The key is dropped however the worktree spells it. Unlike reading a ticket
// out of a branch name, where upper case is what tells a key from a dependency
// bump, the key here is already known — and a worktree spelling it in lower
// case is still the row saying the same ticket twice.
func TestAWorktreeSpellingItsTicketInLowerCaseStillDropsIt(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "fire-2841-paging")

	model := knowing(
		&known{of: map[string]ticket.Key{at: "FIRE-2841"}},
		live("fire-2841-paging", at, session.Working),
	)

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "wt·paging") {
		t.Errorf("row = %q, want the ticket dropped from the label whatever its case", row)
	}
}

// The project key is the same on every row of every repo, so the row spells
// the part that differs: the initial, the hyphen and the number.
func TestTheRowsTicketIsTheProjectInitialAndTheNumber(t *testing.T) {
	root := mainRoot(t, "service-ai-credit-usage")

	model := knowing(
		&known{of: map[string]ticket.Key{root: "FIRE-2923"}},
		live("service-ai-credit-usage-c9", root, session.Working),
	)

	row := sessionRow(t, tree(model))
	if !strings.Contains(row, "F-2923") {
		t.Errorf("row = %q, want the ticket abbreviated to its initial and number", row)
	}
	if strings.Contains(row, "FIRE-2923") {
		t.Errorf("row = %q, want the project key spelled out only in the box", row)
	}
}

// Abbreviating on the row costs nothing because the whole key is in the box,
// on the same screen, where the key that opens it is offered too.
func TestTheSelectedBoxKeepsTheWholeTicket(t *testing.T) {
	root := mainRoot(t, "service-ai-credit-usage")
	model := knowing(
		&known{of: map[string]ticket.Key{root: "FIRE-2923"}},
		live("service-ai-credit-usage-c9", root, session.Working),
	)

	// Past the repo header, onto the Session.
	model = press(model, tea.KeyDown)

	if box := detail(model); !strings.Contains(box, "FIRE-2923") {
		t.Errorf("SELECTED = %q, want the whole ticket in it", box)
	}
}

// no ticket cost nine columns to say nothing, on the rows with the least to
// spare. The row leaves the column empty and the box goes on saying it, which
// is where the key that sets one is offered.
func TestASessionAboutNoTicketSaysSoInTheBoxRatherThanOnItsRow(t *testing.T) {
	root := mainRoot(t, "ganymede")
	model := knowing(&known{}, live("ganymede-51", root, session.Idle))

	row := sessionRow(t, tree(model))
	if strings.Contains(row, "no ticket") {
		t.Errorf("row = %q, want nothing at all in the ticket column", row)
	}

	model = press(model, tea.KeyDown)
	box := detail(model)
	if !strings.Contains(box, "no ticket") {
		t.Errorf("SELECTED = %q, want it to say the Session is about no ticket", box)
	}
	if !strings.Contains(box, "t ticket") {
		t.Errorf("SELECTED = %q, want the key that gives it one offered there", box)
	}
}

// A row reading main leaves you needing to know whose main, so the box names
// the repo where it used to name the Session. The name is dropped rather than
// given a line of its own: it is either Claude Code's <repo>-<xx> or the
// worktree name the row already shows, and the box is no longer than it was.
func TestTheSelectedBoxNamesTheRepoOnASessionRow(t *testing.T) {
	root := mainRoot(t, "service-ai-credit-usage")
	model := sidepanel(&jumps{}, live("service-ai-credit-usage-c9", root, session.Idle))

	model = press(model, tea.KeyDown)

	box := detail(model)
	if !strings.Contains(box, "service-ai-credit-usage") {
		t.Errorf("SELECTED = %q, want it to name the repo", box)
	}
	if strings.Contains(box, "service-ai-credit-usage-c9") {
		t.Errorf("SELECTED = %q, want the Session's own name dropped rather than given a line", box)
	}
}

// The marks are things you have done to the row rather than what its Session
// is doing, and relabelling the row must not cost you either of them: they
// keep their place between the state glyph and the label.
func TestTheMarksKeepTheirPlaceOnARelabelledRow(t *testing.T) {
	root := mainRoot(t, "service-billing")
	at := worktree(t, root, "max-paging")
	model := sidepanel(&jumps{}, live("max-paging", at, session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-id": true})
	model, _ = model.Update(dashboard.PopupStatuses{at: {Command: "composer install", Busy: true}})

	row := sessionRow(t, tree(model))
	label := strings.Index(row, "wt·max-paging")
	if label < 0 {
		t.Fatalf("row = %q, want the worktree label on it", row)
	}
	for _, mark := range []string{frozenMark, popupBusy} {
		at := strings.Index(row, mark)
		if at < 0 {
			t.Fatalf("row = %q, want the %q mark on it", row, mark)
		}
		if at > label {
			t.Errorf("row = %q, want the %q mark in front of the label", row, mark)
		}
	}
}
