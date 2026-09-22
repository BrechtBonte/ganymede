package pulls

import (
	"context"
	"time"
)

// defaultEvery is how long between cycles. The rate cost is 4 points an hour
// against 5,000 — 0.08% — so this is not politeness to a budget; it is the
// pace at which a census of your own pull requests actually changes.
const defaultEvery = 30 * time.Minute

// defaultRetry is how long before a cycle the network ate is tried again. The
// instinct behind release's 30-minute Retry against its 10-hour Every, scaled:
// a laptop opened on a train has no business waiting out the whole window
// after it finds a connection.
const defaultRetry = 5 * time.Minute

// defaultSettle is how long the cycle waits between its two passes. Measured:
// a re-read five seconds after a cold read resolved 30 of 30, and held at
// +10, +15 and +20 s — so five seconds is the knee rather than a guess.
const defaultSettle = 5 * time.Second

// Reader is one pass of the census, and the re-read of the rows it could not
// resolve. Fetcher is the one that talks to GitHub; a test supplies its own.
type Reader interface {
	Fetch(ctx context.Context) (Set, error)
	Reread(ctx context.Context, rows []Pull) ([]Pull, error)
}

// Report is one cycle's answer: the set, when it landed, and why it did not.
//
// A failed cycle carries no set rather than an empty one. The two are
// different claims — "you have none" against "I could not ask" — and the
// section renders them differently, which is the whole of why silence was
// ruled out as a rendering.
type Report struct {
	Set Set
	At  time.Time
	Err error
}

// Watcher runs a cycle now and again, and whenever you ask for one.
type Watcher struct {
	// Read is where the census comes from. Nil is a Watcher that reports
	// nothing, which is a harness that was never wired up.
	Read Reader
	// Every is how long between cycles. Zero means the default.
	Every time.Duration
	// Retry is how long before a cycle the network ate is tried again. Zero
	// means the default.
	Retry time.Duration
	// Settle is how long the cycle waits between its two passes. Zero means
	// the default.
	Settle time.Duration
	// Now is the clock, for a test that needs one of its own. Nil means
	// time.Now.
	Now func() time.Time
}

// Cycle is one whole answer: both lists, the rows that came back unresolved
// re-read after Settle, and one report.
//
// Atomic rather than progressive. A paint at six seconds followed by a settled
// one at twelve buys six seconds exactly once, at Dashboard start, and charges
// for it permanently — a provisional rendering seen for five seconds every half
// hour, and a section carrying two ages at once where the chrome line has to
// state one. In the steady state the twelve seconds are invisible, because the
// section spends them showing the previous cycle.
func (w Watcher) Cycle(ctx context.Context) Report {
	if w.Read == nil {
		return Report{At: w.now()}
	}

	set, err := w.Read.Fetch(ctx)
	if err != nil {
		return Report{At: w.now(), Err: err}
	}

	unresolved := set.Unresolved()
	if len(unresolved) == 0 {
		return Report{Set: set, At: w.now()}
	}

	select {
	case <-ctx.Done():
		return Report{Set: set, At: w.now()}
	case <-time.After(w.settle()):
	}

	// A second pass that could not be made is not a failed cycle. The first
	// pass is in hand and every row in it is honest — the unresolved ones
	// simply draw no state word, which is what they would have drawn anyway
	// had the re-read come back still UNKNOWN.
	read, err := w.Read.Reread(ctx, unresolved)
	if err != nil {
		return Report{Set: set, At: w.now()}
	}

	fresh := make(map[named]Pull, len(read))
	for _, p := range read {
		fresh[named{p.Repo, p.Number}] = p
	}
	merged := make(Set, len(set))
	for i, p := range set {
		if settled, ok := fresh[named{p.Repo, p.Number}]; ok {
			merged[i] = settled
			continue
		}
		merged[i] = p
	}
	return Report{Set: merged, At: w.now()}
}

// named is what identifies a Pull between one cycle's two passes: the pair the
// second pass asked about, since GitHub's own node id is not in the field set
// and the row's position is not stable across two requests.
type named struct {
	repo   string
	number int
}

// Watch runs a cycle now, then every Every — sooner after a network failure,
// and at once whenever refresh carries an ask. The channel is closed when the
// watch stops.
//
// The clock runs whether or not the section is drawn. That is not a preference
// but a consequence of the Your-move mark: the mark lives on a Session row, the
// tree is always drawn, and collapsing Pulls stops it drawing rows rather than
// fetching them.
func (w Watcher) Watch(ctx context.Context, refresh <-chan struct{}) <-chan Report {
	reports := make(chan Report)
	go w.watch(ctx, refresh, reports)
	return reports
}

func (w Watcher) watch(ctx context.Context, refresh <-chan struct{}, reports chan<- Report) {
	defer close(reports)

	for {
		report := w.Cycle(ctx)
		select {
		case reports <- report:
		case <-ctx.Done():
			return
		}

		// Anything asked for while the cycle was running is discarded here
		// rather than queued: r is a no-op while a cycle is in flight, so
		// holding it cannot stack twelve-second cycles behind each other.
		select {
		case <-refresh:
		default:
		}

		due := time.NewTimer(w.until(report.Err))
		select {
		case <-ctx.Done():
			due.Stop()
			return
		case <-due.C:
		case <-refresh:
			due.Stop()
		}
	}
}

// until is how long before the next cycle, given how this one ended.
//
// A network failure retries at Retry. An auth failure does not: it drops back
// to the plain window and keeps going, so a `gh auth login` in another pane
// heals the section without a Dashboard restart. Stopping would be correct
// about gh never refreshing itself and wrong about what that costs.
func (w Watcher) until(err error) time.Duration {
	if trouble, ok := TroubleOf(err); ok && trouble == Unreachable {
		return w.retry()
	}
	return w.every()
}

func (w Watcher) every() time.Duration {
	if w.Every <= 0 {
		return defaultEvery
	}
	return w.Every
}

func (w Watcher) retry() time.Duration {
	if w.Retry <= 0 {
		return defaultRetry
	}
	return w.Retry
}

func (w Watcher) settle() time.Duration {
	if w.Settle <= 0 {
		return defaultSettle
	}
	return w.Settle
}

func (w Watcher) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

// Refresher is the hand `r` pulls: it asks for a cycle now, and says whether
// the ask landed.
//
// One outstanding ask at a time. The channel holds a single token, so a second
// press with the first still untaken is refused rather than queued — which is
// what makes holding the key a no-op rather than a way to stack cycles.
type Refresher struct{ asked chan struct{} }

// NewRefresher returns a Refresher with nothing outstanding.
func NewRefresher() *Refresher {
	return &Refresher{asked: make(chan struct{}, 1)}
}

// Refresh asks for a cycle, and reports whether this ask is the one that will
// be answered.
func (r *Refresher) Refresh() bool {
	if r == nil {
		return false
	}
	select {
	case r.asked <- struct{}{}:
		return true
	default:
		return false
	}
}

// Asked is what the watch selects on.
func (r *Refresher) Asked() <-chan struct{} { return r.asked }
