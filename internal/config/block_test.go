package config_test

import (
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// A file with no block of the harness's own gets one appended.
func TestWithBlockAppendsWhenAbsent(t *testing.T) {
	got := config.WithBlock("set -g mouse on\n", []string{"source-file -q \"/frag\""})

	if !strings.Contains(got, "set -g mouse on") {
		t.Errorf("the existing line was lost:\n%s", got)
	}
	if !strings.Contains(got, "source-file -q \"/frag\"") {
		t.Errorf("the new line was not appended:\n%s", got)
	}
	if strings.Count(got, "# >>> ganymede >>>") != 1 {
		t.Errorf("want exactly one begin marker:\n%s", got)
	}
}

// Calling it again with the same lines changes nothing: installing is safe to
// repeat.
func TestWithBlockIsIdempotent(t *testing.T) {
	lines := []string{"source-file -q \"/frag\""}
	once := config.WithBlock("set -g mouse on\n", lines)
	twice := config.WithBlock(once, lines)

	if once != twice {
		t.Errorf("a second call changed the result:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

// Re-run with different lines rewrites the block in place rather than
// appending a second one, and leaves what comes after it untouched.
func TestWithBlockReplacesInPlace(t *testing.T) {
	before := config.WithBlock("set -g mouse on\n", []string{"source-file -q \"/old\""})
	before += "set -g history-limit 50000\n"

	after := config.WithBlock(before, []string{"source-file -q \"/new\""})

	if strings.Contains(after, "/old") {
		t.Errorf("the old line survived:\n%s", after)
	}
	if !strings.Contains(after, "/new") {
		t.Errorf("the new line is missing:\n%s", after)
	}
	if strings.Count(after, "# >>> ganymede >>>") != 1 {
		t.Errorf("want exactly one begin marker after replacing:\n%s", after)
	}
	if !strings.Contains(after, "set -g mouse on") || !strings.Contains(after, "set -g history-limit 50000") {
		t.Errorf("lines around the block were lost:\n%s", after)
	}
}

// A block whose end marker has gone missing — hand-edited, mangled by an
// editor — must not cost the lines after it: only the opening marker is
// replaced, and everything below it is the user's, however it got there.
func TestWithBlockRepairsAnUnterminatedBlock(t *testing.T) {
	mangled := "set -g mouse on\n" +
		"# >>> ganymede >>>\n" +
		"source-file -q \"/gone\"\n" +
		"set -g history-limit 50000\n" +
		"bind r source-file ~/.tmux.conf\n"

	after := config.WithBlock(mangled, []string{"source-file -q \"/frag\""})

	for _, want := range []string{"set -g mouse on", "set -g history-limit 50000", "bind r source-file"} {
		if !strings.Contains(after, want) {
			t.Errorf("the repair destroyed %q:\n%s", want, after)
		}
	}

	// And the repair itself must be well-formed, or the next call would add a
	// second block on top of it.
	repaired := after
	again := config.WithBlock(repaired, []string{"source-file -q \"/frag\""})
	if repaired != again {
		t.Errorf("the repaired block was not stable:\n--- repaired ---\n%s\n--- again ---\n%s", repaired, again)
	}
}
