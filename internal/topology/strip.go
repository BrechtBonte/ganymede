package topology

import (
	"fmt"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/tmuxconf"
)

// Show puts the ambient attention strip where you are already looking: the
// tmux status line of the Session you are working in. It is deliberate
// redundancy with the rail — the count sits under your eye line in the pane
// you are typing in, so a Session that has stopped and is waiting on you is
// not something you have to glance sideways to find out about.
//
// The whole strip goes into one option, which the installed configuration has
// already placed in the status line. Writing it is enough to show it: tmux
// redraws its clients when an option changes, so the count follows the working
// set rather than the status line's own timer.
func (h Harness) Show(waiting session.Attention) error {
	if err := h.sessions().run("set", "-g", tmuxconf.AttentionOption, strip(waiting)); err != nil {
		return fmt.Errorf("show the attention strip: %w", err)
	}
	return nil
}

// strip writes the counts the way the status line reads them: "█ 2 blocked ·
// ● 1 ready", each tier in the mark and colour the rail gives it, Blocked
// first because that is the order you deal with them in.
//
// A tier with nothing in it is left out, and a working set asking nothing of
// you is a blank strip: a status line that is always lit is one you stop
// reading, which would cost the Blocked count the only thing it is for.
func strip(waiting session.Attention) string {
	var tiers []string
	if waiting.Blocked > 0 {
		tiers = append(tiers, tier(session.Blocked, waiting.Blocked, "blocked"))
	}
	if waiting.Ready > 0 {
		tiers = append(tiers, tier(session.Ready, waiting.Ready, "ready"))
	}
	return strings.Join(tiers, " · ")
}

// tier writes one count, in its state's colour, handing the status line back
// its own styling afterwards so the rest of the line is left as the user has
// it.
func tier(state session.State, count int, word string) string {
	return fmt.Sprintf("#[fg=%s]%s %d %s#[default]", state.Colour(), state.Glyph(), count, word)
}
