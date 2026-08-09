package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/config"
)

// sidecar is a harness state file in a directory of the test's own.
func sidecar(t *testing.T) config.Sidecar {
	t.Helper()
	return config.Sidecar{Path: filepath.Join(t.TempDir(), "ganymede", "state.json")}
}

// The point of the sidecar: what the harness was told last time is still there
// next time.
func TestSidecarKeepsASectionBetweenRuns(t *testing.T) {
	state := sidecar(t)
	if err := state.Write("tickets", []string{"FIRE-2841"}); err != nil {
		t.Fatalf("write the section: %v", err)
	}

	// A second reader, standing in for the next run of the harness.
	reopened := config.Sidecar{Path: state.Path}
	var read []string
	if err := reopened.Read("tickets", &read); err != nil {
		t.Fatalf("read the section back: %v", err)
	}
	if len(read) != 1 || read[0] != "FIRE-2841" {
		t.Errorf("read back %q, want the section that was written", read)
	}
}

// One file, several parts of the harness. A section written by one of them must
// not cost the others theirs — including sections written by a version of the
// harness this one has never heard of, which is the whole reason the file is
// rewritten key by key rather than as a shape somebody declared.
func TestSidecarLeavesEveryOtherSectionAlone(t *testing.T) {
	state := sidecar(t)
	if err := os.MkdirAll(filepath.Dir(state.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Path, []byte(`{"claims":{"/repos/billing":"reviewing #21"},"popups":[7]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := state.Write("tickets", map[string]string{"main": "FIRE-2841"}); err != nil {
		t.Fatalf("write the section: %v", err)
	}

	var claims map[string]string
	if err := state.Read("claims", &claims); err != nil {
		t.Fatalf("read the other section: %v", err)
	}
	if claims["/repos/billing"] != "reviewing #21" {
		t.Errorf("claims = %v, want the section that was already there", claims)
	}
	body, err := os.ReadFile(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"popups"`) {
		t.Errorf("state file = %s, want the section this harness does not read still in it", body)
	}
}

// A harness starting for the first time has no state file, and a part of it
// asking for a section nobody has written yet is not an error — it is the
// ordinary first run, and it has to leave the caller with nothing rather than
// with a complaint.
func TestSidecarWithNothingInItReadsAsNothing(t *testing.T) {
	state := sidecar(t)

	kept := map[string]string{"untouched": "yes"}
	if err := state.Read("tickets", &kept); err != nil {
		t.Fatalf("read from a state file that is not there: %v", err)
	}
	if kept["untouched"] != "yes" {
		t.Errorf("kept = %v, want it left as it was", kept)
	}

	if err := state.Write("claims", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if err := state.Read("tickets", &kept); err != nil {
		t.Fatalf("read a section nobody has written: %v", err)
	}
	if kept["untouched"] != "yes" {
		t.Errorf("kept = %v, want it left as it was", kept)
	}
}

// A state file somebody has been editing by hand, or a half-written one from a
// disk that filled up, is worth saying out loud: it is the harness's own file,
// and quietly starting from nothing would throw away everything in it.
func TestSidecarThatWillNotParseSaysSo(t *testing.T) {
	state := sidecar(t)
	if err := os.MkdirAll(filepath.Dir(state.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Path, []byte(`{"tickets": [`), 0o644); err != nil {
		t.Fatal(err)
	}

	var read []string
	if err := state.Read("tickets", &read); err == nil {
		t.Error("read a state file that will not parse without complaint, want an error")
	}
}
