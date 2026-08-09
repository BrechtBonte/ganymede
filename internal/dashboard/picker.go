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

// Discovered is what a scan of the inventory came back with. A scan can fail
// and still have found repos — one unreadable scan root among several — so
// both fields matter whichever way it went.
type Discovered struct {
	Repos []string
	Err   error
}

// scanning reads the inventory off the main loop. Discovery walks somebody's
// whole projects directory, and a picker that opened only once the filesystem
// had been read would be a picker that stutters every time.
func scanning(in Inventory) tea.Cmd {
	return func() tea.Msg {
		repos, err := in.Repos()
		return Discovered{Repos: repos, Err: err}
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
		// A key that does nothing at all reads as a Dashboard that has hung.
		// Whatever went wrong was said on stderr, which is behind the TUI.
		m.notice = "no repo discovery is configured"
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
		return m.goTo(chosen), nil
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
//
// A scan that failed still hands over whatever it did find: one scan root on a
// mount that has gone away is no reason to lose the repos under the others.
// Only a failure that found nothing at all leaves the last inventory standing,
// since an empty picker looks exactly like a machine with no repos on it.
//
// The cursor keeps the repo it was on rather than the row it was on. A scan
// lands on its own schedule, and a repo cloned since the last one can sort
// above the line you were reading — Enter has to open the repo you were
// looking at, not whatever moved into its place.
func (p picker) found(msg Discovered) picker {
	under := p.chosen()
	p.scanned = true
	p.failed = ""
	if msg.Err != nil {
		p.failed = msg.Err.Error()
	}
	if msg.Repos != nil || msg.Err == nil {
		p.repos = msg.Repos
	}

	p.matches = ranked(p.repos, p.query)
	p.cursor = 0
	for i, repo := range p.matches {
		if repo == under {
			p.cursor = i
			break
		}
	}
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
	// A sidepanel with no room for the matches gives up the matches, not the
	// whole Dashboard: the panel can be dragged to any height at all, and a
	// negative amount of room is not something to hand to a slice.
	lines = append(lines, m.picker.offered(m.width, max(0, m.height-len(lines)))...)
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// offered is the repos the query reaches, or why there are none.
//
// Repos come first, whatever the last scan had to say. A scan that failed with
// an inventory already in hand is a transient thing — a mount that went away,
// a permissions blip — and hiding a perfectly good picker behind it would cost
// you the repos for as long as the harness is up.
func (p picker) offered(width, space int) []string {
	switch {
	case len(p.matches) > 0:
	case p.failed != "":
		// The picker is the only way to the repos the Dashboard is not
		// showing, so an inventory that could not be read is worth the whole
		// panel rather than one truncated line: an empty picker looks exactly
		// like a machine with no repos on it.
		return clip(wrap(p.failed, width), space)
	case !p.scanned:
		return clip([]string{quietStyle.Render(truncate("Scanning…", width))}, space)
	default:
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
	// The name gets the panel first. It is what you typed at and what tells
	// the rows apart; the directory is there to break a tie, and a long one
	// must not be allowed to take every column and elide the name to nothing.
	filed = truncate(filed, width/3)
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
// A repo whose own name matches is what you meant. One that matches by the
// directory it is filed under is a fallback worth offering — you may well know
// a repo by the organisation it belongs to — and never worth offering first.
//
// The fallback is the directory, not the whole path. Every repo under the scan
// roots shares the path to them, so matching against the absolute path would
// let any query whose letters turn up in "/Users/somebody/Projects" reach the
// entire inventory — which is the narrowing the picker exists for, gone. What
// can be matched is what the row shows.
func ranked(repos []string, query string) []string {
	offers := make([]offer, 0, len(repos))
	for _, repo := range repos {
		name, filed := labelOf(repo)
		if score, ok := matched(query, name); ok {
			offers = append(offers, offer{repo: repo, named: true, score: score})
			continue
		}
		if score, ok := matched(query, filed+"/"+name); ok {
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
