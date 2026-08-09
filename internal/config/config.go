// Package config locates the files the harness keeps, and replaces files it
// does not own without ever leaving one half-written.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// Home is the XDG config directory. Go's os.UserConfigDir points at
// ~/Library/Application Support on macOS, which is not where tmux or the
// harness keep their configuration.
func Home(home string) string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".config")
}

// Dir is the harness's own directory: its tmux fragments, the dock's config,
// and the socket the hooks report to.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(Home(home), "ganymede"), nil
}

// Replace writes body to path atomically, so an interrupted install cannot
// leave the user with half a config file.
//
// A file that already says exactly this is left where it is. Installing is
// meant to be safe to repeat, and rewriting a file changes it as far as
// everything watching it is concerned — Claude Code reads its settings back
// when they are touched, and `ganymede up` touches them every time it runs.
func Replace(path string, body []byte) error {
	// The file being replaced may be a symlink into somebody's dotfiles. It is
	// the file at the end of the link that is meant to be rewritten: renaming
	// over the link itself would leave the dotfiles copy in place and no
	// longer connected to anything.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, body) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create the directory for %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".ganymede-*")
	if err != nil {
		return fmt.Errorf("create temporary file beside %s: %w", path, err)
	}
	defer os.Remove(temp.Name())

	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return fmt.Errorf("write %s: %w", temp.Name(), err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temp.Name(), err)
	}
	// A file that already existed keeps the permissions it had. Claude Code's
	// settings can hold an API key, and a harness that quietly widened them to
	// everyone's default would be handing that key to every account on the
	// machine.
	if err := os.Chmod(temp.Name(), permissions(path)); err != nil {
		return fmt.Errorf("set permissions on %s: %w", temp.Name(), err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// permissions are the ones path should end up with: whatever it already had,
// or the ordinary ones for a config file the harness is creating.
func permissions(path string) os.FileMode {
	if existing, err := os.Stat(path); err == nil {
		return existing.Mode().Perm()
	}
	return 0o644
}
