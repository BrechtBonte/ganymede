package dashboard

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BrechtBonte/ganymede/internal/popup"
	"github.com/BrechtBonte/ganymede/internal/repo"
	"github.com/BrechtBonte/ganymede/internal/session"
	"github.com/BrechtBonte/ganymede/internal/ticket"
)

// row is one line of the repo tree: a repo's header, or one of its Sessions
// indented beneath it.
type row struct {
	// root is the Main root the row belongs to.
	root string
	// session is the Session the row draws, and nil on a repo's header row.
	session *session.Session
	// ticket is what the Session is about, and empty when it is about none.
	ticket ticket.Key
	// state is what the Main root is doing, on a repo's header row.
	state repo.State
	// holderWorking says the Session actually holding this root — as
	// against every Session merely grouped under the repo — is Working, on
	// a repo's header row. It is narrower than state == InUse, which fires
	// for a holder in any state (state.go): this is what says whether the
	// header row's own mark has anything worth animating.
	holderWorking bool
	// claimNote is the note the root was claimed with, on a repo's header row
	// whose state is Claimed — empty otherwise (including the collision
	// state.go documents, where a live occupant outranks an underlying
	// Claim: the state reads InUse, and this stays empty with it), and empty
	// for a Claim made with none.
	claimNote string
	// caution is what the Main root's own checkout is carrying, on a repo's
	// header row. It is drawn there whatever state the root is in: the two
	// answer different questions, and both of them are asked before a PR is
	// checked out.
	caution repo.Caution
	// cautionKnown says git has actually been asked about this root at
	// least once — as against a root nothing has asked about yet, which
	// reads as the same zero Caution a genuinely clean root does. The
	// release nudge (dashboard.go) reads this rather than caution alone, so
	// a root's git status still in flight never gets called clean by
	// default.
	cautionKnown bool
	// popup is what the row's own hidden Popup shell is doing, keyed by the
	// same directory a popup opened over this row would open in: a Session's
	// own on a Session row, the Main root on a repo's header row. A busy one
	// is what earns the row its marker (§8).
	popup popup.Status
	// frozen says the Session's own pane is holding a mode over the live
	// view, so what that pane shows is a held picture of the Session rather
	// than the Session itself. It is orthogonal to the Session's State — a
	// Frozen pane sits over one in any state, which is exactly when it reads
	// as a hang — and it is never set on a repo's header row, which has no
	// pane of its own to hold anything.
	frozen bool
	// holdsRoot says a Session row's own Session is the one actually holding
	// its repo's Main root as its checkout — as against every Session merely
	// grouped under the repo, a Worktree session included. It is what a
	// Takeover reads to find the root's sole occupant (claim.go).
	holdsRoot bool
}

// answers is what laying the tree out has to ask about a directory or a root.
// They are gathered here so that a row is built out of answers rather than out
// of a handful of arguments, and so that the Dashboard can be the one
// remembering them.
type answers struct {
	// root is the Main root a Session's directory belongs to.
	root func(dir string) string
	// checkout is the checkout a Session has its hands on, which is what
	// separates a Session holding a Main root from one working in a worktree
	// inside it.
	checkout func(dir string) string
	// ticket is what a Session's checkout is about.
	ticket func(dir, root string) ticket.Key
	// caution is what a Main root's checkout is carrying, and whether git
	// has actually been asked yet.
	caution func(root string) (repo.Caution, bool)
	// popup is what a directory's hidden Popup shell is doing.
	popup func(dir string) popup.Status
	// frozen is whether a Session's own pane is holding a mode over the live
	// view. It is asked by Session id rather than by directory: the pane
	// belongs to the Session, and two Sessions can share a checkout.
	frozen func(id string) bool
	// claimed is the note a Main root was claimed with, and whether it is
	// claimed at all.
	claimed func(root string) (string, bool)
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

// rowsOf lays the working set out as the repo tree: a header row per repo in
// working, with every Session indented under the Main root ask.root gives its
// directory — a Worktree session under the repo it came from, and a Session
// outside every scan root under its own directory, because the registry's cwd
// is ground truth. Repos are grouped by root rather than by name, so two
// checkouts that happen to share a name stay apart. Each Session row carries
// the ticket it is about, and each repo's header row the state of its Main root
// and what that checkout is carrying.
//
// A repo in the working set with nothing running in it still gets its header:
// that is the difference between the Dashboard and a list of live Sessions —
// it shows where you are working, and you are not always mid-turn. A Session
// in a repo the working set left out keeps its row all the same, since a
// Session nothing on the Dashboard mentions is worse than a repo too many.
func rowsOf(sessions []session.Session, working []string, ask answers) []row {
	byRoot := map[string][]session.Session{}
	for _, s := range sessions {
		root := ask.root(s.Dir)
		byRoot[root] = append(byRoot[root], s)
	}
	for _, root := range working {
		if _, drawn := byRoot[root]; !drawn {
			byRoot[root] = nil
		}
	}

	roots := make([]string, 0, len(byRoot))
	for root, group := range byRoot {
		slices.SortFunc(group, moreUrgent)
		roots = append(roots, root)
	}
	slices.SortFunc(roots, func(a, b string) int {
		if d := louder(byRoot[a], byRoot[b]); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})

	rows := make([]row, 0, len(sessions)+len(roots))
	for _, root := range roots {
		note, claimed := ask.claimed(root)
		state := stateOf(root, byRoot[root], ask, claimed)
		if state != repo.Claimed {
			// A live occupant outranks a Claim on the state a row draws
			// (state.go): the note underneath stays with the harness, and
			// never rides along on a row whose own state says it is not
			// Claimed.
			note = ""
		}
		caution, cautionKnown := ask.caution(root)
		rows = append(rows, row{
			root: root, state: state, claimNote: note,
			caution: caution, cautionKnown: cautionKnown, popup: ask.popup(root),
			holderWorking: holderWorking(root, byRoot[root], ask),
		})
		for i := range byRoot[root] {
			running := &byRoot[root][i]
			rows = append(rows, row{
				root: root, session: running, ticket: ask.ticket(running.Dir, root), popup: ask.popup(running.Dir),
				holdsRoot: ask.checkout(running.Dir) == root, frozen: ask.frozen(running.ID),
			})
		}
	}
	return rows
}

// stateOf is what the Main root root is doing, given the Sessions grouped
// under it and whether you have claimed it. The Sessions are the live ones by
// construction — what the registry, the hooks and the cross-check between
// them say is running — so whether any of them is holding the root is the
// first question. A repo on the rail with nothing running in it and no Claim
// on it is Free, which is what a repo you were working in yesterday should
// say.
func stateOf(root string, group []session.Session, ask answers, claimed bool) repo.State {
	working := make([]string, 0, len(group))
	for _, s := range group {
		working = append(working, ask.checkout(s.Dir))
	}
	return repo.StateOf(root, working, claimed)
}

// holderWorking says the Session actually holding root, if any, is Working —
// not merely present, which is all state == InUse promises.
func holderWorking(root string, group []session.Session, ask answers) bool {
	for _, s := range group {
		if ask.checkout(s.Dir) == root {
			return s.State == session.Working
		}
	}
	return false
}

// louder orders two repos by what each is asking of you: a repo is as urgent
// as the most urgent Session in it, and a repo with nothing running in it is
// quieter than any of them. It is on the Dashboard because you were there, not
// because it wants something.
func louder(a, b []session.Session) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	return moreUrgent(a[0], b[0])
}

// tier ranks a Session by how much it is asking of you. Blocked outranks
// Ready: one cannot continue at all, the other is only waiting to be read.
func tier(s session.Session) int {
	switch s.State {
	case session.Blocked:
		return 0
	case session.Ready:
		return 1
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
		if a.Attention() {
			return d
		}
		return -d
	}
	return strings.Compare(a.Name, b.Name)
}
