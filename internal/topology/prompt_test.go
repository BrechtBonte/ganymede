package topology_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/BrechtBonte/ganymede/internal/topology"
)

// promptable is a Harness on a throwaway server, the same shape guardable
// uses: the guard only ever touches the one pane it is pointed at.
func promptable(t *testing.T) topology.Harness {
	t.Helper()
	repo := initRepo(t, filepath.Join(t.TempDir(), "service-billing"))
	return testHarness(t, repo)
}

// Two Sessions guarded-ending at once must never share so much as a byte:
// tmux's unnamed set-buffer/paste-buffer pair is one global slot per server,
// and every Session here shares the one server a Harness carries, so an End
// racing another's paste-buffer call — a real shape, since concurrent
// Takeovers each fire their own End off the main loop as a goroutine — is
// exactly what a shared buffer would garble.
func TestConcurrentSendsNeverCrossPanes(t *testing.T) {
	h := promptable(t)
	pidA, textlogA := exitPane(t, h, true, "a-")
	pidB, textlogB := exitPane(t, h, true, "b-")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = h.End(pidA)
	}()
	go func() {
		defer wg.Done()
		errs[1] = h.End(pidB)
	}()
	wg.Wait()

	if errs[0] != nil {
		t.Errorf("End on A: %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("End on B: %v", errs[1])
	}
	if got := readKeylog(t, textlogA); got != "/exit" {
		t.Errorf("session A's pane received %q, want its own /exit", got)
	}
	if got := readKeylog(t, textlogB); got != "/exit" {
		t.Errorf("session B's pane received %q, want its own /exit", got)
	}
}
