package workingset_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/workingset"
)

// file is a state file inside a temporary directory the test owns.
func file(t *testing.T) config.Sidecar {
	t.Helper()
	return config.Sidecar{Path: filepath.Join(t.TempDir(), "state.json")}
}

// load reads the activity in state, failing the test if it cannot be read.
func load(t *testing.T, state config.Sidecar) *workingset.Activity {
	t.Helper()
	activity, err := workingset.Load(state)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return activity
}

// touch records activity, failing the test if it cannot be written down.
func touch(t *testing.T, activity *workingset.Activity, root string, at time.Time) {
	t.Helper()
	if err := activity.Touch(root, at); err != nil {
		t.Fatalf("Touch(%q): %v", root, err)
	}
}

// The point of keeping this in a file: a repo you worked in is still on the
// Dashboard after the Dashboard has been restarted, which is the only way the
// recency window means anything at all.
func TestActivitySurvivesBeingReloaded(t *testing.T) {
	state := file(t)
	when := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)

	touch(t, load(t, state), "/Users/brecht/Projects/ganymede", when)

	if got := load(t, state).Active()["/Users/brecht/Projects/ganymede"]; !got.Equal(when) {
		t.Errorf("Active()[ganymede] = %v, want %v", got, when)
	}
}

// A state file that is not there yet is a harness that has not been used yet,
// which is the state every first run is in.
func TestActivityThatIsNotThereLoadsEmpty(t *testing.T) {
	if got := load(t, file(t)).Active(); len(got) != 0 {
		t.Errorf("Active() = %v, want nothing recorded", got)
	}
}

// Activity is the last time you were in a repo, so an older stamp arriving
// late — a Session whose root was already touched by something newer — must
// not drag the repo back towards falling off.
func TestActivityKeepsTheLatestStamp(t *testing.T) {
	activity := load(t, file(t))
	newest := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)

	touch(t, activity, "/repo", newest)
	touch(t, activity, "/repo", newest.Add(-time.Hour))

	if got := activity.Active()["/repo"]; !got.Equal(newest) {
		t.Errorf("Active()[/repo] = %v, want the latest stamp %v", got, newest)
	}
}

// The tree is rebuilt every time any Session moves, and each rebuild stamps
// every repo with a Session in it. The window is measured in days, so seconds
// are not worth a write.
func TestStampsWithinTheSameMinuteAreNotWrittenAgain(t *testing.T) {
	state := file(t)
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)
	activity := load(t, state)
	touch(t, activity, "/repo", at)
	written, err := os.Stat(state.Path)
	if err != nil {
		t.Fatal(err)
	}

	touch(t, activity, "/repo", at.Add(30*time.Second))

	again, err := os.Stat(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(written.ModTime()) {
		t.Error("Touch rewrote the state file for a stamp inside the same minute")
	}
}

// A minute later is a real move, and has to land — otherwise a repo you are
// working in every day would still fall off seven days after the first time
// the harness heard of it.
func TestStampsANewMinuteAreWritten(t *testing.T) {
	state := file(t)
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)
	activity := load(t, state)

	touch(t, activity, "/repo", at)
	touch(t, activity, "/repo", at.Add(time.Minute))

	if got, want := load(t, state).Active()["/repo"], at.Add(time.Minute); !got.Equal(want) {
		t.Errorf("Active()[/repo] = %v, want %v", got, want)
	}
}

// The state file is shared with everything else the harness remembers. Writing
// activity into it must leave the rest exactly as it was.
func TestOtherSectionsOfTheStateFileAreLeftAlone(t *testing.T) {
	state := file(t)
	if err := state.Write("tickets", []string{"FIRE-2841"}); err != nil {
		t.Fatal(err)
	}

	touch(t, load(t, state), "/repo", time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC))

	var kept []string
	if err := state.Read("tickets", &kept); err != nil {
		t.Fatalf("Read(tickets): %v", err)
	}
	if len(kept) != 1 || kept[0] != "FIRE-2841" {
		t.Errorf("the tickets section reads %v, want it untouched", kept)
	}
}

// A state file somebody has hand-edited into nonsense must not stop the
// Dashboard opening: it costs the harness its memory of the quiet repos, and
// everything running still shows.
func TestStateFileThatCannotBeReadIsUsableAndReported(t *testing.T) {
	state := file(t)
	if err := os.WriteFile(state.Path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	activity, err := workingset.Load(state)
	if err == nil {
		t.Error("Load() reported nothing wrong with a state file it could not read")
	}
	if activity == nil {
		t.Fatal("Load() handed back nothing to go on with")
	}
	if got := activity.Active(); len(got) != 0 {
		t.Errorf("Active() = %v, want nothing recorded", got)
	}
}

// Active is the caller's to read, not to edit: this is the one writer of the
// section, and a caller that could reach in would be a second.
func TestActiveHandsOutACopy(t *testing.T) {
	activity := load(t, file(t))
	at := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)
	touch(t, activity, "/repo", at)

	delete(activity.Active(), "/repo")

	if got := activity.Active()["/repo"]; !got.Equal(at) {
		t.Errorf("Active()[/repo] = %v, want %v — the map handed out is the harness's own", got, at)
	}
}
