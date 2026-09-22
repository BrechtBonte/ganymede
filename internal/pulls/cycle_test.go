package pulls

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reader is a Reader whose two passes are scripted, and which records what it
// was asked.
type reader struct {
	sets     []Set
	fetchErr []error
	rereads  [][]Pull
	fetches  int
	rereadsN int
}

func (r *reader) Fetch(context.Context) (Set, error) {
	i := min(r.fetches, len(r.sets)-1)
	r.fetches++
	if i < len(r.fetchErr) && r.fetchErr[i] != nil {
		return nil, r.fetchErr[i]
	}
	return r.sets[i], nil
}

func (r *reader) Reread(_ context.Context, rows []Pull) ([]Pull, error) {
	r.rereads = append(r.rereads, rows)
	r.rereadsN++
	read := make([]Pull, len(rows))
	for i, p := range rows {
		p.Mergeable, p.MergeState = "MERGEABLE", "BEHIND"
		read[i] = p
	}
	return read, nil
}

func unresolved(number int) Pull {
	return Pull{
		List: Authored, Repo: "teamleadercrm/core", Number: number, Head: "head",
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}
}

func resolved(number int) Pull {
	p := unresolved(number)
	p.Mergeable, p.MergeState = "MERGEABLE", "BLOCKED"
	return p
}

func TestACycleIsTwoPassesAndOneAnswer(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1), unresolved(2)}}}
	w := Watcher{Read: read, Settle: time.Millisecond}

	report := w.Cycle(context.Background())
	if report.Err != nil {
		t.Fatalf("cycle: %v", report.Err)
	}
	if read.fetches != 1 || read.rereadsN != 1 {
		t.Errorf("passes: %d fetches, %d rereads, want 1 and 1", read.fetches, read.rereadsN)
	}
	// Only the unresolved row is re-read.
	if len(read.rereads[0]) != 1 || read.rereads[0][0].Number != 2 {
		t.Errorf("the second pass asked about %v, want only 2", read.rereads[0])
	}
	// One answer, both passes merged into it — never a provisional paint
	// followed by a settled one.
	if len(report.Set) != 2 {
		t.Fatalf("got %d rows, want 2", len(report.Set))
	}
	for _, p := range report.Set {
		if p.Number == 2 && p.State() != Behind {
			t.Errorf("the re-read row did not land: %q", p.State())
		}
		if p.Number == 1 && p.State() != Sent {
			t.Errorf("the resolved row changed under the second pass: %q", p.State())
		}
	}
	if report.At.IsZero() {
		t.Error("the report carries no time, so the chrome line has nothing to draw")
	}
}

func TestACycleWithNothingUnresolvedMakesOnePass(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1)}}}
	if report := (Watcher{Read: read, Settle: time.Millisecond}).Cycle(context.Background()); report.Err != nil {
		t.Fatal(report.Err)
	}
	if read.rereadsN != 0 {
		t.Errorf("made %d second passes over a fully resolved set, want none", read.rereadsN)
	}
}

func TestAFailedFirstPassIsReportedRatherThanRetriedInsideTheCycle(t *testing.T) {
	boom := &Error{Trouble: Unreachable, Err: errors.New("no route to host")}
	read := &reader{sets: []Set{nil}, fetchErr: []error{boom}}
	report := (Watcher{Read: read, Settle: time.Millisecond}).Cycle(context.Background())
	if report.Err == nil {
		t.Fatal("a failed pass reported no error")
	}
	if got, _ := TroubleOf(report.Err); got != Unreachable {
		t.Errorf("got trouble %v, want Unreachable", got)
	}
	if read.rereadsN != 0 {
		t.Error("a failed first pass still ran a second")
	}
}

func TestANetworkFailureRetriesSoonerThanTheWindow(t *testing.T) {
	// The instinct behind release's 30-minute Retry against a 10-hour Every,
	// scaled: five minutes rather than waiting out the whole window.
	w := Watcher{Every: 30 * time.Minute, Retry: 5 * time.Minute}
	for _, c := range []struct {
		says string
		err  error
		want time.Duration
	}{
		{"a landed cycle waits the window", nil, 30 * time.Minute},
		{"the network is retried sooner", &Error{Trouble: Unreachable}, 5 * time.Minute},
		// Stopping would strand the section until a Dashboard restart, where
		// thirty minutes costs nothing and lets a gh auth login in another
		// pane heal it without one.
		{"an expired login keeps polling at the window", &Error{Trouble: Unauthorized}, 30 * time.Minute},
		{"a login never made keeps polling too", &Error{Trouble: NotLoggedIn}, 30 * time.Minute},
	} {
		if got := w.until(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.says, got, c.want)
		}
	}
}

func TestTheWatchReportsEveryCycleAndAnswersARefresh(t *testing.T) {
	read := &reader{sets: []Set{{resolved(1)}, {resolved(1), resolved(2)}}}
	// A window long enough that only the refresh can produce the second
	// report, so the test is about the key rather than about a clock.
	w := Watcher{Read: read, Every: time.Hour, Retry: time.Hour, Settle: time.Millisecond}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	asks := NewRefresher()
	reports := w.Watch(ctx, asks.Asked())

	first := <-reports
	if len(first.Set) != 1 {
		t.Fatalf("first cycle: got %d rows, want 1", len(first.Set))
	}
	if !asks.Refresh() {
		t.Fatal("a refresh between cycles was refused")
	}
	second := <-reports
	if len(second.Set) != 2 {
		t.Errorf("after r: got %d rows, want 2", len(second.Set))
	}

	stop()
	if _, open := <-reports; open {
		// Drain whatever was in flight, then the channel must close with the
		// watch.
		if _, open := <-reports; open {
			t.Error("the reports channel outlived the context")
		}
	}
}

func TestHoldingRefreshCannotStackCycles(t *testing.T) {
	asks := NewRefresher()
	if !asks.Refresh() {
		t.Fatal("the first ask was refused")
	}
	// A second ask with the first still unanswered is a no-op: holding r
	// cannot stack twelve-second cycles behind each other.
	if asks.Refresh() {
		t.Error("a second ask was accepted while the first was outstanding")
	}
	<-asks.Asked()
	if !asks.Refresh() {
		t.Error("an ask after the first was taken was refused")
	}
}
