package release_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/config"
	"github.com/BrechtBonte/ganymede/internal/release"
)

// refusing is a bucket that must not be asked anything.
func refusing(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the bucket was asked for %s, want a check that used what it remembered", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// remembering is a memory on a state file of this test's own.
func remembering(t *testing.T) *release.Memory {
	t.Helper()
	memory, err := release.Load(config.Sidecar{Path: filepath.Join(t.TempDir(), "state.json")})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return memory
}

// awaiting takes the next check off the watch.
func awaiting(t *testing.T, updates <-chan release.Update, description string) release.Update {
	t.Helper()
	select {
	case update, ok := <-updates:
		if !ok {
			t.Fatalf("the watch stopped before reporting %s", description)
		}
		return update
	case <-time.After(5 * time.Second):
		t.Fatalf("the watch never reported %s", description)
		return release.Update{}
	}
}

// TestAFreshMemoryIsUsedWithoutAskingTheBucket is what makes ten hours mean
// ten hours. A Dashboard restarted four times in an afternoon would otherwise

// TestAFreshMemoryIsUsedWithoutAskingTheBucket is what makes ten hours mean
// ten hours. A Dashboard restarted four times in an afternoon would otherwise
// have made four checks.
func TestAFreshMemoryIsUsedWithoutAskingTheBucket(t *testing.T) {
	memory := remembering(t)
	if err := memory.Remember(release.Remembered{CheckedAt: time.Now(), Channel: "stable", Latest: "2.1.240"}); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: latest\n"),
		Releases: refusing(t),
		Memory:   memory,
	}

	update := awaiting(t, checker.Watch(t.Context()), "the remembered check")

	if update.Latest != "2.1.240" || update.Channel != "stable" {
		t.Errorf("reported %+v, want the remembered 2.1.240 on stable", update)
	}
	if !update.Behind() {
		t.Error("Behind() = false, want true: 2.1.237 is behind the remembered 2.1.240")
	}
}

// The install is still read fresh: it is the half of the comparison that moves
// on its own, since Claude Code updates itself whenever a Session starts. A
// remembered answer paired with a remembered install would go on saying you
// were behind for hours after you had caught up.
func TestARememberedCheckStillReadsTheInstall(t *testing.T) {
	memory := remembering(t)
	if err := memory.Remember(release.Remembered{CheckedAt: time.Now(), Channel: "latest", Latest: "2.1.240"}); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.240 (Claude Code)\n", "Auto-update channel: latest\n"),
		Releases: refusing(t),
		Memory:   memory,
	}

	update := awaiting(t, checker.Watch(t.Context()), "the remembered check")

	if update.Installed != "2.1.240" {
		t.Errorf("Installed = %q, want the 2.1.240 on the machine now", update.Installed)
	}
	if update.Behind() {
		t.Error("Behind() = true, want false: the install has caught up with what was remembered")
	}
}

// A memory older than the window is a check that is due, and the answer it
// comes back with is written down for the restarts to come.
func TestAStaleMemoryIsCheckedAgainAndWrittenBack(t *testing.T) {
	memory := remembering(t)
	if err := memory.Remember(release.Remembered{CheckedAt: time.Now().Add(-11 * time.Hour), Channel: "latest", Latest: "2.1.100"}); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: latest\n"),
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
		Memory:   memory,
	}

	update := awaiting(t, checker.Watch(t.Context()), "the check that was due")

	if update.Latest != "2.1.240" {
		t.Errorf("Latest = %q, want the 2.1.240 the bucket is publishing", update.Latest)
	}
	if remembered := memory.Remembered(); remembered.Latest != "2.1.240" || remembered.CheckedAt.IsZero() {
		t.Errorf("remembered %+v, want the check just made", remembered)
	}
}

// A notice standing on the Dashboard is confirmed against the install far more
// often than the bucket is asked, so that the update you have just installed
// takes the notice down with it. Claude Code updates itself whenever a Session
// starts, which is exactly while the Dashboard is up.
func TestAStandingNoticeIsClearedWhenTheInstallCatchesUp(t *testing.T) {
	claude := claudeThat(t, "2.1.237 (Claude Code)\n", "Auto-update channel: latest\n")
	checker := release.Checker{
		Claude:   claude,
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
		Every:    time.Hour,
		Confirm:  20 * time.Millisecond,
	}
	updates := checker.Watch(t.Context())

	if behind := awaiting(t, updates, "the first check"); !behind.Behind() {
		t.Fatalf("the first check reported %+v, want an install behind the bucket", behind)
	}
	nowSaying(t, claude, "2.1.240 (Claude Code)\n")

	caught := awaiting(t, updates, "the install catching up")

	if caught.Behind() {
		t.Errorf("reported %+v, want an install level with the bucket", caught)
	}
	if caught.Latest != "2.1.240" {
		t.Errorf("Latest = %q, want the 2.1.240 already asked about", caught.Latest)
	}
}

// An install that is current costs nothing between checks. There is no notice
// to be wrong, so there is nothing to confirm — and a Dashboard that spawned a
// process every half minute for the answer it already had would be paying the
// whole cost of the feature to learn nothing.
func TestACurrentInstallIsLeftAloneBetweenChecks(t *testing.T) {
	checker := release.Checker{
		Claude:   claudeThat(t, "2.1.240 (Claude Code)\n", "Auto-update channel: latest\n"),
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
		Every:    time.Hour,
		Confirm:  20 * time.Millisecond,
	}
	updates := checker.Watch(t.Context())

	if current := awaiting(t, updates, "the first check"); current.Behind() {
		t.Fatalf("the first check reported %+v, want an install level with the bucket", current)
	}

	checked := asks(t, checker.Claude)
	time.Sleep(200 * time.Millisecond)

	if since := asks(t, checker.Claude) - checked; since != 0 {
		t.Errorf("Claude Code was run %d more times, want an install that is current left alone", since)
	}
}

// A check that cannot be made says nothing at all. The Dashboard draws no
// notice either way, and an Update invented to fill the silence would be a
// notice claiming to know something the harness never found out.
func TestWatchSaysNothingWhenTheCheckCannotBeMade(t *testing.T) {
	checker := release.Checker{
		Claude:   filepath.Join(t.TempDir(), "no-claude-here"),
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
		Every:    time.Hour,
		Retry:    20 * time.Millisecond,
	}
	updates := checker.Watch(t.Context())

	select {
	case update, ok := <-updates:
		if ok {
			t.Errorf("reported %+v, want nothing from a check that could not be made", update)
		} else {
			t.Error("the watch stopped rather than keeping on trying")
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// And it keeps trying on the shorter timer, so a laptop that had no network
// when the Dashboard came up is not left without an answer for ten hours.
func TestWatchTriesAgainSoonAfterACheckThatFailed(t *testing.T) {
	claude := claudeThat(t, "", "Auto-update channel: latest\n")
	checker := release.Checker{
		Claude:   claude,
		Releases: publishing(t, map[string]string{"latest": "2.1.240"}),
		Every:    time.Hour,
		Retry:    20 * time.Millisecond,
	}
	updates := checker.Watch(t.Context())
	awaitAsked(t, claude, 1, "for the version it could not read")
	nowSaying(t, claude, "2.1.237 (Claude Code)\n")

	update := awaiting(t, updates, "the check after the one that failed")

	if update.Installed != "2.1.237" {
		t.Errorf("Installed = %q, want the version the retry could read", update.Installed)
	}
}
