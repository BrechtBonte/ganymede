// Package shellpath reads the PATH your terminal would have.
//
// An app launched from Finder or Spotlight inherits launchd's PATH, which
// holds the system directories and nothing you added yourself. Everything
// the harness starts — tmux, the Dashboard, claude in every Session — inherits
// it in turn, so a claude installed under ~/.local/bin is simply not there.
package shellpath

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// marker fences the PATH off from whatever the startup files print around it.
const marker = "__ganymede_path__"

// Login asks shell, started as an interactive login shell, for the PATH it
// ends up with. Interactive as well as login, because .zshrc is where a PATH
// is most often extended, and only an interactive shell reads it.
//
// It gives up after timeout: a startup file waiting on input it will never get
// must not hold the harness up.
func Login(shell string, timeout time.Duration) (string, error) {
	if shell == "" {
		return "", errors.New("SHELL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '`+marker+`%s`+marker+`' "$PATH"`)
	// Killing the shell leaves any child it started holding stdout open, and
	// Output would wait on that child for as long as it runs.
	cmd.WaitDelay = timeout
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s did not answer within %v", shell, timeout)
	}
	if err != nil {
		return "", fmt.Errorf("ask %s for its PATH: %w", shell, err)
	}

	_, rest, found := strings.Cut(string(out), marker)
	path, _, closed := strings.Cut(rest, marker)
	if !found || !closed || path == "" {
		return "", fmt.Errorf("%s printed no PATH", shell)
	}
	return path, nil
}
