package dashboard_test

import (
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// headerOfPanel is the panel's first line — the Dashboard's own header — as the
// eye reads it and as the terminal is given it.
//
// One render answers both, unlike panelLines: a panel drawn a second time could
// be drawn on the far side of a minute's turn, and the two headers would then be
// reading different faces.
func headerOfPanel(model tea.Model) (stripped, raw string) {
	raw, _, _ = strings.Cut(model.View(), "\n")
	return ansi.Strip(raw), raw
}

// faces is what the header's clock may honestly be reading: the minute the
// panel was drawn in, and the minute it may have turned into between the draw
// and the assertion. A test that insisted on one of them would fail once a
// minute, at the moment nobody is watching.
func faces(drawnAt time.Time) []string {
	return []string{drawnAt.Format("15:04"), time.Now().Format("15:04")}
}

// endsWithAFace says the header ends with one of the faces the clock may
// honestly be reading, and returns the one it read.
func endsWithAFace(header string, faces []string) (string, bool) {
	for _, face := range faces {
		if strings.HasSuffix(header, face) {
			return face, true
		}
	}
	return "", false
}

// Ghostty runs fullscreen with the menu bar hidden, so the Dock is the only
// place left to read the time — and it goes at the header's far end, after the
// counts, which keep the place they had.
func TestTheHeaderCarriesTheTimeAfterTheAttentionCounts(t *testing.T) {
	drawnAt := time.Now()
	header, _ := headerOfPanel(sidepanel(&jumps{},
		live("aaa-blocked", "/repos/service-billing", session.Blocked)))

	face, ok := endsWithAFace(header, faces(drawnAt))
	if !ok {
		t.Fatalf("header = %q, want the time at its far end", header)
	}
	// One space between the counts and the clock, and the counts still first:
	// the clock is what was added, and it costs them nothing.
	if !strings.HasSuffix(header, session.Blocked.Glyph()+" 1 "+face) {
		t.Errorf("header = %q, want the clock after the Attention counts", header)
	}
}

// The time is a thing to glance at rather than a thing waiting on you, so it
// reads in the panel's own quiet — beside counts that are drawn in the colours
// of the tiers they are counting.
func TestTheHeadersClockIsDrawnQuietly(t *testing.T) {
	drawnAt := time.Now()
	model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", session.Working))

	header, raw := headerOfPanel(model)
	face, ok := endsWithAFace(header, faces(drawnAt))
	if !ok {
		t.Fatalf("header = %q, want the time at its far end", header)
	}
	if !strings.Contains(raw, styleCodeOf(quiet)+face) {
		t.Errorf("header = %q, want the clock drawn in the panel's own quiet", raw)
	}
}

// The clock is a third clock alongside the half-minute tick and the spinner,
// and unlike the spinner it never stops for want of something to animate: a
// Dashboard sitting at a quiet prompt all afternoon still has to be showing the
// right time when you look up at it.
func TestTheHeadersClockKeepsGoingWhetherOrNotAnythingIsAnimating(t *testing.T) {
	for _, c := range []struct {
		what  string
		state session.State
	}{
		{"a working set with nothing animating on it", session.Idle},
		{"one with a Working Session in it", session.Working},
	} {
		model := sidepanel(&jumps{}, live("ganymede-78", "/repos/ganymede", c.state))

		if _, cmd := model.Update(dashboard.Minute{}); cmd == nil {
			t.Errorf("on %s the header's clock stopped rescheduling itself", c.what)
		}
	}
}

// A working set asking nothing of you draws no counts at all, and the gap they
// leave is not a column the clock has to carry: a header assembled with an empty
// count in it would spend one on nothing, and the name is what pays for it.
func TestTheAbsentCountsLeaveNoDoubleSpaceBeforeTheClock(t *testing.T) {
	// Narrow enough that the name and the clock meet in the middle, which is
	// where a column spent on nothing can be seen at all: at the sidepanel's own
	// width the gap between them swallows it.
	drawnAt := time.Now()
	model := railSized(14, 45, nil, live("ganymede-78", "/repos/ganymede", session.Working))
	header, _ := headerOfPanel(model)

	face, ok := endsWithAFace(header, faces(drawnAt))
	if !ok {
		t.Fatalf("header = %q, want the time at its far end", header)
	}
	if header != "GANYMEDE "+face {
		t.Errorf("header = %q, want %q — the name whole and one space before the clock", header, "GANYMEDE "+face)
	}
}
