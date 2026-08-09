package sidecar_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/sidecar"
)

// file is a sidecar path inside a temporary directory the test owns.
func file(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}

// load reads the sidecar at path, failing the test if it cannot be read.
func load(t *testing.T, path string) *sidecar.State {
	t.Helper()
	state, err := sidecar.Load(path)
	if err != nil {
		t.Fatalf("Load(%q): %v", path, err)
	}
	return state
}

// save writes the sidecar, failing the test if it cannot be written.
func save(t *testing.T, state *sidecar.State) {
	t.Helper()
	if err := state.Save(); err != nil {
		t.Fatalf("Save(): %v", err)
	}
}

// The point of the sidecar: a repo you worked in is still on the Dashboard
// after the Dashboard has been restarted, which is the only way the recency
// window means anything at all.
func TestActivitySurvivesBeingReloaded(t *testing.T) {
	path := file(t)
	worked := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)

	state := load(t, path)
	state.Touch("/Users/brecht/Projects/ganymede", worked)
	save(t, state)

	if got := load(t, path).Active()["/Users/brecht/Projects/ganymede"]; !got.Equal(worked) {
		t.Errorf("Active()[ganymede] = %v, want %v", got, worked)
	}
}

// A sidecar that is not there yet is a harness that has not been used yet,
// which is the state every first run is in.
func TestSidecarThatIsNotThereLoadsEmpty(t *testing.T) {
	if got := load(t, file(t)).Active(); len(got) != 0 {
		t.Errorf("Active() = %v, want nothing recorded", got)
	}
}

// Activity is the last time you were in a repo, so an older stamp arriving
// late — a Session whose root was already touched by something newer — must
// not drag the repo back towards falling off.
func TestActivityKeepsTheLatestStamp(t *testing.T) {
	state := load(t, file(t))
	newest := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)

	state.Touch("/repo", newest)
	state.Touch("/repo", newest.Add(-time.Hour))

	if got := state.Active()["/repo"]; !got.Equal(newest) {
		t.Errorf("Active()[/repo] = %v, want the latest stamp %v", got, newest)
	}
}

// The working set is rebuilt every time any Session moves, and each rebuild
// stamps every repo with a Session in it. The window is measured in days, so
// seconds are not worth a write — and a write is what every other harness
// reading this file has to notice.
func TestStampsWithinTheSameMinuteAreNotWrittenAgain(t *testing.T) {
	path := file(t)
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)

	state := load(t, path)
	state.Touch("/repo", at)
	save(t, state)
	written, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	state.Touch("/repo", at.Add(30*time.Second))
	save(t, state)

	again, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(written.ModTime()) {
		t.Error("Save() rewrote the sidecar for a stamp inside the same minute")
	}
}

// A minute later is a real move, and has to land — otherwise a repo you are
// working in every day would still fall off seven days after the first time
// the harness heard of it.
func TestStampsANewMinuteAreWritten(t *testing.T) {
	path := file(t)
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)

	state := load(t, path)
	state.Touch("/repo", at)
	save(t, state)

	state.Touch("/repo", at.Add(time.Minute))
	save(t, state)

	if got, want := load(t, path).Active()["/repo"], at.Add(time.Minute); !got.Equal(want) {
		t.Errorf("Active()[/repo] = %v, want %v", got, want)
	}
}

// The sidecar is the harness's own file, and the harness will want more in it
// than this — root claims, ticket overrides. A reader that dropped what it did
// not recognise would make every later addition a migration.
func TestFieldsTheHarnessDoesNotReadYetAreKept(t *testing.T) {
	path := file(t)
	body := `{"repos":{"/repo":{"lastActive":"2026-08-09T14:30:00Z","claim":{"note":"reviewing #23"}}},"somethingElse":1}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	state := load(t, path)
	state.Touch("/repo", time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC))
	save(t, state)

	kept := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatalf("the sidecar is no longer JSON: %v", err)
	}
	if _, ok := kept["somethingElse"]; !ok {
		t.Errorf("Save() dropped a field it does not read: %s", raw)
	}
	repos, _ := kept["repos"].(map[string]any)
	entry, _ := repos["/repo"].(map[string]any)
	if _, ok := entry["claim"]; !ok {
		t.Errorf("Save() dropped a repo's claim: %s", raw)
	}
	if got, want := entry["lastActive"], "2026-08-09T15:00:00Z"; got != want {
		t.Errorf("lastActive = %v, want the new stamp %v", got, want)
	}
}

// A sidecar somebody has hand-edited into nonsense must not stop the harness
// opening — the Dashboard's whole job is elsewhere — but it is worth saying
// out loud, because nothing else can tell an empty harness from a broken one.
func TestSidecarThatCannotBeReadIsUsableAndReported(t *testing.T) {
	path := file(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := sidecar.Load(path)
	if err == nil {
		t.Error("Load() reported nothing wrong with a sidecar it could not read")
	}
	if state == nil {
		t.Fatal("Load() handed back no state to go on with")
	}
	if got := state.Active(); len(got) != 0 {
		t.Errorf("Active() = %v, want nothing recorded", got)
	}
}

// And it must not be written over. What is in this file is the harness's
// memory of decisions you made — root claims and their notes are specified to
// live here — so a file it cannot understand is yours to fix, not the
// harness's to replace with an empty one.
func TestSidecarThatCannotBeReadIsNotWrittenOver(t *testing.T) {
	path := file(t)
	body := []byte(`{"repos":{"/repo":{"claim":{"note":"reviewing #23"` + "\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	state, _ := sidecar.Load(path)

	state.Touch("/repo", time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC))
	if err := state.Save(); err == nil {
		t.Error("Save() reported success over a sidecar it could not read")
	}

	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(body) {
		t.Errorf("the sidecar was rewritten:\ngot:  %s\nwant: %s", kept, body)
	}
}

// A repos object the harness cannot read is the same problem as a file it
// cannot read, and gets the same answer.
func TestSidecarWithUnreadableReposIsNotWrittenOver(t *testing.T) {
	path := file(t)
	body := []byte(`{"repos":"not an object","claims":{"/repo":"mine"}}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := sidecar.Load(path)
	if err == nil {
		t.Error("Load() reported nothing wrong with repos it could not read")
	}

	state.Touch("/repo", time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC))
	if err := state.Save(); err == nil {
		t.Error("Save() reported success over repos it could not read")
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(body) {
		t.Errorf("the sidecar was rewritten:\ngot:  %s\nwant: %s", kept, body)
	}
}

// A sidecar that is simply not there is a harness that has not been used yet,
// which is what every first run looks like — no error, and nothing to fix.
func TestSidecarThatIsNotThereIsNotAFailure(t *testing.T) {
	if _, err := sidecar.Load(file(t)); err != nil {
		t.Errorf("Load() on a sidecar that is not there: %v", err)
	}
}

// The Dashboard calls Save every time any Session moves. Nothing recorded
// since the last one is nothing to write, and nothing to read either.
func TestSavingWithNothingRecordedSinceTheLastSaveTouchesNothing(t *testing.T) {
	path := file(t)
	state := load(t, path)
	state.Touch("/repo", time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC))
	save(t, state)
	written, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	save(t, state)

	again, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(written.ModTime()) {
		t.Error("Save() rewrote the sidecar with nothing recorded since the last one")
	}
}

// Nothing recorded is nothing to write. A Dashboard that has done nothing yet
// should not be creating files.
func TestSavingNothingWritesNothing(t *testing.T) {
	path := file(t)

	save(t, load(t, path))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Save() created %s with nothing to record", path)
	}
}

// Active is the caller's to read, not to edit: the sidecar decides what is in
// it, and a caller that could reach in would be a second writer of the file.
func TestActiveHandsOutACopy(t *testing.T) {
	state := load(t, file(t))
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)
	state.Touch("/repo", at)

	delete(state.Active(), "/repo")

	if got := state.Active()["/repo"]; !got.Equal(at) {
		t.Errorf("Active()[/repo] = %v, want %v — the map handed out is the sidecar's own", got, at)
	}
}
