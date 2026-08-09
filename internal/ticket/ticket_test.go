package ticket_test

import (
	"testing"

	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// The whole of the derivation: a branch named after the work carries the
// ticket, and the harness reads it off rather than asking anybody.
func TestTicketIsTheFirstKeyNamedInTheText(t *testing.T) {
	for _, c := range []struct {
		text string
		want ticket.Key
	}{
		{"FIRE-2841-paging", "FIRE-2841"},
		{"feat/FIRE-2841-max-paging-numbers", "FIRE-2841"},
		{"fix/CORE-119", "CORE-119"},
		// A project key can carry digits of its own, and the number is
		// whatever follows the last hyphen before it.
		{"S2-4-spike", "S2-4"},
		// Two keys in one name: the first is the one the work is about.
		{"FIRE-2841-reverts-FIRE-2790", "FIRE-2841"},
	} {
		got, ok := ticket.In(c.text)
		if !ok || got != c.want {
			t.Errorf("In(%q) = %q, %v; want %q, true", c.text, got, ok, c.want)
		}
	}
}

// A name with no ticket in it must come back with nothing, so that the row can
// say "no ticket" rather than show a key nobody chose.
func TestTextWithNoKeyInItNamesNoTicket(t *testing.T) {
	for _, text := range []string{
		"",
		"main",
		"feat/attention-strip-and-detail",
		// JIRA keys are upper case, and reading lower case as one would turn
		// every dependency bump — renovate/lodash-4.17.21 — into a ticket.
		"fix/fire-2841-paging",
		// A key is a project and a number, not either on its own.
		"FIRE-",
		"-2841",
	} {
		if got, ok := ticket.In(text); ok {
			t.Errorf("In(%q) = %q, true; want no ticket", text, got)
		}
	}
}

// The link is the other half of what the harness knows about a ticket, and it
// is built rather than looked up: there is no JIRA API here, ever.
func TestTicketLinksToItsBrowsePage(t *testing.T) {
	if got, want := ticket.Key("FIRE-2841").URL(), "https://teamleader.atlassian.net/browse/FIRE-2841"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}
