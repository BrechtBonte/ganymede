package config

import "strings"

// beginMarker and endMarker delimit the harness's block inside a file it
// does not own, so a re-install can replace it in place rather than append a
// second copy.
const (
	beginMarker = "# >>> ganymede >>>"
	endMarker   = "# <<< ganymede <<<"
)

// WithBlock returns conf carrying exactly one harness block holding lines:
// the existing one rewritten where it stands if conf already has it,
// appended otherwise.
//
// It works a line at a time and never drops a line it cannot account for. A
// block whose end marker has gone missing is repaired by replacing the
// opening marker alone — everything below it is the user's, however it got
// there.
func WithBlock(conf string, lines []string) string {
	block := make([]string, 0, len(lines)+2)
	block = append(block, beginMarker)
	block = append(block, lines...)
	block = append(block, endMarker)

	existing := strings.Split(conf, "\n")
	trailingNewline := len(existing) > 0 && existing[len(existing)-1] == ""
	if trailingNewline {
		existing = existing[:len(existing)-1]
	}

	begin := indexOfLine(existing, beginMarker, 0)
	if begin < 0 {
		return joinLines(append(existing, block...))
	}

	end := indexOfLine(existing, endMarker, begin+1)
	if end < 0 {
		end = begin
	}
	kept := append(append(append([]string{}, existing[:begin]...), block...), existing[end+1:]...)
	return joinLines(kept)
}

// indexOfLine finds the first line at or after start whose content is
// marker, ignoring the whitespace an editor may have left around it.
func indexOfLine(lines []string, marker string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			return i
		}
	}
	return -1
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
