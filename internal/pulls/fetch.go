package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout is how long one pass may take. Measured at 4.4–5.6 s for 17
// PRs carrying every field, against a gateway that gave up at about ten on a
// ten-repo query — so this is generous next to the real thing and short next
// to the thirty minutes until the next cycle.
const defaultTimeout = 30 * time.Second

// defaultFirst is the page size. Seventeen was the measured census and twenty
// is the next round number above it; a set that outgrows this loses its tail
// rather than its section.
const defaultFirst = 20

// reviewQuery and mineQuery are the two searches, as GitHub's own qualifiers.
//
// review-requested rather than user-review-requested, which inverts the obvious
// guess: measured, the broad qualifier returns 8 — three human requests and
// five dependabot — where the narrow one returns 5, all five dependabot. Every
// human review request on this account arrives as a team request, so the
// narrower-looking qualifier builds a section that hides every person asking
// you for something.
//
// There is no assignee:@me. It returns 7 PRs and they are the same 7 author:@me
// returns, every one authored by you: GitHub's PR assignee carries no
// independent meaning in this workflow.
const (
	reviewQuery = "is:pr is:open review-requested:@me"
	mineQuery   = "is:pr is:open author:@me"
)

// fields is what one PR is read for, shared by both passes so the two can
// never drift into disagreeing about what a row holds.
//
// mergeStateStatus is the expensive one and is kept: it is the only source for
// Behind and the only exact source for Landable, and at this section's scale
// the whole query with it included is under six seconds. defaultBranchRef is
// what withholds Landable from a stacked Pull. headRefName is free — measured
// cost: 1 unchanged — and pays for the stack parent and the Session match.
const fields = `
  number title url isDraft baseRefName headRefName updatedAt
  mergeable mergeStateStatus reviewDecision
  author { login }
  repository { nameWithOwner defaultBranchRef { name } }
  statusCheckRollup { state }`

// searchQuery carries both lists as aliased search fields, which is what makes
// a whole pass one HTTP request for one rate-limit point.
//
// rateLimit is asked for here and in the second pass, and nothing decodes it.
// It is what proves the cost claim — 1 point a pass, 4 an hour against 5,000 —
// and a query that stops asking is one nobody can check.
const searchQuery = `query($review: String!, $mine: String!, $first: Int!) {
  rateLimit { cost remaining }
  review: search(query: $review, type: ISSUE, first: $first) {
    issueCount
    nodes { ... on PullRequest {` + fields + ` } }
  }
  mine: search(query: $mine, type: ISSUE, first: $first) {
    issueCount
    nodes { ... on PullRequest {` + fields + ` } }
  }
}`

// Trouble is why a fetch did not land. The three are told apart because they
// want three different renderings and two different clocks, and gh already
// diagnoses all three — which is the argument for running it rather than
// holding a token of our own.
type Trouble int

const (
	// NotLoggedIn is gh exit 4, and a gh that is not installed at all: both
	// end with you running `gh auth login`, or installing gh first.
	NotLoggedIn Trouble = iota
	// Unauthorized is exit 1 carrying HTTP 401 — a token expired or revoked.
	Unauthorized
	// Unreachable is everything else, which is the network.
	Unreachable
)

// Error is a fetch that did not land, carrying which of the three it was and
// whatever gh had to say for itself.
type Error struct {
	Trouble Trouble
	// Said is gh's stderr, trimmed. It is kept for a diagnostic rather than
	// for the section, which says its own four things in its own words.
	Said string
	Err  error
}

func (e *Error) Error() string {
	if e.Said != "" {
		return fmt.Sprintf("read your pull requests: %v: %s", e.Err, e.Said)
	}
	return fmt.Sprintf("read your pull requests: %v", e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// TroubleOf is which of the three an error is, and whether it is one of them
// at all.
func TroubleOf(err error) (Trouble, bool) {
	var trouble *Error
	if errors.As(err, &trouble) {
		return trouble.Trouble, true
	}
	return 0, false
}

// Fetcher runs one pass of the census through gh.
//
// gh is a subprocess rather than an HTTP client of our own, and that is a
// decision about failure modes rather than about convenience: the three
// outcomes above are gh's own diagnosis, and a raw 401 from a client we wrote
// cannot tell a revoked token from a changed scope. It is also why the harness
// holds no credential at all — it inherits the login you have already granted.
type Fetcher struct {
	// GH is the gh binary to run. Empty means the gh on PATH.
	GH string
	// Timeout is how long one pass may take. Zero means the default.
	Timeout time.Duration
	// First is the page size for each list. Zero means the default.
	First int
}

// Fetch reads both lists in one request.
func (f Fetcher) Fetch(ctx context.Context) (Set, error) {
	var answer struct {
		Data struct {
			Review searchResult `json:"review"`
			Mine   searchResult `json:"mine"`
		} `json:"data"`
	}
	err := f.ask(ctx, &answer,
		"-f", "query="+searchQuery,
		"-f", "review="+reviewQuery,
		"-f", "mine="+mineQuery,
		"-F", "first="+strconv.Itoa(f.first()),
	)
	if err != nil {
		return nil, err
	}

	set := make(Set, 0, len(answer.Data.Mine.Nodes)+len(answer.Data.Review.Nodes))
	for _, node := range answer.Data.Mine.Nodes {
		set = append(set, node.pull(Authored))
	}
	for _, node := range answer.Data.Review.Nodes {
		set = append(set, node.pull(Requested))
	}
	return set, nil
}

// ask runs gh and decodes what it wrote, turning every way of not getting an
// answer into one of the three troubles.
func (f Fetcher) ask(ctx context.Context, into any, args ...string) error {
	ctx, giveUp := context.WithTimeout(ctx, f.timeout())
	defer giveUp()

	var out, said strings.Builder
	cmd := exec.CommandContext(ctx, f.gh(), append([]string{"api", "graphql"}, args...)...)
	cmd.Stdout = &out
	cmd.Stderr = &said
	err := cmd.Run()
	complaint := strings.TrimSpace(said.String())
	if err != nil {
		return &Error{Trouble: troubleOf(err, complaint), Said: complaint, Err: err}
	}
	if err := json.Unmarshal([]byte(out.String()), into); err != nil {
		// An answer that is not JSON is a gateway or a proxy talking, not
		// GitHub: the same thing a dropped connection is, and the same
		// rendering.
		return &Error{Trouble: Unreachable, Said: complaint, Err: err}
	}
	return nil
}

// troubleOf reads gh's own diagnosis. Exit 4 is the login never made; exit 1
// carrying a 401 is a token that has expired or been revoked; everything else
// is the network.
//
// A gh that could not be started at all lands on NotLoggedIn rather than on
// Unreachable, because the body that names `gh auth login` is the one that
// sends a reader somewhere useful — installing gh is the same errand. It is
// read two ways because gh arrives two ways: a bare name off PATH fails in
// LookPath with an *exec.Error, and a path spelled out fails in the exec
// itself with the filesystem's own complaint.
func troubleOf(err error, said string) Trouble {
	var started *exec.Error
	if errors.As(err, &started) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return NotLoggedIn
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		if exited.ExitCode() == 4 {
			return NotLoggedIn
		}
		if strings.Contains(said, "HTTP 401") {
			return Unauthorized
		}
	}
	return Unreachable
}

func (f Fetcher) gh() string {
	if f.GH == "" {
		return "gh"
	}
	return f.GH
}

func (f Fetcher) timeout() time.Duration {
	if f.Timeout <= 0 {
		return defaultTimeout
	}
	return f.Timeout
}

func (f Fetcher) first() int {
	if f.First <= 0 {
		return defaultFirst
	}
	return f.First
}

// searchResult is one aliased search field.
type searchResult struct {
	IssueCount int    `json:"issueCount"`
	Nodes      []node `json:"nodes"`
}

// node is one PullRequest as the query asks for it. The nested shapes are
// pointers because GitHub sends null for every one of them in some real case —
// a PR with no checks, a repository whose default branch the token cannot see,
// a ghost author.
type node struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	IsDraft    bool      `json:"isDraft"`
	Base       string    `json:"baseRefName"`
	Head       string    `json:"headRefName"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Mergeable  string    `json:"mergeable"`
	MergeState string    `json:"mergeStateStatus"`
	Review     string    `json:"reviewDecision"`
	Author     *struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
		DefaultBranch *struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	} `json:"repository"`
	Rollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (n node) pull(list List) Pull {
	p := Pull{
		List: list, Number: n.Number, Title: n.Title, URL: n.URL,
		IsDraft: n.IsDraft, Base: n.Base, Head: n.Head, UpdatedAt: n.UpdatedAt,
		Mergeable: n.Mergeable, MergeState: n.MergeState, Review: n.Review,
	}
	if n.Author != nil {
		p.Author = n.Author.Login
	}
	if n.Repository != nil {
		p.Repo = n.Repository.NameWithOwner
		if n.Repository.DefaultBranch != nil {
			p.Default = n.Repository.DefaultBranch.Name
		}
	}
	if n.Rollup != nil {
		p.Checks = n.Rollup.State
	}
	return p
}

// Reread asks about exactly the rows it is given, and nothing else.
//
// GitHub computes mergeability on a background test-merge commit and serves
// UNKNOWN until it lands. The commit is invalidated whenever the base branch
// moves, so a thirty-minute poll arrives cold every time — measured, one active
// repository went from 0/30 UNKNOWN to 14/30 in twenty minutes, and a re-read
// five seconds later resolved 30/30.
//
// It is one aliased request over the named rows rather than the search run
// again: the search would re-read every row to correct a handful, and would
// also let the set change shape between the two passes of one cycle.
//
// The rows come back in the order they were given, carrying the list they
// arrived in — the second pass reads repositories, which do not know which
// search found a row.
func (f Fetcher) Reread(ctx context.Context, rows []Pull) ([]Pull, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	var query strings.Builder
	query.WriteString("query {\n  rateLimit { cost remaining }\n")
	for i, p := range rows {
		owner, name, ok := strings.Cut(p.Repo, "/")
		if !ok {
			continue
		}
		fmt.Fprintf(&query, "  p%d: repository(owner: %q, name: %q) { pullRequest(number: %d) {%s } }\n",
			i, owner, name, p.Number, fields)
	}
	query.WriteString("}")

	var answer struct {
		Data map[string]struct {
			PullRequest *node `json:"pullRequest"`
		} `json:"data"`
	}
	if err := f.ask(ctx, &answer, "-f", "query="+query.String()); err != nil {
		return nil, err
	}

	read := make([]Pull, len(rows))
	for i, p := range rows {
		read[i] = p
		found, ok := answer.Data["p"+strconv.Itoa(i)]
		if !ok || found.PullRequest == nil {
			// A row the re-read could not see keeps what the first pass said
			// about it, which is an unresolved row — the honest answer, and
			// the one the next cycle corrects.
			continue
		}
		read[i] = found.PullRequest.pull(p.List)
	}
	return read, nil
}
