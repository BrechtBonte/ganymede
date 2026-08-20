// Package release asks whether the Claude Code on this machine is behind the
// one Anthropic is publishing.
//
// The answer is worth very little and costs a network call, so it is asked
// rarely — once every ten hours — and remembered across restarts. Nothing here
// installs anything: the Dashboard says an update exists and `claude update`
// is still the thing that goes and gets it.
package release

import (
	"strconv"
	"strings"
)

// Update is what one check found: the build installed here, the build the
// channel is publishing, and which channel that was.
type Update struct {
	// Installed is the version of the Claude Code on PATH, as it reported it.
	Installed string
	// Latest is the version the channel is publishing.
	Latest string
	// Channel is the auto-update channel the comparison was made against —
	// `latest` or `stable`. Comparing a stable install against the latest
	// channel would report an update it can never install.
	Channel string
}

// Behind says the installed build is older than the published one.
//
// A comparison missing either side, or with either side in a shape this cannot
// read, is not behind. Every one of those is the check having failed to find
// something out, and a notice invented out of half an answer would be worse
// than the notice not being drawn at all.
func (u Update) Behind() bool {
	installed, read := fields(u.Installed)
	if !read {
		return false
	}
	latest, read := fields(u.Latest)
	if !read {
		return false
	}
	for i := range max(len(installed), len(latest)) {
		if at(installed, i) != at(latest, i) {
			return at(installed, i) < at(latest, i)
		}
	}
	return false
}

// fields reads a version into its numeric parts. They are compared as numbers
// rather than as text because 2.1.99 and 2.1.100 sort the other way round when
// read as strings, and that is a comparison this gets wrong roughly once every
// hundred releases.
func fields(version string) ([]int, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, false
	}
	parts := strings.Split(version, ".")
	read := make([]int, 0, len(parts))
	for _, part := range parts {
		field, err := strconv.Atoi(part)
		if err != nil || field < 0 {
			return nil, false
		}
		read = append(read, field)
	}
	return read, true
}

// at is a version's ith field, and zero past its end: 2.1 and 2.1.0 are the
// same release, and one written each way must not read as an update.
func at(fields []int, i int) int {
	if i >= len(fields) {
		return 0
	}
	return fields[i]
}
