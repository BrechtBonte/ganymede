package claim_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/claim"
	"github.com/BrechtBonte/ganymede/internal/config"
)

// state is a harness state file in a directory of the test's own.
func state(t *testing.T) config.Sidecar {
	t.Helper()
	return config.Sidecar{Path: filepath.Join(t.TempDir(), "ganymede", "state.json")}
}

// loaded is the Claims the state file holds.
func loaded(t *testing.T, sidecar config.Sidecar) *claim.Claims {
	t.Helper()
	claims, err := claim.Load(sidecar)
	if err != nil {
		t.Fatalf("load the claims: %v", err)
	}
	return claims
}

// A root Claim outlives the run that made it, the same reason a ticket set by
// hand does: it is a decision about the root, not about a process that ends
// every evening.
func TestAClaimOutlivesTheRunThatMadeIt(t *testing.T) {
	sidecar := state(t)
	if err := loaded(t, sidecar).Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	note, claimed := loaded(t, sidecar).NoteOf("/repos/billing")
	if !claimed || note != "reviewing PR #4123" {
		t.Errorf("NoteOf(billing) = %q, %v; want %q, true", note, claimed, "reviewing PR #4123")
	}
}

// The note is optional: claiming with none is still a Claim.
func TestAClaimNeedsNoNote(t *testing.T) {
	sidecar := state(t)
	if err := loaded(t, sidecar).Claim("/repos/billing", ""); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	note, claimed := loaded(t, sidecar).NoteOf("/repos/billing")
	if !claimed || note != "" {
		t.Errorf("NoteOf(billing) = %q, %v; want %q, true", note, claimed, "")
	}
}

// A root nobody has claimed reports so, rather than an empty note standing in
// for "claimed with nothing said" — the two have to be told apart.
func TestNoteOfAnUnclaimedRootReportsNotClaimed(t *testing.T) {
	sidecar := state(t)

	if note, claimed := loaded(t, sidecar).NoteOf("/repos/billing"); claimed {
		t.Errorf("NoteOf(billing) = %q, true; want an unclaimed root to report false", note)
	}
}

// Releasing is undoing the Claim, and outlives the run the same way making it
// did.
func TestReleaseUndoesAClaim(t *testing.T) {
	sidecar := state(t)
	claims := loaded(t, sidecar)
	if err := claims.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if err := claims.Release("/repos/billing"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, claimed := loaded(t, sidecar).NoteOf("/repos/billing"); claimed {
		t.Error("NoteOf(billing) reports claimed after Release")
	}
}

// Releasing a root nobody has claimed costs nothing to ask for: it is not an
// error to already have what you were asking for.
func TestReleasingAnUnclaimedRootIsANoOp(t *testing.T) {
	sidecar := state(t)

	if err := loaded(t, sidecar).Release("/repos/billing"); err != nil {
		t.Errorf("Release: %v", err)
	}
}

// Claiming again over your own Claim corrects the note rather than stacking
// a second one — the same bargain a ticket correction strikes.
func TestClaimingAgainCorrectsTheNote(t *testing.T) {
	sidecar := state(t)
	claims := loaded(t, sidecar)
	if err := claims.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if err := claims.Claim("/repos/billing", "reviewing PR #4200"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if note, _ := loaded(t, sidecar).NoteOf("/repos/billing"); note != "reviewing PR #4200" {
		t.Errorf("NoteOf(billing) = %q, want the corrected note", note)
	}
}

// Claimed is every root reserved right now, by root, with the note it was
// claimed with — what the working set reads to keep a Claimed repo on the
// rail even with nothing running in it.
func TestClaimedListsEveryRootReservedNow(t *testing.T) {
	sidecar := state(t)
	claims := loaded(t, sidecar)
	if err := claims.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := claims.Claim("/repos/assistant", ""); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	got := claims.Claimed()
	want := map[string]string{"/repos/billing": "reviewing PR #4123", "/repos/assistant": ""}
	if len(got) != len(want) || got["/repos/billing"] != want["/repos/billing"] || got["/repos/assistant"] != want["/repos/assistant"] {
		t.Errorf("Claimed() = %v, want %v", got, want)
	}
}

// The map Claimed hands back is the caller's own: mutating it must never
// reach back into the Claims this harness state actually holds.
func TestClaimedReturnsACopy(t *testing.T) {
	sidecar := state(t)
	claims := loaded(t, sidecar)
	if err := claims.Claim("/repos/billing", "reviewing PR #4123"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	got := claims.Claimed()
	got["/repos/billing"] = "tampered"

	if note, _ := claims.NoteOf("/repos/billing"); note != "reviewing PR #4123" {
		t.Errorf("NoteOf(billing) = %q, want the original note untouched by the caller's copy", note)
	}
}

// A nil Claims — a harness with no state file configured — has nothing
// claimed and refuses nothing about being asked.
func TestNilClaimsHasNothingClaimed(t *testing.T) {
	var claims *claim.Claims

	if note, claimed := claims.NoteOf("/repos/billing"); claimed {
		t.Errorf("NoteOf(billing) = %q, true; want a nil Claims to report false", note)
	}
	if got := claims.Claimed(); got != nil {
		t.Errorf("Claimed() = %v, want nil", got)
	}
}

// A nil Claims refuses Claim rather than panicking — there is nowhere for it
// to keep one — and reports Release a no-op, the same failing-soft its read
// methods already promise.
func TestNilClaimsRefusesClaimAndNoOpsRelease(t *testing.T) {
	var claims *claim.Claims

	if err := claims.Claim("/repos/billing", "reviewing PR #4123"); err == nil {
		t.Error("Claim on a nil Claims reported success")
	}
	if err := claims.Release("/repos/billing"); err != nil {
		t.Errorf("Release on a nil Claims: %v, want no error", err)
	}
}

// Concurrent Claims and Releases against the same state must never race:
// nothing here has a lock of its own to protect it if Claims did not have
// one, and go test -race is what would catch it if it did not.
func TestClaimsAreSafeForConcurrentUse(t *testing.T) {
	sidecar := state(t)
	claims := loaded(t, sidecar)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		root := fmt.Sprintf("/repos/repo-%d", i)
		go func() {
			defer wg.Done()
			_ = claims.Claim(root, "note")
		}()
		go func() {
			defer wg.Done()
			_ = claims.Claimed()
		}()
	}
	wg.Wait()

	if len(claims.Claimed()) != 20 {
		t.Errorf("Claimed() has %d roots, want all 20 concurrent Claims to have landed", len(claims.Claimed()))
	}
}
