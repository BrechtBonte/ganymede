package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BrechtBonte/ganymede/internal/registry"
	"github.com/BrechtBonte/ganymede/internal/session"
)

// entry is one registry file's contents, in the shape Claude Code 2.1.220
// writes it (§11 of the spec). The registry is undocumented, so these literals
// are the harness's record of the schema it was built against.
type entry struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Name            string `json:"name"`
	NameSource      string `json:"nameSource"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	WaitingFor      string `json:"waitingFor,omitempty"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
}

// registryOf writes one file per entry, named after its pid as Claude Code
// does, and returns the directory.
func registryOf(t *testing.T, entries ...entry) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entries {
		body, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		write(t, filepath.Join(dir, strconv.Itoa(e.PID)+".json"), string(body))
	}
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// living is a registry that believes every process it is told about.
func living(dir string) registry.Registry {
	return registry.Registry{Dir: dir, Alive: func(int) bool { return true }}
}

func read(t *testing.T, r registry.Registry) []session.Session {
	t.Helper()
	sessions, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return sessions
}

// Every field the Dashboard draws comes from the registry file.
func TestReadsWhatTheRegistrySaysAboutASession(t *testing.T) {
	dir := registryOf(t, entry{
		PID:             72144,
		SessionID:       "11105884-3fd7-496b-a49e-833cea89a5c7",
		CWD:             "/Users/brechtbonte/Projects/BrechtBonte/ganymede",
		Name:            "ganymede-78",
		NameSource:      "derived",
		Kind:            "interactive",
		Status:          "busy",
		StatusUpdatedAt: 1786272362730,
	})

	sessions := read(t, living(dir))

	want := session.Session{
		PID:   72144,
		ID:    "11105884-3fd7-496b-a49e-833cea89a5c7",
		Dir:   "/Users/brechtbonte/Projects/BrechtBonte/ganymede",
		Name:  "ganymede-78",
		State: session.Working,
		Since: time.UnixMilli(1786272362730),
	}
	if len(sessions) != 1 {
		t.Fatalf("read %d Sessions, want 1: %+v", len(sessions), sessions)
	}
	if sessions[0] != want {
		t.Errorf("read\n\t%+v\nwant\n\t%+v", sessions[0], want)
	}
}

// The states the registry alone can tell apart. Ready is not among them: it is
// Idle plus the harness's own seen-tracking, which lives elsewhere.
func TestRegistryStatusBecomesASessionState(t *testing.T) {
	for _, c := range []struct {
		status string
		want   session.State
	}{
		{"busy", session.Working},
		{"waiting", session.Blocked},
		{"idle", session.Idle},
		{"shell", session.Shell},
		// A status from a Claude Code newer than this harness. Idle is the
		// state that claims the least about a Session we cannot read.
		{"transcendent", session.Idle},
	} {
		t.Run(c.status, func(t *testing.T) {
			dir := registryOf(t, entry{PID: 1, Status: c.status})
			if got := read(t, living(dir))[0].State; got != c.want {
				t.Errorf("status %q reads as %s, want %s", c.status, got, c.want)
			}
		})
	}
}

// A Blocked Session is always displayed with its reason, so the reason has to
// survive the read.
func TestBlockedSessionCarriesWhatItIsWaitingFor(t *testing.T) {
	dir := registryOf(t, entry{PID: 1, Status: "waiting", WaitingFor: "permission: Bash"})

	if got := read(t, living(dir))[0].Reason; got != "permission: Bash" {
		t.Errorf("Blocked reason = %q, want %q", got, "permission: Bash")
	}
}

// A dead pid means Gone whether or not the file is still there — a killed
// Session never gets to clean up after itself.
func TestSessionsWhoseProcessHasDiedAreGone(t *testing.T) {
	dir := registryOf(t,
		entry{PID: 100, Name: "alive", Status: "idle"},
		entry{PID: 200, Name: "killed", Status: "busy"},
	)
	r := registry.Registry{Dir: dir, Alive: func(pid int) bool { return pid == 100 }}

	sessions := read(t, r)

	if len(sessions) != 1 || sessions[0].Name != "alive" {
		t.Errorf("read %+v, want only the Session whose process is still running", sessions)
	}
}

// Claude Code writes these files while the harness is reading them. A file
// caught mid-write must not take the rest of the working set down with it.
func TestAFileItCannotParseCostsOnlyThatSession(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "readable", Status: "idle"})
	write(t, filepath.Join(dir, "200.json"), `{"pid":200,"cwd":"/half`)

	sessions := read(t, living(dir))

	if len(sessions) != 1 || sessions[0].Name != "readable" {
		t.Errorf("read %+v, want the Session whose file was whole", sessions)
	}
}

// Nothing in the registry directory, or no directory at all, is an empty
// working set — not an error the Dashboard has to render.
func TestAnAbsentRegistryIsAnEmptyWorkingSet(t *testing.T) {
	r := living(filepath.Join(t.TempDir(), "never-created"))

	if sessions := read(t, r); len(sessions) != 0 {
		t.Errorf("read %+v, want no Sessions", sessions)
	}
}

// Files the registry directory picks up that are not Sessions are none of the
// harness's business.
func TestIgnoresFilesThatAreNotSessionRecords(t *testing.T) {
	dir := registryOf(t, entry{PID: 100, Name: "session", Status: "idle"})
	write(t, filepath.Join(dir, "notes.txt"), "not a Session")
	if err := os.Mkdir(filepath.Join(dir, "300.json"), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	if sessions := read(t, living(dir)); len(sessions) != 1 {
		t.Errorf("read %+v, want only the one Session", sessions)
	}
}

// A record that does not say when the Session last moved gets no time at all,
// rather than the start of the Unix epoch — which reads as a Session that has
// been waiting on you for fifty years, to the ordering and to anything else
// weighing it against a clock.
func TestASessionWithNoTimestampHasNoTime(t *testing.T) {
	dir := registryOf(t, entry{PID: 1, Status: "waiting"})

	if got := read(t, living(dir))[0].Since; !got.IsZero() {
		t.Errorf("Since = %v, want no time at all", got)
	}
}
