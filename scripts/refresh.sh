#!/usr/bin/env bash
set -euo pipefail

# refresh rebuilds the ganymede binary and restarts the Dashboard's own
# tmux window with it.
#
# `ganymede up` only ever reuses an existing Dashboard session
# (harness.go's ensureSession checks has-session and returns early) — it
# never re-execs it, so a rebuilt bin/ganymede on disk never reaches the
# process that already exec'd the old one into memory. Killing and
# respawning just that one pane, in place, is what actually picks up a
# fresh build. The dock, the working client, and every repo Session are
# untouched: they live on a different tmux server (or a different
# session entirely) from the Dashboard's own.

cd "$(dirname "$0")/.."

go build -o bin/ganymede ./cmd/ganymede
bin="$(pwd)/bin/ganymede"

if tmux -L default has-session -t "=ganymede" 2>/dev/null; then
	# respawn-pane wants a fully-qualified session:window.pane target — it
	# does not descend from a bare session name to its active pane the way
	# attach-session/has-session do. ensureSession always creates the
	# Dashboard as a fresh single-window, single-pane session, so :0.0 is
	# always its one pane.
	tmux -L default respawn-pane -k -t "=ganymede:0.0" "$bin" dashboard
	echo "Dashboard restarted with the freshly built binary."
else
	echo "No running Dashboard session found; bringing the harness up."
	"$bin" up
fi
