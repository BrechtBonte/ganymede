package browser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/browser"
)

// records is a stand-in for the desktop's open command, writing down the link
// it was handed instead of opening it.
func records(t *testing.T) (browser.Browser, func() string) {
	t.Helper()
	dir := t.TempDir()
	opened := filepath.Join(dir, "opened")
	binary := filepath.Join(dir, "open")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + opened + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return browser.Browser{Binary: binary}, func() string {
		body, err := os.ReadFile(opened)
		if err != nil {
			return ""
		}
		return string(body)
	}
}

// The ticket link is the whole of what the harness does with JIRA, so handing
// one to the desktop has to be the whole of what this does.
func TestOpenHandsTheLinkToTheDesktop(t *testing.T) {
	open, opened := records(t)

	if err := open.Open("https://teamleader.atlassian.net/browse/FIRE-2841"); err != nil {
		t.Fatalf("open the link: %v", err)
	}
	if got, want := opened(), "https://teamleader.atlassian.net/browse/FIRE-2841"; got != want {
		t.Errorf("opened %q, want %q", got, want)
	}
}

// A browser that would not open is worth a word: it is one of the two things
// the `o` key does, and it happened because you asked for it.
func TestOpenSaysWhenItCouldNotOpenAnything(t *testing.T) {
	missing := browser.Browser{Binary: filepath.Join(t.TempDir(), "not-installed")}

	err := missing.Open("https://teamleader.atlassian.net/browse/FIRE-2841")
	if err == nil {
		t.Fatal("opened a link with a command that is not there, want an error")
	}
	if !strings.Contains(err.Error(), "FIRE-2841") {
		t.Errorf("error = %q, want it to say which link could not be opened", err)
	}
}
