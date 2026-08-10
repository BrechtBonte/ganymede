// Package ghostty opens the emulator window the harness lives in. Ghostty is
// purely that window: none of its tabs or splits are used, because tmux owns
// the layout.
package ghostty

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// bundleBinary is where the macOS app keeps its executable, which doubles as
// Ghostty's CLI.
const bundleBinary = "/Applications/Ghostty.app/Contents/MacOS/ghostty"

// app is the bundle Activate brings forward.
const app = "/Applications/Ghostty.app"

// Emulator opens windows.
type Emulator struct {
	// Binary is the emulator executable. Empty means the installed Ghostty.
	Binary string
	// Width and Height size the window in cells. Zero leaves it to Ghostty.
	Width, Height int
	// FrontApp is how Frontmost asks which application is in front of you.
	// Empty means System Events' own answer.
	FrontApp func() (string, error)
}

// frontmostName is what System Events calls Ghostty's own process.
const frontmostName = "Ghostty"

// Frontmost reports whether Ghostty is the application in front of you — the
// one signal the notifier gates every banner on (§9): no banner fires while
// the sidepanel is already telling you the same thing.
func (e Emulator) Frontmost() (bool, error) {
	query := e.FrontApp
	if query == nil {
		query = frontApp
	}
	name, err := query()
	if err != nil {
		return false, err
	}
	return name == frontmostName, nil
}

// frontApp asks System Events which application is in front — the one place
// macOS answers this from outside the application itself.
func frontApp() (string, error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to name of first application process whose frontmost is true`).Output()
	if err != nil {
		return "", fmt.Errorf("ask which application is frontmost: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Activate brings the emulator's existing window forward. Use it in place of
// Open when a window is already showing what you were about to launch.
func (e Emulator) Activate() error {
	if err := exec.Command("open", "-a", app).Run(); err != nil {
		return fmt.Errorf("bring %s forward: %w", app, err)
	}
	return nil
}

// Open launches a window running command and returns once it has started.
func (e Emulator) Open(command []string) error {
	binary, err := e.binary()
	if err != nil {
		return err
	}

	args := []string{}
	if e.Width > 0 {
		args = append(args, "--window-width="+strconv.Itoa(e.Width))
	}
	if e.Height > 0 {
		args = append(args, "--window-height="+strconv.Itoa(e.Height))
	}
	args = append(args, "-e")
	args = append(args, command...)

	cmd := exec.Command(binary, args...)
	// The window must not inherit the caller's tmux client: with TMUX set,
	// the dock's attach is refused as a nested session.
	cmd.Env = withoutTmux(os.Environ())
	var complaint bytes.Buffer
	cmd.Stderr = &complaint
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", binary, err)
	}

	// Start only reports that the binary executed. An emulator that rejects
	// the invocation exits a moment later, and reporting that as a successful
	// launch would leave the caller believing in a window that never appeared
	// — so give it a moment to fail before declaring the window up.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("open %s: %w: %s", binary, err, strings.TrimSpace(complaint.String()))
		}
		return nil
	case <-time.After(settleTime):
		return nil
	}
}

// settleTime is how long an emulator gets to reject its invocation before the
// window counts as open.
const settleTime = 500 * time.Millisecond

func (e Emulator) binary() (string, error) {
	if e.Binary != "" {
		return e.Binary, nil
	}
	if _, err := os.Stat(bundleBinary); err == nil {
		return bundleBinary, nil
	}
	path, err := exec.LookPath("ghostty")
	if err != nil {
		return "", fmt.Errorf("Ghostty is not installed: looked in %s and on PATH", bundleBinary)
	}
	return path, nil
}

func withoutTmux(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "TMUX=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
