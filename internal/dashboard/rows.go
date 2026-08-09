package dashboard

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/session"
)

// row is one line of the repo tree: a repo's header, or one of its Sessions
// indented beneath it.
type row struct {
	// root is the Main root the row belongs to.
	root string
	// session is the Session the row draws, and nil on a repo's header row.
	session *session.Session
}

// label is what the row is called.
func (r row) label() string {
	if r.session == nil {
		return filepath.Base(r.root)
	}
	return r.session.Name
}

// key identifies a row across redraws, so the selection stays on what you put
// it on rather than on whichever row lands in that position next.
//
// A Session is keyed by its process, not by the registry's session id: the pid
// is the one field the harness has checked — it names a process that is alive,
// and no two live Sessions share one — while the id comes from an undocumented
// file this package has promised to survive the shape of.
func (r row) key() string {
	if r.session == nil {
		return "repo\x00" + r.root
	}
	return "session\x00" + strconv.Itoa(r.session.PID)
}

// rowsOf lays the working set out as the repo tree: every Session under the
// Main root rootOf gives its directory — a Worktree session under the repo it
// came from, and a Session outside every scan root under its own directory,
// because the registry's cwd is ground truth. Repos are grouped by root rather
// than by name, so two checkouts that happen to share a name stay apart.
func rowsOf(sessions []session.Session, rootOf func(dir string) string) []row {
	byRoot := map[string][]session.Session{}
	for _, s := range sessions {
		root := rootOf(s.Dir)
		byRoot[root] = append(byRoot[root], s)
	}

	roots := make([]string, 0, len(byRoot))
	for root, group := range byRoot {
		slices.SortFunc(group, moreUrgent)
		roots = append(roots, root)
	}
	slices.SortFunc(roots, func(a, b string) int {
		// A repo is as urgent as the most urgent Session in it.
		if d := moreUrgent(byRoot[a][0], byRoot[b][0]); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})

	rows := make([]row, 0, len(sessions)+len(roots))
	for _, root := range roots {
		rows = append(rows, row{root: root})
		for i := range byRoot[root] {
			rows = append(rows, row{root: root, session: &byRoot[root][i]})
		}
	}
	return rows
}

// tier ranks a Session by how much it is asking of you.
func tier(s session.Session) int {
	switch s.State {
	case session.Blocked:
		return 0
	// 1 is Ready's, once the harness tracks which turns you have seen.
	case session.Working:
		return 2
	case session.Shell:
		return 3
	default:
		return 4
	}
}

// moreUrgent orders Sessions the way the tree reads: Attention at the top and,
// within a tier, the Session that has been waiting on you longest. A tier that
// is asking nothing of you reads the other way round — most recently moved
// first, which is where you were last.
func moreUrgent(a, b session.Session) int {
	if d := tier(a) - tier(b); d != 0 {
		return d
	}
	if d := a.Since.Compare(b.Since); d != 0 {
		if a.State == session.Blocked {
			return d
		}
		return -d
	}
	return strings.Compare(a.Name, b.Name)
}
