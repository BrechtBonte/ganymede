package dashboard

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Spawner starts a background Worktree session for a repo: a new tmux window
// in that repo's Session running claude --worktree, named after the worktree
// the dialog derived and given the first prompt as its own when there is one
// (§6).
//
// Starting one is two questions, not one, because tmux answers the first
// immediately and the one that matters late: a window is accepted at once, and
// the session in it can still die ten seconds later over a WorktreeCreate hook
// or a flag claude will not take.
type Spawner interface {
	// Spawn starts the session and returns the window it opened.
	Spawn(dir, name, prompt string) (window string, err error)
	// SpawnWatch waits out that window's startup and reports the session
	// dying in it, along with the last thing it said. It blocks for as long as
	// being sure takes, so it is asked away from the goroutine that draws.
	SpawnWatch(window string) (output string, died bool)
}

// spawned is a Worktree session that did not survive its own startup, and what
// it said on the way out.
type spawned struct{ name, output string }

// said is what the rail is told about it. A window that had already gone
// leaves nothing to read, and the death itself is still worth the word — it is
// the difference between a spawn that quietly did nothing and one that said so.
func (s spawned) said() string {
	if s.output == "" {
		return s.name + " died on startup"
	}
	return s.name + " died on startup: " + s.output
}

// spawnField is which of the dialog's three inputs the keyboard is in.
type spawnField int

const (
	fieldTicket spawnField = iota
	fieldSuffix
	fieldPrompt
)

// spawning is the worktree-spawn dialog open over a repo: what has been
// typed into each of its fields, and which one the keyboard is in.
//
// It holds the repo by its root and label rather than the row it was opened
// over, for the same reason the ticket-setting input holds a checkout instead
// of a row: the working set is rebuilt out from under this dialog every time
// any Session anywhere changes state, and the repo need not even be on the
// rail yet — spawning into one from the picker is what puts it there.
type spawning struct {
	root, label            string
	field                  spawnField
	ticket, suffix, prompt string
	// claimWarning is the mention this dialog carries when the repo has a
	// Claim on it (§4.2: "warns agent spawns away") — empty on the ordinary
	// Free or InUse root.
	claimWarning string
}

// name is the worktree name the fields ask for (§6): the ticket and the
// suffix run together, or the suffix alone once there is no ticket to run it
// after — which is what makes typing a suffix alone "the user just names the
// worktree".
func (s *spawning) name() string {
	ticket := strings.ToUpper(slug(s.ticket))
	suffix := slug(s.suffix)
	switch {
	case ticket == "":
		return suffix
	case suffix == "":
		return ticket
	default:
		return ticket + "-" + suffix
	}
}

// slug is text written the way a git branch and a tmux window can both carry
// it: lower case, and every run of whitespace or punctuation a single dash.
// The ticket field is upper-cased on top of this rather than exempted from
// it — JIRA writes keys in upper case, but a ticket pasted as a title rather
// than typed as a key is exactly the text a git branch name cannot carry
// as-is.
func slug(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// spawnFields is how many inputs the dialog has, which is what next and prev
// wrap around.
const spawnFields = 3

// next moves the keyboard to the field after this one, wrapping from prompt
// back to ticket.
func (s *spawning) next() *spawning {
	n := *s
	n.field = (n.field + 1) % spawnFields
	return &n
}

// prev moves the keyboard to the field before this one, wrapping from ticket
// back to prompt.
func (s *spawning) prev() *spawning {
	n := *s
	n.field = (n.field + spawnFields - 1) % spawnFields
	return &n
}

// edit rewrites whichever field the keyboard is in.
func (s *spawning) edit(f func(string) string) *spawning {
	n := *s
	switch n.field {
	case fieldTicket:
		n.ticket = f(n.ticket)
	case fieldSuffix:
		n.suffix = f(n.suffix)
	case fieldPrompt:
		n.prompt = f(n.prompt)
	}
	return &n
}

// with appends text to whichever field the keyboard is in.
func (s *spawning) with(text string) *spawning {
	return s.edit(func(v string) string { return v + text })
}

// back drops the last character of whichever field the keyboard is in.
func (s *spawning) back() *spawning {
	return s.edit(trimRune)
}

// spawn opens the dialog over the selected repo. It is w's whole job on a
// repo's own row; a Session row has no repo of its own to spawn into, so w
// does nothing there — the same asymmetry t and o already keep the other way
// round.
func (m Model) spawn() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	r := m.rows[m.cursor]
	if r.session != nil {
		return m
	}
	m.spawning = &spawning{root: r.root, label: r.repoName(), claimWarning: m.claimedWarning(r.root)}
	return m
}

// spawnInto opens the same dialog over a repo picked out of the picker —
// closing the picker first, since the dialog and the picker both want the
// sidepanel. The repo may never have been opened at all, which is exactly
// what spawning into it from here is for.
func (m Model) spawnInto(dir string) Model {
	m.picker = m.picker.closed()
	m.spawning = &spawning{root: dir, label: filepath.Base(dir), claimWarning: m.claimedWarning(dir)}
	return m
}

// spawningKey is the dialog's own key handling. While it is up every key
// belongs to it, the same way the ticket-setting input owns the keyboard.
func (m Model) spawningKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Abandoned: nothing is spawned, and the repo is left as it was.
		m.spawning = nil
	case tea.KeyEnter:
		return m.launch()
	case tea.KeyTab:
		m.spawning = m.spawning.next()
	case tea.KeyShiftTab:
		m.spawning = m.spawning.prev()
	case tea.KeyRunes:
		m.spawning = m.spawning.with(string(msg.Runes))
	case tea.KeySpace:
		m.spawning = m.spawning.with(" ")
	case tea.KeyBackspace:
		m.spawning = m.spawning.back()
	}
	return m, nil
}

// launch starts the Worktree session the dialog asked for.
//
// A name that cannot be worked out from what was typed is left open to be
// corrected, the same bargain an empty ticket strikes — there is something to
// type, not something to type again. A Spawn that runs and fails is worth the
// same word a jump that could not be made gets, and there the dialog closes:
// retyping the same fields would ask tmux for exactly the same thing.
//
// A Spawn that runs and works is not the end of it. It leaves behind the watch
// on the window it opened, which is the only thing that can tell a Worktree
// session that started from one that was merely begun.
func (m Model) launch() (Model, tea.Cmd) {
	name := m.spawning.name()
	if name == "" {
		m.notice = "name the worktree"
		return m, nil
	}
	root, prompt := m.spawning.root, strings.TrimSpace(m.spawning.prompt)
	if m.harness.Spawner == nil {
		m.notice = "no worktree spawning is configured"
		m.spawning = nil
		return m, nil
	}
	window, err := m.harness.Spawner.Spawn(root, name, prompt)
	if err != nil {
		m.notice = err.Error()
		m.spawning = nil
		return m, nil
	}
	m.spawning = nil
	if m.harness.Activity != nil {
		if err := m.harness.Activity.Touch(root, time.Now()); err != nil {
			// The Session is already running — a Touch that could not be kept
			// costs only the working set's memory of it, the same trade goTo
			// makes. The spawned Session must still show up once the registry
			// notices it.
			m.notice = err.Error()
		}
	}
	return m.rebuilt().selecting(root), m.watchingSpawn(window, name)
}

// watchingSpawn waits out a spawned session's startup away from the goroutine
// that draws, and reports it if it dies there.
//
// A spawn that stays up reports nothing at all: the Session turning up on the
// rail is the whole account of it, and a notice for every good spawn would be
// noise on the one panel that has to stay worth reading.
func (m Model) watchingSpawn(window, name string) tea.Cmd {
	spawner := m.harness.Spawner
	if window == "" {
		// Nothing to watch: a Spawner that names no window is one that cannot be
		// asked about it afterwards.
		return nil
	}
	return func() tea.Msg {
		if output, died := spawner.SpawnWatch(window); died {
			return spawned{name: name, output: output}
		}
		return nil
	}
}

// fieldLine draws one of the dialog's inputs: its label, what has been typed
// into it, and the cursor when the keyboard is in it.
func fieldLine(label, typed string, focused bool, width int) string {
	if focused {
		return ticketColour.Render(tail(label+" › "+typed+"▌", width))
	}
	return quietStyle.Render(tail(label+" › "+typed, width))
}

// spawningView is the SELECTED box for as long as the dialog is open: the
// repo it is about, a live preview of the worktree name the fields add up to
// — which is what "editable" (§6) means in practice — and the three fields
// themselves.
func (m Model) spawningView() []string {
	s := m.spawning
	first := s.label
	if preview := s.name(); preview != "" {
		first = s.label + " → " + preview
	}
	lines := []string{elide(first, m.width)}
	if s.claimWarning != "" {
		lines = append(lines, claimedStyle.Render(truncate("⚑ "+s.claimWarning, m.width)))
	}
	return append(lines,
		fieldLine("ticket", s.ticket, s.field == fieldTicket, m.width),
		fieldLine("suffix", s.suffix, s.field == fieldSuffix, m.width),
		fieldLine("prompt", s.prompt, s.field == fieldPrompt, m.width),
		quietStyle.Render(shorten(s.root, m.width)),
		quietStyle.Render(truncate("tab next field · ⏎ spawn · esc cancel", m.width)),
	)
}
