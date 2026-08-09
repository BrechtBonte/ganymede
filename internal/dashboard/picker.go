package dashboard

import (
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// picker is the fuzzy repo picker: the way to every repo the Dashboard is
// deliberately not showing.
//
// The Dashboard is short because it only shows the working set. That is only
// bearable if everything it leaves out is one keystroke away, which is what
// this is — the whole discovered inventory, narrowed by whatever you type,
// and taking you to what you chose.
type picker struct {
	// open says the picker has the sidepanel.
	open bool
	// query is what you have typed.
	query string
	// repos is the inventory the last scan came back with, kept while the
	// picker is closed so that opening it again is instant.
	repos []string
	// scanned says a scan has come back at all, which is the difference
	// between an inventory with nothing in it and one not read yet.
	scanned bool
	// failed is why the inventory could not be read.
	failed string
	// matches is repos narrowed to query, the best answer first.
	matches []string
	cursor  int
}

// discovered is what a scan of the inventory came back with.
type discovered struct {
	repos []string
	err   error
}

// scanning reads the inventory off the main loop. Discovery walks somebody's
// whole projects directory, and a picker that opened only once the filesystem
// had been read would be a picker that stutters every time.
func scanning(in Inventory) tea.Cmd {
	return func() tea.Msg {
		repos, err := in.Repos()
		return discovered{repos: repos, err: err}
	}
}

// opening puts the picker up.
//
// It offers whatever the last scan found straight away and asks for a fresh
// one behind it: repos are cloned and removed while the harness is up, so an
// inventory read once at startup would quietly go stale — and an inventory
// nothing has read yet is a picker that says it is still looking.
func (m Model) opening() (tea.Model, tea.Cmd) {
	if m.harness.Inventory == nil {
		return m, nil
	}
	m.picker = m.picker.opened()
	return m, scanning(m.harness.Inventory)
}

// picking is the picker's own key handling. While it is up every key belongs
// to it: the keys it needs are the printable ones.
func (m Model) picking(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.picker = m.picker.closed()
	case tea.KeyEnter:
		chosen := m.picker.chosen()
		m.picker = m.picker.closed()
		if chosen == "" {
			return m, nil
		}
		return m.open(chosen), nil
	case tea.KeyUp:
		if m.picker.cursor > 0 {
			m.picker.cursor--
		}
	case tea.KeyDown:
		if m.picker.cursor+1 < len(m.picker.matches) {
			m.picker.cursor++
		}
	case tea.KeyBackspace:
		m.picker = m.picker.asking(trimRune(m.picker.query))
	case tea.KeySpace:
		m.picker = m.picker.asking(m.picker.query + " ")
	case tea.KeyRunes:
		m.picker = m.picker.asking(m.picker.query + string(msg.Runes))
	}
	return m, nil
}

// opened puts the picker up over whatever the last scan found.
func (p picker) opened() picker {
	p.open, p.query, p.cursor = true, "", 0
	p.matches = ranked(p.repos, "")
	return p
}

// closed puts the picker away, keeping the inventory it was showing.
func (p picker) closed() picker {
	p.open, p.query, p.matches, p.cursor = false, "", nil, 0
	return p
}

// asking narrows the inventory to a new query, from the top: the best answer
// to what you have typed now is not where the cursor was on what you typed
// before.
func (p picker) asking(query string) picker {
	p.query, p.cursor = query, 0
	p.matches = ranked(p.repos, query)
	return p
}

// found takes in a scan. It arrives whether or not the picker is still up —
// keeping it either way is what makes the next opening instant.
func (p picker) found(msg discovered) picker {
	p.scanned = true
	if msg.err != nil {
		p.failed = msg.err.Error()
		return p
	}
	p.failed, p.repos = "", msg.repos
	p.matches = ranked(p.repos, p.query)
	p.cursor = min(p.cursor, max(0, len(p.matches)-1))
	return p
}

// chosen is the repo under the cursor, and empty when there is nothing to
// choose.
func (p picker) chosen() string {
	if p.cursor >= len(p.matches) {
		return ""
	}
	return p.matches[p.cursor]
}

// view is the picker filling the sidepanel: what you have typed, and the
// repos it reaches.
func (m Model) pickerView() string {
	rule := ruleStyle.Render(strings.Repeat("─", m.width))
	lines := []string{
		m.header(),
		rule,
		titleStyle.Render(truncate("GO TO REPO", m.width)),
		quietStyle.Render("› ") + truncate(m.picker.query, m.width-2),
		rule,
	}
	lines = append(lines, m.picker.offered(m.width, m.height-len(lines))...)
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// offered is the repos the query reaches, or why there are none.
func (p picker) offered(width, space int) []string {
	switch {
	case p.failed != "":
		// The picker is the only way to the repos the Dashboard is not
		// showing, so an inventory that could not be read is worth the whole
		// panel rather than one truncated line: an empty picker looks exactly
		// like a machine with no repos on it.
		return clip(wrap(p.failed, width), space)
	case !p.scanned:
		return clip([]string{quietStyle.Render(truncate("Scanning…", width))}, space)
	case len(p.matches) == 0:
		return clip([]string{quietStyle.Render(truncate("No repo matches.", width))}, space)
	}

	first := 0
	if len(p.matches) > space {
		// Centre what is on show on the selection.
		first = min(max(0, p.cursor-space/2), len(p.matches)-space)
	}
	lines := make([]string, 0, space)
	for i := first; i < len(p.matches) && len(lines) < space; i++ {
		lines = append(lines, p.row(i, width))
	}
	return lines
}

// row draws one repo: its own name at the left, where it is filed at the
// right. The name is what you typed at; the directory above it is what tells
// two repos of the same name apart, which is the only reason it is there.
func (p picker) row(i, width int) string {
	name, filed := labelOf(p.matches[i])
	// A name too long for the panel says so: the repos with the longest names
	// are the ones whose names differ only at the end.
	name = elide(name, width-lipgloss.Width(filed)-1)
	if i == p.cursor {
		return selectedStyle.Width(width).Render(spread(name, filed, width))
	}
	return spread(name, quietStyle.Render(filed), width)
}

// labelOf is how a repo reads in the picker.
func labelOf(repo string) (name, filed string) {
	above := filepath.Dir(repo)
	if above == repo || above == string(filepath.Separator) || above == "." {
		return filepath.Base(repo), ""
	}
	return filepath.Base(repo), filepath.Base(above)
}

// offer is one repo the picker can offer, and how well it answers the query.
type offer struct {
	repo string
	// named says the query was found in the repo's own name rather than only
	// somewhere up the path it is filed under.
	named bool
	score int
}

// ranked narrows repos to the ones query reaches, best answer first.
//
// A repo whose own name matches is what you meant. One that matches only
// somewhere up its path is a fallback worth offering — you may well know a
// repo by the organisation it belongs to — and never worth offering first.
func ranked(repos []string, query string) []string {
	offers := make([]offer, 0, len(repos))
	for _, repo := range repos {
		if score, ok := matched(query, filepath.Base(repo)); ok {
			offers = append(offers, offer{repo: repo, named: true, score: score})
			continue
		}
		if score, ok := matched(query, repo); ok {
			offers = append(offers, offer{repo: repo, score: score})
		}
	}
	slices.SortFunc(offers, func(a, b offer) int {
		if a.named != b.named {
			if a.named {
				return -1
			}
			return 1
		}
		if d := a.score - b.score; d != 0 {
			return d
		}
		return strings.Compare(a.repo, b.repo)
	})

	matches := make([]string, len(offers))
	for i, o := range offers {
		matches[i] = o.repo
	}
	return matches
}

// matched reports whether query appears in candidate as a subsequence,
// ignoring case, and how loosely — a lower score is a closer match.
//
// Subsequence rather than substring is the whole point: "gnm" should reach
// ganymede, because the letters you remember of a repo's name are rarely the
// consecutive ones. The score is how far the match is spread out, counting
// from the start of the candidate, so that a name the query is a prefix of
// scores nothing and beats every looser reading of it.
//
// The walk is greedy — it takes the first letter that will do rather than
// searching for the tightest arrangement. That is predictable, which matters
// more here than optimal: the order the picker offers repos in has to be one
// you can learn.
func matched(query, candidate string) (int, bool) {
	if query == "" {
		return 0, true
	}
	wanted := []rune(strings.ToLower(query))

	at, last, score := 0, -1, 0
	for i, r := range []rune(strings.ToLower(candidate)) {
		if r != wanted[at] {
			continue
		}
		if last < 0 {
			score += i
		} else {
			score += i - last - 1
		}
		last, at = i, at+1
		if at == len(wanted) {
			return score, true
		}
	}
	return 0, false
}

// trimRune drops the last character of s, which is what Backspace means when
// what you are editing may hold any character a directory can be named with.
func trimRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	return string(runes[:len(runes)-1])
}

// wrap breaks a line across as many sidepanel-width lines as it takes.
func wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		if ansi.StringWidth(line) > width {
			// A path with no spaces in it is one word and wider than the
			// sidepanel, and the tail of it is the part that says where.
			broken := split(line, width)
			lines, line = append(lines, broken[:len(broken)-1]...), broken[len(broken)-1]
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// split breaks one word too wide for the panel across lines.
func split(word string, width int) []string {
	var lines []string
	line := ""
	for _, r := range word {
		if line != "" && ansi.StringWidth(line)+ansi.StringWidth(string(r)) > width {
			lines, line = append(lines, line), ""
		}
		line += string(r)
	}
	return append(lines, line)
}
