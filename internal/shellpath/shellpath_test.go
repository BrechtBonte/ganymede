package shellpath_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/shellpath"
)

// fakeShell writes an executable standing in for your login shell: it runs
// what it is asked to run under body's PATH, after whatever chatter an
// interactive startup file might print.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "fakesh")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}
	return shell
}

// The PATH that matters is the one your terminal has, and that one is often
// set in .zshrc — which only an interactive shell reads.
func TestLoginReadsThePathAnInteractiveLoginShellEndsUpWith(t *testing.T) {
	shell := fakeShell(t, `case "$1" in *i*l*|*l*i*) ;; *) exit 3 ;; esac
PATH=/home/you/.local/bin:/usr/bin:/bin exec /bin/sh -c "$2"`)

	got, err := shellpath.Login(shell, time.Second)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got != "/home/you/.local/bin:/usr/bin:/bin" {
		t.Errorf("Login = %q, want the shell's own PATH", got)
	}
}

// Startup files print banners, greetings and update nags to stdout, and none
// of that is a PATH.
func TestLoginIgnoresWhatTheStartupFilesPrint(t *testing.T) {
	shell := fakeShell(t, `echo "Welcome back"
echo "oh-my-zsh: update available"
PATH=/home/you/.local/bin:/usr/bin exec /bin/sh -c "$2"`)

	got, err := shellpath.Login(shell, time.Second)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got != "/home/you/.local/bin:/usr/bin" {
		t.Errorf("Login = %q, want the PATH without the chatter around it", got)
	}
}

// A startup file that waits on input it will never get must not hold the
// harness up for good.
func TestLoginGivesUpOnAShellThatNeverAnswers(t *testing.T) {
	shell := fakeShell(t, `sleep 5`)

	start := time.Now()
	if _, err := shellpath.Login(shell, 100*time.Millisecond); err == nil {
		t.Fatalf("Login answered for a shell that never printed a PATH")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("Login waited %v, want it to give up at its timeout", waited)
	}
}

func TestLoginFailsForAShellThatPrintsNoPath(t *testing.T) {
	shell := fakeShell(t, `echo "no path here"`)

	if got, err := shellpath.Login(shell, time.Second); err == nil {
		t.Errorf("Login = %q, want an error when the shell never printed a PATH", got)
	}
}

func TestLoginFailsWithNoShell(t *testing.T) {
	if _, err := shellpath.Login("", time.Second); err == nil || !strings.Contains(err.Error(), "SHELL") {
		t.Errorf("Login(\"\") = %v, want an error naming SHELL", err)
	}
}
