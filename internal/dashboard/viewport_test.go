package dashboard

import (
	"strconv"
	"testing"
)

// The tree's window is a budget in lines while the cursor counts rows. Every
// row draws exactly one line today, so View() cannot yet be shown a row of two
// and asked whether the selection stayed on the panel — which is the whole
// point of counting lines. This is the seam where that can be asked, and it is
// inside the package for exactly that reason: the arithmetic has an invariant
// the drawn panel has no way of expressing until the git caution line lands.
func TestTheWindowHoldsTheSelectedRowWhateverItsRowsDraw(t *testing.T) {
	for _, c := range []struct {
		what string
		// rows is how many lines each row of the tree draws.
		rows   []int
		cursor int
		space  int
	}{
		{"a tree that fits whole", []int{1, 1, 1}, 1, 10},
		{"a line a row, selected at the head", []int{1, 1, 1, 1, 1, 1, 1, 1}, 0, 3},
		{"a line a row, selected in the middle", []int{1, 1, 1, 1, 1, 1, 1, 1}, 4, 3},
		{"a line a row, selected at the foot", []int{1, 1, 1, 1, 1, 1, 1, 1}, 7, 3},
		{"a two-line row selected, with room for one of them", []int{1, 1, 1, 2, 1, 1}, 3, 2},
		{"a row taller than half the block", []int{1, 1, 1, 1, 1, 4, 1}, 5, 6},
		{"two lines a row throughout", []int{2, 2, 2, 2, 2}, 3, 5},
		{"a two-line row selected at the foot", []int{1, 1, 2}, 2, 3},
		{"a block with no room at all", []int{1, 2, 1}, 1, 0},
	} {
		drawn := blocks(c.rows)
		first, last := shown(drawn, c.cursor, c.space)

		if first > c.cursor || c.cursor >= last {
			t.Errorf("%s: the window holds rows [%d,%d), leaving the selected row %d off the panel", c.what, first, last, c.cursor)
			continue
		}
		// A window may overrun the block only by drawing the selection's own
		// row, which is cut to fit rather than dropped.
		if lines := linesIn(drawn[first:last]); lines > c.space && last-first > 1 {
			t.Errorf("%s: the window draws %d lines of rows [%d,%d) into a block of %d", c.what, lines, first, last, c.space)
		}
	}
}

// Half the block is the rows-above share, not their limit: a repo header drawing
// two lines cannot always take its half, and what the rows below then leave
// unspent goes back to the rows above. A window left a line short of the block is
// a line of the sidepanel drawn blank while a whole row was waiting for it — and
// on a tree of two-line headers, the row waiting is usually the header saying
// which repo the selected Session row is working in.
func TestTheWindowSpendsTheWholeBlockRatherThanStoppingAtHalfOfIt(t *testing.T) {
	for _, c := range []struct {
		what   string
		rows   []int
		cursor int
		space  int
	}{
		{"a repo per two-line header and a Session each", []int{2, 1, 2, 1, 2, 1, 2, 1}, 3, 4},
		{"the same tree with a taller block", []int{2, 1, 2, 1, 2, 1, 2, 1}, 5, 7},
		{"a line a row", []int{1, 1, 1, 1, 1, 1, 1, 1}, 4, 3},
		{"two lines a row throughout", []int{2, 2, 2, 2, 2}, 3, 5},
	} {
		drawn := blocks(c.rows)
		first, last := shown(drawn, c.cursor, c.space)

		spare := c.space - linesIn(drawn[first:last])
		if first > 0 && len(drawn[first-1]) <= spare {
			t.Errorf("%s: the window holds rows [%d,%d), leaving %d lines of a %d-line block unused while row %d would have fitted in them",
				c.what, first, last, spare, c.space, first-1)
		}
		if last < len(drawn) && len(drawn[last]) <= spare {
			t.Errorf("%s: the window holds rows [%d,%d), leaving %d lines of a %d-line block unused while row %d would have fitted in them",
				c.what, first, last, spare, c.space, last)
		}
	}
}

// A window on a tree that fits shows the whole of it, wherever the cursor is.
func TestAWindowOnATreeThatFitsShowsAllOfIt(t *testing.T) {
	drawn := blocks([]int{2, 1, 2, 1})
	for cursor := range len(drawn) {
		if first, last := shown(drawn, cursor, 6); first != 0 || last != len(drawn) {
			t.Errorf("with the cursor on row %d the window holds rows [%d,%d), want the whole tree", cursor, first, last)
		}
	}
}

// blocks is a tree whose rows draw the given numbers of lines.
func blocks(rows []int) [][]string {
	drawn := make([][]string, len(rows))
	for i, lines := range rows {
		for line := range lines {
			drawn[i] = append(drawn[i], "row "+strconv.Itoa(i)+" line "+strconv.Itoa(line))
		}
	}
	return drawn
}

// linesIn is how many lines a run of rows draws.
func linesIn(rows [][]string) int {
	lines := 0
	for _, row := range rows {
		lines += len(row)
	}
	return lines
}
