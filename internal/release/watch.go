package release

import (
	"context"
	"time"
)

// defaultEvery is how long between checks of the bucket. What is published is
// news roughly once a day and is never urgent — the harness draws it, it does
// not act on it — so this is deliberately slow enough that a Dashboard left up
// for a week costs a handful of requests.
const defaultEvery = 10 * time.Hour

// defaultRetry is how long before a check that could not be made is tried
// again. A laptop opened on a train has no network and no business waiting
// out the whole window before noticing it has one again.
const defaultRetry = 30 * time.Minute

// defaultConfirm is how often the install is re-read while the Dashboard is
// showing a notice. Claude Code updates itself whenever a Session starts, so
// the notice would otherwise stand for hours after you had already caught up
// — and a notice that is wrong is worse than one that is late.
const defaultConfirm = 30 * time.Second

// Watch checks now and again every Every, until ctx ends. The channel is
// closed when the watch stops.
//
// The first check does not always cost a request: a memory younger than the
// window is used as it stands, which is what makes a Dashboard restarted four
// times in an afternoon still one check.
//
// Nothing here is worth an error. A check that cannot be made reports nothing
// at all rather than an empty Update — the Dashboard would draw the absence of
// a notice either way, and the difference is that one of them is honest — and
// is tried again on the shorter Retry.
func (c Checker) Watch(ctx context.Context) <-chan Update {
	updates := make(chan Update)
	go c.watch(ctx, updates)
	return updates
}

func (c Checker) watch(ctx context.Context, updates chan<- Update) {
	defer close(updates)

	for {
		update, err := c.checked(ctx)
		until := c.retry()
		if err == nil {
			if !send(ctx, updates, update) {
				return
			}
			until = c.every()
		}
		if !c.holding(ctx, until, update, updates) {
			return
		}
	}
}

// checked is one check: the bucket if the memory is stale, and what was
// remembered if it is not. Either way the install is read fresh.
func (c Checker) checked(ctx context.Context) (Update, error) {
	remembered := c.Memory.Remembered()
	if time.Since(remembered.CheckedAt) >= c.every() {
		update, err := c.Read(ctx)
		if err != nil {
			return Update{}, err
		}
		// A check that could not be written down is still a check that was
		// made: the Dashboard draws it, and the only cost of the memory not
		// landing is that the next start asks again.
		_ = c.Memory.Remember(Remembered{CheckedAt: time.Now(), Channel: update.Channel, Latest: update.Latest})
		return update, nil
	}

	installed, err := c.Installed(ctx)
	if err != nil {
		return Update{}, err
	}
	return Update{Installed: installed, Latest: remembered.Latest, Channel: remembered.Channel}, nil
}

// holding waits until the next check is due, watching the install while a
// notice is standing so that updating clears it within the half minute rather
// than within the window.
func (c Checker) holding(ctx context.Context, until time.Duration, update Update, updates chan<- Update) bool {
	due := time.NewTimer(until)
	defer due.Stop()
	confirm := time.NewTicker(c.confirm())
	defer confirm.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-due.C:
			return true
		case <-confirm.C:
			// Nothing is standing, so there is nothing to be wrong: the
			// install is left alone until the window comes round.
			if !update.Behind() {
				continue
			}
			installed, err := c.Installed(ctx)
			if err != nil || installed == update.Installed {
				continue
			}
			update.Installed = installed
			if !send(ctx, updates, update) {
				return false
			}
		}
	}
}

func send(ctx context.Context, updates chan<- Update, update Update) bool {
	select {
	case updates <- update:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c Checker) every() time.Duration {
	if c.Every <= 0 {
		return defaultEvery
	}
	return c.Every
}

func (c Checker) retry() time.Duration {
	if c.Retry <= 0 {
		return defaultRetry
	}
	return c.Retry
}

func (c Checker) confirm() time.Duration {
	if c.Confirm <= 0 {
		return defaultConfirm
	}
	return c.Confirm
}
