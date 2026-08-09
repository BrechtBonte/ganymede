// Package workingset settles which repos the Dashboard shows.
//
// The Dashboard is a sidepanel, not an index: it is worth reading because it
// is short. So a repo earns its row by being somewhere you are actually
// working — something running in it, a root you have reserved, or a place you
// were recently enough that you have not finished with it — and loses the row
// again once none of those is true. Everything else is a keystroke away in the
// picker, over the full inventory.
//
// Nothing here reads a file or asks git anything. It is the rule on its own,
// so that what falls off the Dashboard and when is a thing a test can state.
package workingset

import (
	"slices"
	"time"
)

// Window is how long a repo stays in the working set after the last time you
// worked in it. A week covers the way work actually goes — a repo you were in
// on Friday is still yours on Monday — and is short enough that the Dashboard
// stays the handful of rows it is meant to be.
const Window = 7 * 24 * time.Hour

// Membership is everything the working set is worked out from.
type Membership struct {
	// Live is the Main root of every Session the Dashboard is showing,
	// including Sessions running outside every scan root: the registry's
	// working directory is ground truth for where a Session belongs.
	Live []string
	// Claimed is the roots you have reserved.
	Claimed []string
	// Active is when each repo was last worked in, as the harness state
	// remembers it.
	Active map[string]time.Time
	// Window is how long a repo stays after the last activity in it. Zero
	// means Window.
	Window time.Duration
}

// Roots is the working set: the Main roots the Dashboard shows, each once and
// in a settled order.
//
// A repo drops off only when every reason for it has gone. A live Session or a
// Claim keeps it there however long ago the harness last recorded activity in
// it — one is where you are working now, and the other a decision you made on
// purpose, and a clock is not entitled to overrule either.
func (m Membership) Roots(now time.Time) []string {
	window := m.Window
	if window <= 0 {
		window = Window
	}

	shown := make(map[string]bool, len(m.Live)+len(m.Claimed)+len(m.Active))
	for _, root := range m.Live {
		shown[root] = true
	}
	for _, root := range m.Claimed {
		shown[root] = true
	}
	for root, at := range m.Active {
		// A stamp in the future is a clock that has jumped, not a repo from
		// next week: it is recent by every reading that matters here.
		if !at.Before(now.Add(-window)) {
			shown[root] = true
		}
	}

	if len(shown) == 0 {
		return nil
	}
	roots := make([]string, 0, len(shown))
	for root := range shown {
		roots = append(roots, root)
	}
	slices.Sort(roots)
	return roots
}
