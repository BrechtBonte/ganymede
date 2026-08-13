// Package tile carries the Blocked count to Ganymede's own Dock tile — the
// harness's presence outside the emulator window, where a standing count can
// be read from whatever application you are actually in.
//
// Ghostty's own tile cannot be badged: macOS keeps a Dock tile private to the
// process that owns it, and Ghostty offers no badge of its own. So the count
// goes on Ganymede.app, whose process this package spawns and then talks to
// down a pipe. Everything the tile shows is decided here; the app bundle's own
// process renders and decides nothing.
package tile

import (
	"strconv"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// Label is what the Tile shows: how many Sessions cannot continue without your
// decision, and nothing at all when none of them can.
//
// Ready is deliberately absent. It already has the rail and its own delayed,
// silent notification, and a single number outside the Dashboard cannot say
// which tier it is about — so this one is always about the tier you have to
// act on.
func Label(waiting session.Attention) string {
	if waiting.Blocked == 0 {
		return ""
	}
	return strconv.Itoa(waiting.Blocked)
}
