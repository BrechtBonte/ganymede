package session_test

import (
	"testing"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// Attention is what the harness counts on your behalf: the Sessions that
// cannot go on without a decision, and the turns you have not read.
func TestAttentionCountsWhatIsWaitingOnYou(t *testing.T) {
	got := session.AttentionIn([]session.Session{
		{Name: "a", State: session.Blocked},
		{Name: "b", State: session.Ready},
		{Name: "c", State: session.Ready},
		{Name: "d", State: session.Working},
		{Name: "e", State: session.Idle},
		{Name: "f", State: session.Shell},
	})

	if want := (session.Attention{Blocked: 1, Ready: 2}); got != want {
		t.Errorf("counted %+v, want %+v", got, want)
	}
	if !got.Any() {
		t.Error("something is waiting on you and Attention says nothing is")
	}
}

// A working set asking nothing of you is a quiet one, and the surfaces drawing
// the counts are meant to fall silent rather than show a pair of zeroes.
func TestNothingWaitingIsNoAttention(t *testing.T) {
	got := session.AttentionIn([]session.Session{
		{Name: "a", State: session.Working},
		{Name: "b", State: session.Idle},
	})

	if got.Any() {
		t.Errorf("nothing is waiting on you and Attention reports %+v", got)
	}
}

// One mark per state wherever the harness draws it — the rail, the attention
// strip — so that two surfaces cannot end up saying the same thing in two
// ways. One column wide: the rail has no more to spare.
func TestEveryStateHasOneMarkOfItsOwn(t *testing.T) {
	seen := map[string]session.State{}
	for _, state := range []session.State{
		session.Blocked, session.Ready, session.Working, session.Idle, session.Shell,
	} {
		glyph := state.Glyph()
		if glyph == "" {
			t.Errorf("%s has no mark", state)
			continue
		}
		if other, taken := seen[glyph]; taken {
			t.Errorf("%s and %s are both drawn %q", state, other, glyph)
		}
		seen[glyph] = state
	}
}

// Blocked and Ready are told apart by colour as well as by mark, because the
// strip is read out of the corner of your eye.
func TestAttentionIsColouredApart(t *testing.T) {
	blocked, ready := session.Blocked.Colour(), session.Ready.Colour()

	if blocked == "" || ready == "" {
		t.Fatalf("Attention is not coloured: Blocked %q, Ready %q", blocked, ready)
	}
	if blocked == ready {
		t.Errorf("Blocked and Ready are both %q", blocked)
	}
}
