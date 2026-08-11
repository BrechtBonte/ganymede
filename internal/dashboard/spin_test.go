package dashboard_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
)

// A Working row is the one thing on the rail actually moving: a Spin message
// steps its glyph to the spinner's next frame.
func TestASpinMessageAdvancesAWorkingSessionsGlyph(t *testing.T) {
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working))

	before := drawn(model)
	if !strings.Contains(before, session.Working.Frame(0)) {
		t.Fatalf("view = %q, want the spinner's first frame", before)
	}

	model, _ = model.Update(dashboard.Spin{})
	after := drawn(model)
	if !strings.Contains(after, session.Working.Frame(1)) {
		t.Errorf("view = %q, want the spinner's second frame", after)
	}
}

// A Working Session arriving starts the spinner clock, and it keeps
// rescheduling itself only for as long as something still is Working — once
// the last one goes Idle, the next Spin stops it rather than running the
// clock forever in the background of a quiet Dashboard.
func TestTheSpinnerStopsOnceNothingIsWorkingAnyMore(t *testing.T) {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})

	model, cmd := model.Update(dashboard.Sessions{live("ganymede-78", "/repos/ganymede", session.Working)})
	if cmd == nil {
		t.Fatal("a Working Session arrived and the Dashboard did not start the spinner")
	}

	model, _ = model.Update(dashboard.Sessions{live("ganymede-78", "/repos/ganymede", session.Idle)})
	if _, cmd := model.Update(dashboard.Spin{}); cmd != nil {
		t.Error("nothing is Working any more and the spinner kept rescheduling itself")
	}
}

// The repo header row borrows Working's own animated mark when the Session
// actually holding the root is Working — the same borrowing rootStyle
// already makes for the header row's colour.
func TestAWorkingHolderAnimatesTheRepoHeaderRow(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")
	model := rail(t, live("ai-assistant-b3", root, session.Working))

	if line := headerOf(t, model, root); !strings.Contains(line, session.Working.Frame(0)) {
		t.Errorf("header = %q, want the spinner's first frame", line)
	}
}

// The header row's mark advances the same way a Session row's does.
func TestASpinMessageAdvancesTheRepoHeaderGlyphToo(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")
	model := rail(t, live("ai-assistant-b3", root, session.Working))

	model, _ = model.Update(dashboard.Spin{})
	if line := headerOf(t, model, root); !strings.Contains(line, session.Working.Frame(1)) {
		t.Errorf("header = %q, want the spinner's second frame", line)
	}
}

// A root is InUse the moment anybody holds it (state.go) — Idle, Ready,
// Blocked, and Shell included — and that is most of the time a repo sits on
// the rail at all. Gating the spinner on InUse alone would keep it running
// for as long as any of them held the root; only an actually-Working holder
// may start it.
func TestAnOccupiedButIdleRootNeverStartsTheSpinner(t *testing.T) {
	root := mainRoot(t, "service-ai-assistant")
	model := rail(t, live("ai-assistant-b3", root, session.Idle))

	if _, cmd := model.Update(dashboard.Spin{}); cmd != nil {
		t.Error("an Idle holder should never have started the spinner, but it kept rescheduling")
	}
}
