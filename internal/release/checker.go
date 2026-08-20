package release

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// defaultTimeout is how long one check may take. Generous next to the moment
// this takes in practice, and short next to the ten hours until the next one.
const defaultTimeout = 10 * time.Second

// defaultReleases is where Claude Code's own installer reads published
// versions from: one file per channel, holding nothing but a version string.
const defaultReleases = "https://downloads.claude.ai/claude-code-releases"

// defaultChannel is the auto-update channel assumed when this harness cannot
// find out which one the install is on.
const defaultChannel = "latest"

// Checker asks what is installed here and what is published there.
type Checker struct {
	// Claude is the Claude Code binary to ask. Empty means the claude on PATH.
	Claude string
	// Releases is where published versions are read from. Empty means the
	// bucket Claude Code's own installer downloads from.
	Releases string
	// Timeout is how long one check may take. Zero means the default.
	Timeout time.Duration
	// Every is how long between checks of the bucket. Zero means the default.
	Every time.Duration
	// Retry is how long before a check that could not be made is tried again.
	// Zero means the default.
	Retry time.Duration
	// Confirm is how often the install is re-read while a notice is standing.
	// Zero means the default.
	Confirm time.Duration
	// Memory is where the last check is kept across restarts. Nil is a
	// Checker that remembers nothing, and so checks every time it is started.
	Memory *Memory
}

// version is the release Claude Code names itself by, at the front of what it
// prints for `--version`: `2.1.237 (Claude Code)`.
var version = regexp.MustCompile(`\b\d+(\.\d+)+\b`)

// Read asks once.
func (c Checker) Read(ctx context.Context) (Update, error) {
	ctx, giveUp := context.WithTimeout(ctx, c.timeout())
	defer giveUp()

	installed, err := c.Installed(ctx)
	if err != nil {
		return Update{}, err
	}
	channel := c.channel(ctx)
	latest, err := c.latest(ctx, channel)
	if err != nil {
		return Update{}, err
	}
	return Update{Installed: installed, Latest: latest, Channel: channel}, nil
}

// Installed is the version of the Claude Code this harness would run.
//
// It is the cheap half of the check — one process, no network — which is what
// lets a standing notice be confirmed far more often than the bucket is asked.
func (c Checker) Installed(ctx context.Context) (string, error) {
	ctx, giveUp := context.WithTimeout(ctx, c.timeout())
	defer giveUp()

	claude := c.claude()
	// Output keeps whatever Claude Code has to say for itself out of the
	// terminal, which the Dashboard is drawing in.
	said, err := exec.CommandContext(ctx, claude, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("ask %s its version: %w", claude, err)
	}
	found := version.FindString(strings.TrimSpace(string(said)))
	if found == "" {
		return "", fmt.Errorf("read the version %s reported: %q", claude, said)
	}
	return found, nil
}

// channel is the auto-update channel this install follows.
//
// It is read out of `claude doctor`, which is text written for a person rather
// than an interface with a shape anything promises to keep — the same footing
// the registry is on, and read the same way: best-effort, and never worth the
// check it is part of. A doctor that has been reworded, or will not run at
// all, leaves the check on the channel nearly every install is on rather than
// abandoning it.
func (c Checker) channel(ctx context.Context) string {
	claude := c.claude()
	said, err := exec.CommandContext(ctx, claude, "doctor").Output()
	if err != nil {
		return defaultChannel
	}
	found := channelLine.FindSubmatch(said)
	if found == nil {
		return defaultChannel
	}
	return string(found[1])
}

// channelLine is doctor's one line about which channel the install follows.
var channelLine = regexp.MustCompile(`(?m)^Auto-update channel:[ \t]*(\S+)`)

// latest is the version the channel is publishing.
func (c Checker) latest(ctx context.Context, channel string) (string, error) {
	releases := c.Releases
	if releases == "" {
		releases = defaultReleases
	}
	url := strings.TrimSuffix(releases, "/") + "/" + channel

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("ask %s what it is publishing: %w", url, err)
	}
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("ask %s what it is publishing: %w", url, err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ask %s what it is publishing: %s", url, answer.Status)
	}

	// The file holds a version string and nothing else. The read is capped
	// because this is a remote answer the harness has no other reason to
	// trust, and a channel file that had turned into something enormous would
	// otherwise be read into the Dashboard's memory in full.
	body, err := io.ReadAll(io.LimitReader(answer.Body, 1024))
	if err != nil {
		return "", fmt.Errorf("read what %s is publishing: %w", url, err)
	}
	found := version.FindString(strings.TrimSpace(string(body)))
	if found == "" {
		return "", fmt.Errorf("read what %s is publishing: %q", url, body)
	}
	return found, nil
}

func (c Checker) claude() string {
	if c.Claude == "" {
		return "claude"
	}
	return c.Claude
}

func (c Checker) timeout() time.Duration {
	if c.Timeout <= 0 {
		return defaultTimeout
	}
	return c.Timeout
}
