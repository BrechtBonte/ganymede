package dashboard_test

import (
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// popupBusy is the mark a busy popup earns its owner's row (§8).
const popupBusy = "⏵"

// popups records what it was asked to sweep, and what the Dashboard told it
// is selected, standing in for the harness talking to tmux.
type popups struct {
	swept    [][]string
	selected []string
	statuses map[string]popup.Status
}

func (p *popups) Sweep(liveDirs []string) (map[string]popup.Status, error) {
	p.swept = append(p.swept, liveDirs)
	return p.statuses, nil
}

func (p *popups) Selected(dir string) error {
	p.selected = append(p.selected, dir)
	return nil
}

// swept runs what a Tick asked for and hands the Dashboard back what the
// harness said the popups are doing, the way the runtime does. Whatever else
// was asked for — the git read, the next tick itself — is left running,
// exactly as roots_test.go's cautioned leaves them.
func swept(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	read := make(chan dashboard.PopupStatuses, 1)
	var run func(tea.Cmd)
	run = func(ask tea.Cmd) {
		if ask == nil {
			return
		}
		go func() {
			switch msg := ask().(type) {
			case dashboard.PopupStatuses:
				select {
				case read <- msg:
				default:
				}
			case tea.BatchMsg:
				for _, inner := range msg {
					run(inner)
				}
			}
		}()
	}
	run(cmd)

	select {
	case statuses := <-read:
		model, _ = model.Update(statuses)
	case <-time.After(10 * time.Second):
		t.Fatal("the Dashboard never asked the harness to sweep the popups")
	}
	return model
}

// A hidden popup with a command running is what earns its owner's row a
// busy marker (§8).
func TestASessionRowShowsABusyPopupMarker(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Idle))

	model, _ = model.Update(dashboard.PopupStatuses{
		"/repos/service-billing": {Command: "composer install", Busy: true},
	})

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
	if !strings.Contains(line, popupBusy) {
		t.Errorf("row = %q, want the busy-popup marker", line)
	}
}

// An open popup sitting at its own prompt is not worth a mark — only a
// running command is (§8).
func TestASessionRowShowsNoMarkerWhenItsPopupIsIdle(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Idle))

	model, _ = model.Update(dashboard.PopupStatuses{
		"/repos/service-billing": {Command: "bash", Busy: false},
	})

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
	if strings.Contains(line, popupBusy) {
		t.Errorf("row = %q, want no busy marker for an idle popup", line)
	}
}

// A popup can be open over a repo's header row too — the rail's own case,
// with no Session claiming the root at all.
func TestARepoHeaderShowsABusyPopupMarkerForItsOwnDirectory(t *testing.T) {
	root := "/repos/service-billing"
	state := remembering(t)
	worked(t, state, root, time.Now())
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Activity: state})

	model, _ = model.Update(dashboard.PopupStatuses{root: {Command: "make", Busy: true}})

	line, ok := lineWith(tree(model), "service-billing")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", tree(model))
	}
	if !strings.Contains(line, popupBusy) {
		t.Errorf("header = %q, want the busy-popup marker", line)
	}
}

// The Tick that re-reads the cautions also sweeps the popups, asking about
// exactly the directories with a live Session — which is what decides
// whose popup gets killed for having gone Gone (§8).
func TestTheTickAsksTheHarnessToSweepPopupsForTheLiveSessions(t *testing.T) {
	fake := &popups{}
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Popups: fake},
		live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, cmd := model.Update(dashboard.Tick{})
	swept(t, model, cmd)

	if len(fake.swept) != 1 {
		t.Fatalf("Sweep called %d times, want 1", len(fake.swept))
	}
	found := false
	for _, dir := range fake.swept[0] {
		if dir == "/repos/service-billing" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sweep asked about %v, want it to include the Session's directory", fake.swept[0])
	}
}

// A repo can be on the rail with no live Session in it at all — Activity
// alone keeps it there — and a popup opened over its header row is exactly
// as live as one opened over a Session's own pane; Sweep has to be told
// about the root too, or it kills that popup the moment the repo has no
// Session, which is the ordinary state of a repo shown only because you
// worked in it recently.
func TestTheTickAlsoTellsTheHarnessAboutRepoRootsWithNoLiveSession(t *testing.T) {
	fake := &popups{}
	state := remembering(t)
	worked(t, state, "/repos/service-billing", time.Now())
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Popups: fake, Activity: state})

	model, cmd := model.Update(dashboard.Tick{})
	swept(t, model, cmd)

	if len(fake.swept) != 1 {
		t.Fatalf("Sweep called %d times, want 1", len(fake.swept))
	}
	found := false
	for _, dir := range fake.swept[0] {
		if dir == "/repos/service-billing" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sweep asked about %v, want it to include the repo's root", fake.swept[0])
	}
}

// The cursor's own directory is what a popup opened from the rail needs —
// the rail has no pane of its own to answer that question (§8).
func TestMovingTheCursorTellsTheHarnessWhichDirectoryIsSelected(t *testing.T) {
	fake := &popups{}
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Popups: fake},
		live("max-paging-numbers", "/repos/service-billing", session.Idle),
		live("FIRE-2841-paging", "/repos/service-ai-assistant", session.Idle),
	)
	if len(fake.selected) == 0 {
		t.Fatal("the initial row's directory was never told to the harness")
	}
	first := fake.selected[len(fake.selected)-1]

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyDown)

	last := fake.selected[len(fake.selected)-1]
	if last == first {
		t.Errorf("Selected still says %q after moving the cursor down", last)
	}
	if last != "/repos/service-billing" && last != "/repos/service-ai-assistant" {
		t.Errorf("Selected = %q, want one of the two repos' directories", last)
	}
}

// Telling tmux the same directory it was just told would be the same waste
// the strip's own write-guard exists to avoid — the tree rebuilds several
// times a second while an agent is working.
func TestSelectedIsNotWrittenAgainForTheSameDirectory(t *testing.T) {
	fake := &popups{}
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Popups: fake},
		live("max-paging-numbers", "/repos/service-billing", session.Idle))
	before := len(fake.selected)
	if before == 0 {
		t.Fatal("the initial row's directory was never told to the harness")
	}

	// A second account of the same working set rebuilds the tree without
	// moving the cursor off the row it is already on.
	model, _ = model.Update(dashboard.Sessions{live("max-paging-numbers", "/repos/service-billing", session.Working)})

	if len(fake.selected) != before {
		t.Errorf("Selected called %d times after a rebuild that did not move the cursor, want %d", len(fake.selected), before)
	}
}
