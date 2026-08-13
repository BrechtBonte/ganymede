package dashboard_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/ticket"
	"github.com/BrechtBonte/ganymede/internal/topology"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// known stands in for everything behind a ticket — the branches, the worktree
// names, the corrections kept in harness state — as the one thing the Dashboard
// sees of them.
type known struct {
	// of is what each Session's directory is about.
	of map[string]ticket.Key
	// set is what the Dashboard asked to have kept, by directory.
	set map[string]ticket.Key
	// opened is every ticket the Dashboard asked to have shown.
	opened []ticket.Key
	// err is what setting or opening runs into.
	err error
}

func (k *known) Of(dir, root string) ticket.Key { return k.of[dir] }

func (k *known) Set(dir, root string, key ticket.Key) error {
	if k.err != nil {
		return k.err
	}
	if k.set == nil {
		k.set = map[string]ticket.Key{}
	}
	if k.of == nil {
		k.of = map[string]ticket.Key{}
	}
	k.set[dir], k.of[dir] = key, key
	return nil
}

func (k *known) Open(key ticket.Key) error {
	k.opened = append(k.opened, key)
	return k.err
}

// knowing is a Dashboard sized for the sidepanel, showing sessions and knowing
// what each of them is about.
func knowing(tickets dashboard.Tickets, sessions ...session.Session) tea.Model {
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Tickets: tickets})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions(sessions))
	return model
}

// types sends text to the Dashboard a keystroke at a time, as a keyboard does.
func types(model tea.Model, text string) tea.Model {
	for _, r := range text {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

// The ticket is what tells two Sessions in one checkout apart at a glance, so
// it belongs on the row rather than only in the box under it — abbreviated
// there, since the project key is the same on every row of every repo
// (checkout_test.go).
func TestSessionRowCarriesItsTicket(t *testing.T) {
	model := knowing(
		&known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}},
		live("max-paging-numbers", "/repos/service-billing", session.Working),
	)

	if line := sessionRow(t, tree(model)); !strings.Contains(line, "F-2841") {
		t.Errorf("row = %q, want the ticket on it", line)
	}
}

// The box under the rail is where a row says what it had no room for, and the
// ticket comes with something you can do about it.
//
// Shell, on purpose: t and o apply to every Session row regardless of state,
// and Shell is the one state with no inline action hint of its own to
// compete with them for the line's fixed width (§7.1: Shell gets none).
func TestSelectedBoxShowsTheTicketAndOffersToOpenIt(t *testing.T) {
	model := knowing(
		&known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}},
		live("max-paging-numbers", "/repos/service-billing", session.Shell),
	)
	// Past the repo header, onto the Session.
	model = press(model, tea.KeyDown)

	box := detail(model)
	if !strings.Contains(box, "FIRE-2841") {
		t.Errorf("SELECTED = %q, want the ticket in it", box)
	}
	if !strings.Contains(box, "o open") {
		t.Errorf("SELECTED = %q, want it to offer to open the ticket", box)
	}
	if !strings.Contains(box, "t ticket") {
		t.Errorf("SELECTED = %q, want it to offer to set the ticket", box)
	}
}

// The other half of what the harness knows about a ticket: o hands it to the
// browser, which is where everything the harness deliberately does not know
// about that ticket lives.
func TestOpenShowsTheSelectedSessionsTicket(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}}
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))
	model = press(model, tea.KeyDown)

	model = types(model, "o")

	if len(about.opened) != 1 || about.opened[0] != "FIRE-2841" {
		t.Errorf("opened %v, want the selected Session's ticket", about.opened)
	}
}

// A row with no ticket has no link to open, and pressing o has to say so: a key
// that silently does nothing reads as a harness that has broken.
func TestOpenOnASessionWithNoTicketSaysThereIsNothingToOpen(t *testing.T) {
	about := &known{}
	model := knowing(about, live("ganymede-78", "/repos/ganymede", session.Idle))
	model = press(model, tea.KeyDown)

	model = types(model, "o")

	if len(about.opened) != 0 {
		t.Errorf("opened %v, want nothing opened for a Session with no ticket", about.opened)
	}
	if box := detail(model); !strings.Contains(box, "no ticket") {
		t.Errorf("SELECTED = %q, want it to say there is nothing to open", box)
	}
}

// A browser that would not open is worth the same word as a jump that could not
// be made: you asked for it, and it did not happen.
func TestOpenSaysWhenTheBrowserWouldNotOpen(t *testing.T) {
	about := &known{
		of:  map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"},
		err: errors.New("no browser installed"),
	}
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))
	model = press(model, tea.KeyDown)

	model = types(model, "o")

	if box := detail(model); !strings.Contains(box, "no browser installed") {
		t.Errorf("SELECTED = %q, want what went wrong in it", box)
	}
}

// What went wrong is a sentence, and the reason is at the end of it: a link the
// desktop would not open says so with the whole address in front of the reason.
// Cut off at the sidepanel's edge, every complaint of that shape reads the same.
func TestWhatWentWrongIsReadableInFortyColumns(t *testing.T) {
	about := &known{
		of:  map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"},
		err: errors.New(`open https://teamleader.atlassian.net/browse/FIRE-2841: exec: "open": executable file not found in $PATH`),
	}
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))
	model = press(model, tea.KeyDown)

	model = types(model, "o")

	if box := detail(model); !strings.Contains(box, "not found in $PATH") {
		t.Errorf("SELECTED = %q, want the end of the complaint in it too", box)
	}
	// Readable, and still inside the sidepanel: a complaint carrying an address
	// with no spaces in it has nowhere obvious to break.
	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}

// Nothing reports a branch being switched: you do it in a shell, and the
// Session sits at its prompt through the whole thing. So the question is asked
// again as the Dashboard ticks, rather than once and then believed all day.
func TestTicketIsAskedForAgainAsTheDashboardTicks(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}}
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))

	// The branch is switched under the harness, which says nothing about it.
	about.of["/repos/service-billing"] = "CORE-119"
	model, _ = model.Update(dashboard.Tick{})

	if line := sessionRow(t, tree(model)); !strings.Contains(line, "C-119") {
		t.Errorf("row = %q, want the ticket the checkout is about now", line)
	}
}

// And not asked for again on every redraw in between. Working a ticket out runs
// git, the tree is redrawn on every keystroke and every state change anywhere,
// and a sidepanel that shelled out per row per redraw would be a sidepanel you
// could feel.
func TestTicketIsNotWorkedOutAgainOnEveryRedraw(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}}
	counted := &counting{known: about}
	model := knowing(counted, live("max-paging-numbers", "/repos/service-billing", session.Working))

	asked := counted.asked
	for range 5 {
		model, _ = model.Update(dashboard.Sessions([]session.Session{
			live("max-paging-numbers", "/repos/service-billing", session.Working),
		}))
		model = press(model, tea.KeyDown)
		_ = model.View()
	}

	if counted.asked != asked {
		t.Errorf("asked %d times over five redraws, want the %d it started with", counted.asked, asked)
	}
}

// counting is a Tickets that says how often it was asked.
type counting struct {
	*known
	asked int
}

func (c *counting) Of(dir, root string) ticket.Key {
	c.asked++
	return c.known.Of(dir, root)
}

// working is a Session in a repo, selected, with about standing behind it.
func working(about *known) tea.Model {
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))
	// Past the repo header, onto the Session.
	return press(model, tea.KeyDown)
}

// The correction itself: the branch says nothing, or says the wrong thing, and
// you tell the harness what the Session is really about.
func TestTicketIsSetFromTheDetailBox(t *testing.T) {
	about := &known{}
	model := working(about)

	model = types(model, "t")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if got := about.set["/repos/service-billing"]; got != "FIRE-2841" {
		t.Errorf("set %q, want the ticket that was typed", got)
	}
	if line := sessionRow(t, tree(model)); !strings.Contains(line, "F-2841") {
		t.Errorf("row = %q, want the ticket on it without waiting for anything", line)
	}
}

// A ticket typed the way it is spoken is the ticket. JIRA writes keys in upper
// case and so does the harness, and refusing what you typed over the shift key
// would be the harness being right at your expense.
func TestTicketTypedInLowerCaseIsStillATicket(t *testing.T) {
	about := &known{}
	model := working(about)

	model = types(model, "t")
	model = types(model, "fire-2841")
	model = press(model, tea.KeyEnter)

	if got := about.set["/repos/service-billing"]; got != "FIRE-2841" {
		t.Errorf("set %q, want %q", got, ticket.Key("FIRE-2841"))
	}
}

// The other way a ticket arrives in your hand is as the address of the tab it
// is open in, so the key is taken out of whatever is pasted.
func TestTicketPastedAsALinkIsTakenOutOfIt(t *testing.T) {
	about := &known{}
	model := working(about)

	model = types(model, "t")
	model = types(model, "https://teamleader.atlassian.net/browse/FIRE-2841")
	model = press(model, tea.KeyEnter)

	if got := about.set["/repos/service-billing"]; got != "FIRE-2841" {
		t.Errorf("set %q, want the key out of the link", got)
	}
}

// Typing over a ticket you did not mean to touch has to be undoable without
// setting anything, which is what escape is for everywhere else. The input
// opens empty, so the ticket it would have replaced has to come back with it.
func TestSettingATicketCanBeAbandoned(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}}
	model := working(about)

	model = types(model, "t")
	model = types(model, "CORE-1")
	model = press(model, tea.KeyEsc)

	if len(about.set) != 0 {
		t.Errorf("set %v, want nothing set", about.set)
	}
	if box := detail(model); !strings.Contains(box, "FIRE-2841") || strings.Contains(box, "CORE-1") {
		t.Errorf("SELECTED = %q, want the ticket it had before", box)
	}
}

// Something that is not a ticket cannot be one, and the harness says so and
// leaves you where you were rather than throwing away what you typed.
func TestSettingSomethingThatIsNotATicketSaysSo(t *testing.T) {
	about := &known{}
	model := working(about)

	model = types(model, "t")
	model = types(model, "paging")
	model = press(model, tea.KeyEnter)

	if len(about.set) != 0 {
		t.Errorf("set %v, want nothing set", about.set)
	}
	box := detail(model)
	if !strings.Contains(box, "paging") {
		t.Errorf("SELECTED = %q, want what you typed still there to be corrected", box)
	}
	if !strings.Contains(box, "not a ticket") {
		t.Errorf("SELECTED = %q, want it to say why nothing happened", box)
	}
}

// Setting nothing clears the correction, which is the only way back to letting
// the branch speak for itself.
func TestSettingAnEmptyTicketClearsTheCorrection(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "CORE-119"}}
	model := working(about)

	model = types(model, "t")
	model = press(model, tea.KeyEnter)

	got, cleared := about.set["/repos/service-billing"]
	if !cleared || got != "" {
		t.Errorf("set %q, %v; want the correction cleared", got, cleared)
	}
}

// While the input is open it owns the keyboard. Every letter on the Dashboard
// is a key that does something, and a t that jumped or an o that opened a
// browser halfway through typing a ticket would be unusable.
func TestTheKeysBelongToTheInputWhileItIsOpen(t *testing.T) {
	about := &known{of: map[string]ticket.Key{"/repos/service-billing": "FIRE-2841"}}
	jumper := &jumps{}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: jumper, Tickets: about})
	model, _ = model.Update(tea.WindowSizeMsg{Width: topology.SidepanelWidth, Height: 45})
	model, _ = model.Update(dashboard.Sessions([]session.Session{
		live("max-paging-numbers", "/repos/service-billing", session.Working),
	}))
	model = press(model, tea.KeyDown)

	model = types(model, "t")
	// o opens a ticket, and it is one of the letters typed here — proof that
	// what you are typing does not leak through to the key it would
	// otherwise be.
	model = types(model, "CORE-119")

	if len(about.opened) != 0 {
		t.Errorf("opened %v while a ticket was being typed", about.opened)
	}
	if len(jumper.pids) != 0 {
		t.Errorf("jumped to %v while a ticket was being typed", jumper.pids)
	}
	if box := detail(model); !strings.Contains(box, "CORE-119") {
		t.Errorf("SELECTED = %q, want everything typed in the input", box)
	}
}

// The input is about a checkout, not about a row. A Session that ends while you
// are typing its ticket takes its row off the rail and moves the selection to
// whatever is left — and the correction you were making is about the branch,
// which is still there and still worth making.
func TestTheInputOutlivesTheRowItWasOpenedOver(t *testing.T) {
	about := &known{}
	ending := live("max-paging-numbers", "/repos/service-billing", session.Working)
	staying := live("ganymede-78", "/repos/ganymede", session.Idle)
	model := knowing(about, ending, staying)
	// Onto the Session that is about to end. Its row is labelled after its
	// checkout and its box names the repo, so what finds it is the one thing
	// on it that is its own: the state it is in.
	model = onto(t, model, string(session.Working))

	model = types(model, "t")
	model = types(model, "FIRE-2841")
	// The Session ends: its row goes, and the selection lands elsewhere.
	model, _ = model.Update(dashboard.Sessions([]session.Session{staying}))

	if box := detail(model); !strings.Contains(box, "FIRE-2841") {
		t.Errorf("SELECTED = %q, want the ticket still being typed", box)
	}
	model = press(model, tea.KeyEnter)
	if got := about.set["/repos/service-billing"]; got != "FIRE-2841" {
		t.Errorf("set %q on %v, want it set on the checkout the input was opened over",
			got, about.set)
	}
}

// Backspace is how a ticket typed wrong is corrected, since the input is where
// you go when something is already wrong.
func TestTicketCanBeCorrectedWhileItIsTyped(t *testing.T) {
	about := &known{}
	model := working(about)

	model = types(model, "t")
	model = types(model, "CORE-1199")
	model = press(model, tea.KeyBackspace)
	model = press(model, tea.KeyEnter)

	if got := about.set["/repos/service-billing"]; got != "CORE-119" {
		t.Errorf("set %q, want %q", got, ticket.Key("CORE-119"))
	}
}

// A repo header row is not a Session and is about no ticket, so the key that
// sets one has nothing to open an input over.
func TestSettingATicketOnARepoHeaderDoesNothing(t *testing.T) {
	about := &known{}
	model := knowing(about, live("max-paging-numbers", "/repos/service-billing", session.Working))

	model = types(model, "t")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if len(about.set) != 0 {
		t.Errorf("set %v, want nothing set from a repo header", about.set)
	}
}

// A correction that could not be kept is worth saying: it was going to outlive
// the Session, and it is not going to.
func TestTicketThatCouldNotBeKeptSaysSo(t *testing.T) {
	about := &known{err: errors.New("read state.json: unexpected end of JSON input")}
	model := working(about)

	model = types(model, "t")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if box := detail(model); !strings.Contains(box, "unexpected end of JSON") {
		t.Errorf("SELECTED = %q, want what went wrong in it", box)
	}
}

// The rail is 40 columns whatever is on it, and the ticket is one more thing
// asking for room on a row that already carries a name and an age.
func TestTicketsFitTheSidepanel(t *testing.T) {
	model := knowing(
		&known{of: map[string]ticket.Key{
			"/repos/teamleadercrm-monolith-and-then-some": "FIRE-2841",
		}},
		live("teamleadercrm-monolith-billing-b7", "/repos/teamleadercrm-monolith-and-then-some", session.Working),
		live("FIRE-2841-max-paging-numbers", "/repos/service-billing", session.Blocked),
	)

	for _, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > topology.SidepanelWidth {
			t.Errorf("line is %d columns, sidepanel is %d:\n%q", width, topology.SidepanelWidth, line)
		}
	}
}
