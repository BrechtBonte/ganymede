package dashboard_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// frozenMark is the mark a row carries while its pane is holding a mode over
// the live Session (CONTEXT.md, Pane view).
const frozenMark = "❄"

// A pane holding a mode shows a picture of the Session from whenever the mode
// was entered, while the Session itself carries on — so the row has to say so,
// or the rail reads normal while the screen reads dead.
func TestASessionRowShowsTheFrozenMark(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})

	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want the Frozen mark", line)
	}
}

// A pane showing the live Session earns nothing: a mark the rail always
// carries is one you stop reading.
func TestASessionRowShowsNoFrozenMarkWhenItsPaneIsLive(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": false})

	line := sessionRow(t, tree(model))
	if strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want no Frozen mark for a pane showing the live Session", line)
	}
}

// Both marks are things you have done to the row rather than what its Session
// is doing, and they share a column. Frozen reads first: whether the pane is
// still showing you the Session changes what the rest of the row means, and
// what a popup underneath it is running is a footnote to that.
func TestAFrozenRowStillShowsItsBusyPopupMark(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})
	model, _ = model.Update(dashboard.PopupStatuses{
		"/repos/service-billing": {Command: "composer install", Busy: true},
	})

	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) || !strings.Contains(line, popupBusy) {
		t.Fatalf("row = %q, want both the Frozen and busy-popup marks", line)
	}
	if strings.Index(line, frozenMark) > strings.Index(line, popupBusy) {
		t.Errorf("row = %q, want the Frozen mark before the busy-popup one", line)
	}
}

// Frozen is a fact about a Session's own pane. A repo's header row has no pane
// of its own to hold anything, so the mark must not climb onto it.
func TestARepoHeaderNeverShowsTheFrozenMark(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})

	header, ok := lineWith(tree(model), "service-billing")
	if !ok {
		t.Fatalf("no header row for the repo:\n%s", tree(model))
	}
	if strings.Contains(header, frozenMark) {
		t.Errorf("header = %q, want no Frozen mark on a row with no pane of its own", header)
	}
}

// Frozen is not Attention. It is your own doing, not the Session asking
// something of you — so a Session that was waiting on you goes on waiting, and
// counts for exactly as much as it did before.
func TestFreezingAPaneChangesNothingAboutAttention(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Ready))
	before := model.View()

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})
	after := model.View()

	if countOf(before, session.Ready.Glyph()) != countOf(after, session.Ready.Glyph()) {
		t.Errorf("freezing a pane changed the Ready marks:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if countOf(before, session.Blocked.Glyph()) != countOf(after, session.Blocked.Glyph()) {
		t.Errorf("freezing a pane changed the Blocked marks:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// countOf is how many times a mark appears in a view.
func countOf(view, mark string) int { return strings.Count(view, mark) }

// The rail draws the mark; the box spells it. A one-column glyph is a
// reminder for somebody who already knows what it means, and this is where
// the first person to see one finds out.
//
// It reads alongside the state rather than instead of it: the Session is
// still doing whatever it is doing, and the pane not showing you that is a
// separate fact about the same row.
func TestTheSelectedBoxSaysTheRowIsFrozen(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))
	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})
	// Onto the Session's own row: the cursor starts on the repo header.
	model = press(model, tea.KeyDown)

	box := detail(model)
	if !strings.Contains(box, "frozen") {
		t.Errorf("SELECTED = %q, want it to say the pane is frozen", box)
	}
	if !strings.Contains(box, string(session.Working)) {
		t.Errorf("SELECTED = %q, want the Session still reading as Working", box)
	}
}

// panes stands in for the harness's hand on tmux, recording what it was asked
// about so a test can check the sweep asks about the live Sessions.
type panes struct {
	frozen map[int]bool
	err    error
	asked  [][]int
}

func (p *panes) Frozen(pids []int) (map[int]bool, error) {
	p.asked = append(p.asked, pids)
	return p.frozen, p.err
}

// sweptFrozen runs what a Tick asked for and hands the Dashboard back what the
// harness said about the panes, the way the runtime does. Whatever else was
// asked for — the git read, the popup sweep, the next tick — is left running,
// exactly as popups_test.go's swept leaves them.
func sweptFrozen(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	read := make(chan dashboard.FrozenPanes, 1)
	var run func(tea.Cmd)
	run = func(ask tea.Cmd) {
		if ask == nil {
			return
		}
		go func() {
			switch msg := ask().(type) {
			case dashboard.FrozenPanes:
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
	case frozen := <-read:
		model, _ = model.Update(frozen)
	case <-time.After(10 * time.Second):
		t.Fatal("the Dashboard never asked the harness which panes are frozen")
	}
	return model
}

// The cross-check under the hook: what catches a mode entered while the
// Dashboard was down, a fragment not yet sourced into a running server, or an
// edge that never arrived.
func TestTheTickSweepsForFrozenPanes(t *testing.T) {
	running := live("max-paging-numbers", "/repos/service-billing", session.Working)
	swept := &panes{frozen: map[int]bool{running.PID: true}}
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Panes: swept}, running)

	_, cmd := model.Update(dashboard.Tick{})
	model = sweptFrozen(t, model, cmd)

	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want the Frozen mark the sweep found", line)
	}
	if len(swept.asked) == 0 || len(swept.asked[0]) != 1 || swept.asked[0][0] != running.PID {
		t.Errorf("the sweep asked about %v, want the live Session's pid %d", swept.asked, running.PID)
	}
}

// A sweep that failed says nothing about any pane, and leaves the last answer
// standing rather than blanking a mark tmux was merely slow to answer about.
func TestAFailedSweepLeavesTheLastAnswerStanding(t *testing.T) {
	running := live("max-paging-numbers", "/repos/service-billing", session.Working)
	swept := &panes{err: errors.New("tmux is not there")}
	model := dashboardOn(dashboard.Harness{Jumper: &jumps{}, Panes: swept}, running)
	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": true})

	_, cmd := model.Update(dashboard.Tick{})
	// Not sweptFrozen: the point is that no FrozenPanes message ever comes
	// back, so waiting for one would only ever time out. Running the batch
	// and going on is what the runtime does with a command that reports
	// nothing.
	if cmd != nil {
		go cmd()
	}

	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want a failed sweep to leave the Frozen mark standing", line)
	}
}

// The edges off the pane-mode-changed hook, which are what make the mark
// quick — and, on the leaving edge, what take it down the moment you press q
// rather than up to half a minute later.
func TestTheMarkFollowsTheModeEdges(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.Froze("max-paging-numbers-id"))
	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q after Froze, want the Frozen mark", line)
	}

	model, _ = model.Update(dashboard.Thawed("max-paging-numbers-id"))
	line = sessionRow(t, tree(model))
	if strings.Contains(line, frozenMark) {
		t.Errorf("row = %q after Thawed, want the Frozen mark gone", line)
	}
}

// An edge must not write through the map the last cross-check handed over: a
// message's own value belongs to whoever sent it, and a Model that edited one
// would be reaching back into a message it has already handled.
func TestAnEdgeDoesNotWriteThroughTheSweptMap(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	swept := dashboard.FrozenPanes{"max-paging-numbers-id": false}
	model, _ = model.Update(swept)
	model, _ = model.Update(dashboard.Froze("max-paging-numbers-id"))

	if swept["max-paging-numbers-id"] {
		t.Error("an edge wrote through the map the cross-check sent")
	}
}

// An edge about a Session the rail has never heard of is remembered rather
// than dropped: the hook resolves panes to Sessions off the registry, which
// can name one a beat before the working set the Dashboard is drawing does.
func TestAnEdgeAheadOfTheWorkingSetStillLands(t *testing.T) {
	model := sidepanel(&jumps{})

	model, _ = model.Update(dashboard.Froze("max-paging-numbers-id"))
	model, _ = model.Update(dashboard.Sessions{
		live("max-paging-numbers", "/repos/service-billing", session.Working),
	})

	line := sessionRow(t, tree(model))
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want the Frozen mark an edge reported before the row existed", line)
	}
}
