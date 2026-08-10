package notifier_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/notifier"
)

// recordingBinary stands in for terminal-notifier: it writes down its argv.
func recordingBinary(t *testing.T) (binary string, invocation func() string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "invocation")
	binary = filepath.Join(dir, "fake-terminal-notifier")
	script := "#!/bin/sh\necho \"$@\" > " + log + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, func() string {
		body, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("terminal-notifier was never invoked: %v", err)
		}
		return strings.TrimSpace(string(body))
	}
}

func TestSendPassesTitleAndMessage(t *testing.T) {
	binary, invocation := recordingBinary(t)
	sender := notifier.TerminalNotifier{Binary: binary}

	if err := sender.Send(notifier.Notification{Title: "service-billing · FIRE-2841", Body: "permission: Bash"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	argv := invocation()
	if !strings.Contains(argv, "-title service-billing · FIRE-2841") {
		t.Errorf("argv does not carry the title: %q", argv)
	}
	if !strings.Contains(argv, "-message permission: Bash") {
		t.Errorf("argv does not carry the body: %q", argv)
	}
}

// Sound is on Blocked only — a Ready escalation must never ask for it.
func TestSendSoundsOnlyWhenAsked(t *testing.T) {
	binary, invocation := recordingBinary(t)
	sender := notifier.TerminalNotifier{Binary: binary}

	if err := sender.Send(notifier.Notification{Title: "t", Body: "b", Sound: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if argv := invocation(); !strings.Contains(argv, "-sound Ping") {
		t.Errorf("a sounding Notification did not ask for a sound: %q", argv)
	}
}

func TestSendLeavesOutSoundWhenNotAsked(t *testing.T) {
	binary, invocation := recordingBinary(t)
	sender := notifier.TerminalNotifier{Binary: binary}

	if err := sender.Send(notifier.Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if argv := invocation(); strings.Contains(argv, "-sound") {
		t.Errorf("a silent Notification asked for a sound: %q", argv)
	}
}

// Clicking has to run a command, not merely activate an app — the Dashboard
// has to jump to a specific Session, which -activate alone cannot do.
func TestSendCarriesTheClickCommand(t *testing.T) {
	binary, invocation := recordingBinary(t)
	sender := notifier.TerminalNotifier{Binary: binary}

	err := sender.Send(notifier.Notification{
		Title: "t", Body: "b",
		Click: []string{"/opt/ganymede", "notify-click", "4242"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if argv := invocation(); !strings.Contains(argv, "-execute '/opt/ganymede' 'notify-click' '4242'") {
		t.Errorf("argv does not run the click command: %q", argv)
	}
}

// A Notification with nothing to click leaves the flag out entirely, rather
// than an -execute with nothing after it.
func TestSendLeavesOutExecuteWithNoClickCommand(t *testing.T) {
	binary, invocation := recordingBinary(t)
	sender := notifier.TerminalNotifier{Binary: binary}

	if err := sender.Send(notifier.Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if argv := invocation(); strings.Contains(argv, "-execute") {
		t.Errorf("a Notification with no Click asked for -execute: %q", argv)
	}
}

func TestSendReportsAFailingBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "failing-terminal-notifier")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'not authorized' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sender := notifier.TerminalNotifier{Binary: binary}

	err := sender.Send(notifier.Notification{Title: "t", Body: "b"})

	if err == nil {
		t.Fatal("Send reported success for a binary that exited 1")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("Send lost the binary's diagnostic: %v", err)
	}
}
