package ghostty_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/ghostty"
)

// recordingEmulator stands in for Ghostty: it writes down how it was invoked.
func recordingEmulator(t *testing.T) (ghostty.Emulator, func() (argv, env string)) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "invocation")
	binary := filepath.Join(dir, "fake-ghostty")
	script := "#!/bin/sh\necho \"$@\" > " + log + "\nenv >> " + log + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Open does not wait for the window, so neither can the recording.
	return ghostty.Emulator{Binary: binary, Width: 200, Height: 50}, func() (string, string) {
		deadline := time.Now().Add(5 * time.Second)
		for {
			body, err := os.ReadFile(log)
			if err == nil {
				argv, env, _ := strings.Cut(string(body), "\n")
				return argv, env
			}
			if time.Now().After(deadline) {
				t.Fatalf("the emulator was never invoked: %v", err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// The window has to run the harness's attach command.
func TestOpenRunsTheGivenCommandInTheWindow(t *testing.T) {
	emulator, invocation := recordingEmulator(t)

	if err := emulator.Open([]string{"tmux", "-L", "ganymede-dock", "attach", "-t", "=dock"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	argv, _ := invocation()
	if !strings.Contains(argv, "-e tmux -L ganymede-dock attach -t =dock") {
		t.Errorf("emulator argv does not run the attach command: %q", argv)
	}
}

// Launching from inside a tmux session must not hand that session's TMUX down
// to the new window: the dock's client would refuse to attach.
func TestOpenDoesNotPassTheCallersTmuxDown(t *testing.T) {
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,123,0")
	emulator, invocation := recordingEmulator(t)

	if err := emulator.Open([]string{"true"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, env := invocation()
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, "TMUX=") {
			t.Errorf("the window inherited %q", line)
		}
	}
}

// A window that never appears must not be reported as a successful launch.
func TestOpenReportsAnEmulatorThatFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "failing-ghostty")
	script := "#!/bin/sh\necho 'unknown option --window-width' >&2\nexit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	err := ghostty.Emulator{Binary: binary, Width: 200, Height: 50}.Open([]string{"true"})

	if err == nil {
		t.Fatal("Open reported success for an emulator that exited 1 without opening a window")
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Errorf("Open lost the emulator's diagnostic: %v", err)
	}
}

// A window that stays up is a success, and Open must not block on it.
func TestOpenDoesNotWaitForTheWindowToClose(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "lingering-ghostty")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := (ghostty.Emulator{Binary: binary}).Open([]string{"true"}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Open blocked for %s; it should return once the window is up", elapsed)
	}
}
