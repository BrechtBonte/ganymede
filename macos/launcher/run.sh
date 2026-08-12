#!/usr/bin/env bash
set -euo pipefail

# A process launched by LaunchServices (Spotlight, double-click, `open`)
# inherits the login session's default environment, not an interactive
# shell's rc-augmented one — so Homebrew's bin directories are not
# guaranteed to be on PATH even though `up` shells out to `tmux` and looks
# up `terminal-notifier` by bare name.
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

exec "@@REPO_DIR@@/bin/ganymede" up "@@REPO_DIR@@"
