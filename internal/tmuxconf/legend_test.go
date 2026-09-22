package tmuxconf

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTheLegendOffersPullsAmongTheChords(t *testing.T) {
	// The placement follows legendKeys's own rule — movement first, then the
	// chords nothing else advertises, because no row is ever standing on them.
	// No row is ever standing on p either.
	at := func(key string) int {
		for i, k := range legendKeys {
			if strings.HasPrefix(k, key) {
				return i
			}
		}
		return -1
	}
	popup, pulls, spawn := at("⌃` popup"), at("p pulls"), at("w spawn")
	if pulls < 0 {
		t.Fatalf("p pulls is not on the legend: %v", legendKeys)
	}
	if !(popup < pulls && pulls < spawn) {
		t.Errorf("p pulls is at %d, want between the popup chord (%d) and w spawn (%d)", pulls, popup, spawn)
	}
}

func TestOpenIsNoLongerOnlyATicket(t *testing.T) {
	// o now has two subjects, and a legend saying "open ticket" over a Pull
	// row would be the box's own words used to mean something they do not.
	for _, key := range legendKeys {
		if key == "o open ticket" {
			t.Error("the legend still says o open ticket")
		}
	}
	if !slices.Contains(legendKeys, "o open") {
		t.Errorf("the legend lost o entirely: %v", legendKeys)
	}
}

func TestSixKeysFitAnEightyColumnDock(t *testing.T) {
	// The strict gain the placement was chosen for. legend() is measured on
	// the plain phrases, so this counts them the same way.
	plain := 0
	visible := 0
	for _, key := range legendKeys {
		next := plain + lipgloss.Width(key)
		if visible > 0 {
			next += len(" · ")
		}
		if next > 80 {
			break
		}
		plain, visible = next, visible+1
	}
	if visible < 6 {
		t.Errorf("%d keys fit 80 columns, want at least 6", visible)
	}
}
