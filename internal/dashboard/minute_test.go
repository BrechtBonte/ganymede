package dashboard

import (
	"testing"
	"time"
)

// at is a time of day, to the fraction of a second.
func at(zone *time.Location, hour, minute, second int, frac time.Duration) time.Time {
	return time.Date(2026, 8, 12, hour, minute, second, int(frac), zone)
}

// The header's clock has to turn over when the minute does. Hung on ticking()'s
// half minute it would fire thirty seconds after that tick last fired, and read
// up to half a minute late — so its own delay is whatever is left of the minute
// it is scheduled in, rather than an interval of its own. This is the seam where
// that arithmetic can be shown times of day the panel cannot be talked into
// being drawn at; what the panel draws is asserted through View() in
// clock_test.go.
func TestTheClocksNextRedrawIsWhateverIsLeftOfTheMinute(t *testing.T) {
	// A zone offset by three quarters of an hour, to show the turn is read off
	// the wall clock's own seconds rather than off an hour boundary somewhere
	// else in the world.
	kathmandu := time.FixedZone("Asia/Kathmandu", 5*3600+45*60)

	for _, c := range []struct {
		what string
		now  time.Time
		want time.Duration
	}{
		{"on the turn", at(time.Local, 14, 32, 0, 0), time.Minute},
		{"halfway through", at(time.Local, 14, 32, 30, 0), 30 * time.Second},
		{"a fraction short of the turn", at(time.Local, 14, 32, 59, 750*time.Millisecond), 250 * time.Millisecond},
		{"the last minute of the hour", at(time.Local, 14, 59, 45, 0), 15 * time.Second},
		{"in a zone offset by three quarters of an hour", at(kathmandu, 14, 32, 20, 0), 40 * time.Second},
	} {
		got := untilMinute(c.now)
		if got != c.want {
			t.Errorf("%s: the clock's next redraw is %s away, want %s", c.what, got, c.want)
		}
		// And it lands on the far side of the turn rather than on the minute
		// that is ending, whatever the arithmetic above is doing: a redraw a
		// moment early would draw the same face again and then not come back
		// for another minute.
		if next := c.now.Add(got); next.Truncate(time.Minute) != next || !next.After(c.now) {
			t.Errorf("%s: the redraw lands at %s, want it on the next minute's own boundary", c.what, next)
		}
	}
}
