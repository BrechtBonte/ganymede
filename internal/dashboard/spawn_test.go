package dashboard_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/dashboard"
	"github.com/BrechtBonte/ganymede/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// spawns records every Worktree session the Dashboard asked to have started,
// standing in for the harness turning that into a tmux window.
type spawns struct {
	dirs, names, prompts []string
	err                  error
}

func (s *spawns) Spawn(dir, name, prompt string) error {
	s.dirs = append(s.dirs, dir)
	s.names = append(s.names, name)
	s.prompts = append(s.prompts, prompt)
	return s.err
}

// onARepo is a Dashboard with one repo on the rail and nothing running in it,
// cursor already on its header row — which is where w is pressed from.
func onARepo(t *testing.T, harness dashboard.Harness, root string) tea.Model {
	t.Helper()
	state := remembering(t)
	worked(t, state, root, time.Now())
	harness.Activity = state
	return dashboardOn(harness)
}

// w on a repo's header row is the one-action flow: it opens the dialog
// rather than doing anything to tmux itself.
func TestWOnARepoHeaderOpensTheSpawnDialog(t *testing.T) {
	model := onARepo(t, dashboard.Harness{}, "/repos/service-billing")

	model = types(model, "w")

	box := detail(model)
	if !strings.Contains(box, "ticket") || !strings.Contains(box, "suffix") || !strings.Contains(box, "prompt") {
		t.Errorf("SELECTED = %q, want the spawn dialog's fields", box)
	}
}

// w only means something on a repo's own row. A Session row already has t,
// o and q on it, and none of them is a worktree spawn.
func TestWOnASessionRowDoesNothing(t *testing.T) {
	spawner := &spawns{}
	var model tea.Model = dashboard.New(nil, dashboard.Harness{Jumper: &jumps{}, Spawner: spawner})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 45})
	model, _ = model.Update(dashboard.Sessions{live("max-paging-numbers", "/repos/service-billing", session.Working)})
	model = press(model, tea.KeyDown)

	model = types(model, "w")

	if len(spawner.names) != 0 {
		t.Errorf("Spawn %v, want nothing spawned from a Session row", spawner.names)
	}
	if box := detail(model); strings.Contains(box, "ticket ›") {
		t.Errorf("SELECTED = %q, want no spawn dialog over a Session row", box)
	}
}

// The whole flow in one keystroke each: a ticket, a suffix, and Enter.
func TestSpawningWithATicketAndSuffixNamesTheWorktree(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "fire-2841")
	model = press(model, tea.KeyTab)
	model = types(model, "paging")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841-paging" {
		t.Errorf("Spawn named %v, want [FIRE-2841-paging]", spawner.names)
	}
	if spawner.dirs[0] != "/repos/service-billing" {
		t.Errorf("Spawn ran in %v, want the repo the dialog was opened over", spawner.dirs)
	}
}

// No ticket means the field typed into is the whole of the worktree's name —
// "the user just names the worktree" (§6).
func TestSpawningWithNoTicketJustNamesTheWorktree(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = press(model, tea.KeyTab)
	model = types(model, "quick fix")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "quick-fix" {
		t.Errorf("Spawn named %v, want [quick-fix]", spawner.names)
	}
}

// A ticket with no suffix at all is still a name worth spawning — the suffix
// is editable, not required.
func TestSpawningWithATicketAndNoSuffixNamesItAfterTheTicketAlone(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841" {
		t.Errorf("Spawn named %v, want [FIRE-2841]", spawner.names)
	}
}

// A first prompt is what makes the spawn fire-and-forget: filled, the
// session starts Working immediately instead of waiting at its prompt.
func TestSpawningWithAFirstPromptCarriesIt(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyTab)
	model = types(model, "paging")
	model = press(model, tea.KeyTab)
	model = types(model, "fix the pagination bug")
	model = press(model, tea.KeyEnter)

	if len(spawner.prompts) != 1 || spawner.prompts[0] != "fix the pagination bug" {
		t.Errorf("Spawn asked for prompt %v, want the first prompt", spawner.prompts)
	}
}

// Shift+Tab walks the fields backwards, landing back on ticket from prompt.
func TestShiftTabWalksTheFieldsBackwards(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = press(model, tea.KeyTab)
	model = press(model, tea.KeyTab)
	model = press(model, tea.KeyShiftTab)
	model = press(model, tea.KeyShiftTab)
	// Back on ticket: whatever is typed now belongs to it.
	model = types(model, "CORE-119")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "CORE-119" {
		t.Errorf("Spawn named %v, want [CORE-119], want Shift+Tab to land back on ticket", spawner.names)
	}
}

// A ticket typed in lower case is still a ticket — the harness upper-cases it
// the same way setting one by hand does.
func TestSpawningUppercasesTheTicketField(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "fire-2841")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841" {
		t.Errorf("Spawn named %v, want [FIRE-2841]", spawner.names)
	}
}

// A ticket pasted as a title rather than typed as a key can carry a space or
// stray punctuation — exactly the text a git branch name cannot hold as-is,
// so it is slugged the same way the suffix already is.
func TestSpawningSlugifiesTheTicketFieldToo(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE 2841")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841" {
		t.Errorf("Spawn named %v, want [FIRE-2841]", spawner.names)
	}
}

// Nothing typed at all is nothing to spawn: the harness says so and leaves
// the dialog open to be corrected, the same bargain an empty ticket strikes.
func TestSpawningWithNoNameAtAllIsRefused(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 0 {
		t.Errorf("Spawn %v, want nothing spawned with no name typed", spawner.names)
	}
	box := detail(model)
	if !strings.Contains(box, "name the worktree") {
		t.Errorf("SELECTED = %q, want it to say why nothing happened", box)
	}
	if !strings.Contains(box, "ticket") {
		t.Errorf("SELECTED = %q, want the dialog left open to correct", box)
	}
}

// Escape abandons the dialog exactly like it abandons a ticket correction.
func TestEscapeCancelsSpawning(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEsc)

	if len(spawner.names) != 0 {
		t.Errorf("Spawn %v, want nothing spawned after Escape", spawner.names)
	}
	if box := detail(model); strings.Contains(box, "ticket ›") {
		t.Errorf("SELECTED = %q, want the dialog closed", box)
	}
}

// A Worktree session that could not be started is worth the same word as a
// jump that could not be made.
func TestSpawnThatFailsSaysSo(t *testing.T) {
	spawner := &spawns{err: errors.New("spawn worktree session FIRE-2841: tmux: no such session")}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if !strings.Contains(detail(model), "no such session") {
		t.Errorf("SELECTED = %q, want what went wrong in it", detail(model))
	}
}

// While the dialog is open every key belongs to it — o and q are letters the
// prompt field is entitled to as much as any other.
func TestTheKeysBelongToTheSpawnDialogWhileItIsOpen(t *testing.T) {
	spawner := &spawns{}
	opener := &opens{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner, Opener: opener}, "/repos/service-billing")

	model = types(model, "w")
	model = press(model, tea.KeyTab)
	model = press(model, tea.KeyTab)
	model = types(model, "open a browser tab")

	if len(opener.dirs) != 0 {
		t.Errorf("opened %v while typing into the spawn dialog", opener.dirs)
	}
	if box := detail(model); !strings.Contains(box, "open a browser tab") {
		t.Errorf("SELECTED = %q, want everything typed in the prompt field", box)
	}
}

// Backspace corrects a field the same way it corrects a ticket.
func TestSpawnFieldsCanBeCorrectedWhileTyped(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-28411")
	model = press(model, tea.KeyBackspace)
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841" {
		t.Errorf("Spawn named %v, want [FIRE-2841]", spawner.names)
	}
}

// The live name is what the dialog is for: seeing FIRE-2841-paging take
// shape as you type is what "editable" (§6) actually means.
func TestSpawnDialogPreviewsTheWorktreeName(t *testing.T) {
	spawner := &spawns{}
	model := onARepo(t, dashboard.Harness{Spawner: spawner}, "/repos/service-billing")

	model = types(model, "w")
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyTab)
	model = types(model, "paging")

	if box := detail(model); !strings.Contains(box, "FIRE-2841-paging") {
		t.Errorf("SELECTED = %q, want a live preview of the worktree name", box)
	}
}

// The picker offers the whole inventory, and Tab is its spawn-into: every
// printable key already belongs to the query, so the dialog needs a key of
// its own to reach a repo the picker is showing rather than the rail.
func TestTabInThePickerOpensTheSpawnDialogForTheHighlightedRepo(t *testing.T) {
	spawner := &spawns{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Spawner:   spawner,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model = press(model, tea.KeyTab)

	if box := detail(model); !strings.Contains(box, "ticket") {
		t.Errorf("SELECTED = %q, want the spawn dialog for the highlighted repo", box)
	}
}

// Spawning into a repo from the picker does both halves of what picking one
// does: it starts the Worktree session, and it puts the repo on the rail —
// there may be nothing else running in it at all.
func TestSpawningIntoARepoFromThePickerPutsItOnTheRail(t *testing.T) {
	spawner := &spawns{}
	state := remembering(t)
	model := picking(t, dashboardOn(dashboard.Harness{
		Spawner:   spawner,
		Activity:  state,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))

	model = press(model, tea.KeyTab)
	model = types(model, "FIRE-2841")
	model = press(model, tea.KeyEnter)

	if len(spawner.names) != 1 || spawner.names[0] != "FIRE-2841" || spawner.dirs[0] != "/repos/ganymede" {
		t.Errorf("Spawn ran %v in %v, want [FIRE-2841] in /repos/ganymede", spawner.names, spawner.dirs)
	}
	if _, known := state.Active()["/repos/ganymede"]; !known {
		t.Errorf("the repo spawned into from the picker was not recorded as worked in: %v", state.Active())
	}
	if _, ok := lineWith(tree(model), "ganymede"); !ok {
		t.Errorf("the repo spawned into from the picker is not on the Dashboard:\n%s", tree(model))
	}
}

// A Tab with nothing highlighted — an empty inventory, or a query reaching
// nothing — has no repo to spawn into, and must not leave the picker either.
func TestTabInThePickerWithNothingHighlightedDoesNothing(t *testing.T) {
	spawner := &spawns{}
	model := picking(t, dashboardOn(dashboard.Harness{
		Spawner:   spawner,
		Inventory: stock{repos: []string{"/repos/ganymede"}},
	}))
	model = typing(model, "zzzz")

	model = press(model, tea.KeyTab)

	if len(spawner.names) != 0 {
		t.Errorf("Spawn %v, want nothing spawned with no repo highlighted", spawner.names)
	}
	if !strings.Contains(drawn(model), "GO TO REPO") {
		t.Errorf("the picker closed on a Tab with nothing highlighted:\n%s", drawn(model))
	}
}
