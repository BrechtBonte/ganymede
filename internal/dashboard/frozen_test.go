package dashboard_test

import (
	"strings"
	"testing"

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

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want the Frozen mark", line)
	}
}

// A pane showing the live Session earns nothing: a mark the rail always
// carries is one you stop reading.
func TestASessionRowShowsNoFrozenMarkWhenItsPaneIsLive(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.FrozenPanes{"max-paging-numbers-id": false})

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
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

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
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

// The edges off the pane-mode-changed hook, which are what make the mark
// quick — and, on the leaving edge, what take it down the moment you press q
// rather than up to half a minute later.
func TestTheMarkFollowsTheModeEdges(t *testing.T) {
	model := sidepanel(&jumps{}, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model, _ = model.Update(dashboard.Froze("max-paging-numbers-id"))
	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q after Froze, want the Frozen mark", line)
	}

	model, _ = model.Update(dashboard.Thawed("max-paging-numbers-id"))
	line, ok = lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
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

	line, ok := lineWith(tree(model), "max-paging-numbers")
	if !ok {
		t.Fatalf("no row for the session:\n%s", tree(model))
	}
	if !strings.Contains(line, frozenMark) {
		t.Errorf("row = %q, want the Frozen mark an edge reported before the row existed", line)
	}
}
