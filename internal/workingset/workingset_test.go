package workingset_test

import (
	"slices"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/workingset"
)

// now is the moment every one of these working sets is worked out at.
var now = time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)

// ago is a stamp that far back.
func ago(d time.Duration) time.Time { return now.Add(-d) }

func want(t *testing.T, got, wanted []string) {
	t.Helper()
	if !slices.Equal(got, wanted) {
		t.Errorf("Roots() = %v, want %v", got, wanted)
	}
}

// The first reason a repo is on the Dashboard: something is running in it.
func TestRepoWithALiveSessionIsInTheWorkingSet(t *testing.T) {
	set := workingset.Membership{Live: []string{"/repos/ganymede"}}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// The second: you were working in it recently enough that it is still where
// you are, even with nothing running in it this minute.
func TestRepoWorkedInInsideTheWindowIsInTheWorkingSet(t *testing.T) {
	set := workingset.Membership{Active: map[string]time.Time{"/repos/ganymede": ago(2 * 24 * time.Hour)}}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// And the eviction that keeps the Dashboard down to the handful of repos it
// is for: a repo you have not touched in a week is one you can still reach
// through the picker, and one the Dashboard should stop spending a row on.
func TestRepoNotWorkedInSinceTheWindowDropsOff(t *testing.T) {
	set := workingset.Membership{Active: map[string]time.Time{"/repos/archived": ago(8 * 24 * time.Hour)}}

	want(t, set.Roots(now), nil)
}

// The window is a closing door, not a threshold to be over: a repo touched
// exactly the window ago has not yet gone a window without you.
func TestRepoWorkedInExactlyAWindowAgoIsStillInTheWorkingSet(t *testing.T) {
	set := workingset.Membership{Active: map[string]time.Time{"/repos/ganymede": ago(workingset.Window)}}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// A live Session outranks the clock. A repo running an agent is where you are
// working, whatever the sidecar last remembered about it — and a Session
// started outside the scan roots is on the Dashboard for the same reason.
func TestRepoWithALiveSessionIsNeverEvicted(t *testing.T) {
	set := workingset.Membership{
		Live:   []string{"/repos/ganymede"},
		Active: map[string]time.Time{"/repos/ganymede": ago(30 * 24 * time.Hour)},
	}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// A Claimed root is one you have reserved on purpose, typically to review a
// PR in. Taking it off the Dashboard would be the harness forgetting a
// decision you made, and hiding the row you release it from.
func TestClaimedRepoIsNeverEvicted(t *testing.T) {
	set := workingset.Membership{
		Claimed: []string{"/repos/ganymede"},
		Active:  map[string]time.Time{"/repos/ganymede": ago(30 * 24 * time.Hour)},
	}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// A repo Claimed with nothing else known about it is still on the Dashboard:
// the claim is the whole reason for the row.
func TestClaimedRepoIsInTheWorkingSetOnItsOwn(t *testing.T) {
	set := workingset.Membership{Claimed: []string{"/repos/ganymede"}}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// One repo is one row however many ways it earned it, and the order is
// settled here so that the Dashboard's own ordering has something stable
// underneath it.
func TestEachRootIsListedOnceAndSorted(t *testing.T) {
	set := workingset.Membership{
		Live:    []string{"/repos/ganymede", "/repos/atlas"},
		Claimed: []string{"/repos/ganymede"},
		Active:  map[string]time.Time{"/repos/ganymede": ago(time.Hour), "/repos/borealis": ago(time.Hour)},
	}

	want(t, set.Roots(now), []string{"/repos/atlas", "/repos/borealis", "/repos/ganymede"})
}

// The window is configurable, and a harness told to keep a shorter memory
// keeps one.
func TestWindowCanBeShortened(t *testing.T) {
	set := workingset.Membership{
		Active: map[string]time.Time{"/repos/ganymede": ago(2 * time.Hour)},
		Window: time.Hour,
	}

	want(t, set.Roots(now), nil)
}

// A stamp in the future is a clock that has jumped — a machine that slept, a
// file synced from elsewhere. It is recent by any reading, and reading it as
// ancient would drop a repo you are working in this minute.
func TestStampInTheFutureCountsAsRecent(t *testing.T) {
	set := workingset.Membership{Active: map[string]time.Time{"/repos/ganymede": now.Add(time.Hour)}}

	want(t, set.Roots(now), []string{"/repos/ganymede"})
}

// Nothing running, nothing claimed, nothing remembered: an empty Dashboard,
// not a broken one.
func TestNothingAtAllIsAnEmptyWorkingSet(t *testing.T) {
	want(t, workingset.Membership{}.Roots(now), nil)
}
