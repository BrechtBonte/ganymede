package pulls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH writes a script standing in for gh: it prints body on stdout, said on
// stderr, and exits with code. The arguments it was called with are written
// beside it, so a test can assert on the query that was actually sent.
func fakeGH(t *testing.T, body, said string, code int) (gh, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	gh = filepath.Join(dir, "gh")
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$*\" > " + argsFile + "\n" +
		"cat >> " + argsFile + "\n" +
		"printf '%s' " + shellQuote(body) + "\n" +
		"printf '%s' " + shellQuote(said) + " >&2\n" +
		"exit " + itoa(code) + "\n"
	if err := os.WriteFile(gh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return gh, argsFile
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const twoLists = `{"data":{
  "rateLimit":{"cost":1,"remaining":4993},
  "review":{"issueCount":1,"nodes":[
    {"number":1564,"title":"[PWR-237] Document the endpoint","url":"https://github.com/teamleadercrm/api-internal/pull/1564",
     "isDraft":false,"baseRefName":"master","headRefName":"PWR-237/document","updatedAt":"2026-09-22T06:11:03Z",
     "mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":"REVIEW_REQUIRED",
     "author":{"login":"a-colleague"},
     "repository":{"nameWithOwner":"teamleadercrm/api-internal","defaultBranchRef":{"name":"master"}},
     "statusCheckRollup":{"state":"PENDING"}}]},
  "mine":{"issueCount":1,"nodes":[
    {"number":1010,"title":"[FIRE-3137] Read the batch size","url":"https://github.com/teamleadercrm/focus-service-bookkeeping/pull/1010",
     "isDraft":true,"baseRefName":"master","headRefName":"FIRE-3137/batch-size","updatedAt":"2026-09-22T05:02:00Z",
     "mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":"REVIEW_REQUIRED",
     "author":{"login":"BrechtBonte"},
     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
     "statusCheckRollup":null}]}}}`

func TestAFetchReadsBothListsFromOneRequest(t *testing.T) {
	gh, argsFile := fakeGH(t, twoLists, "", 0)
	set, err := Fetcher{GH: gh}.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(set) != 2 {
		t.Fatalf("got %d Pulls, want 2", len(set))
	}

	mine := set.Of(Authored)
	if len(mine) != 1 || mine[0].Number != 1010 || !mine[0].IsDraft {
		t.Errorf("AUTHORED: got %+v", mine)
	}
	if mine[0].Repo != "teamleadercrm/focus-service-bookkeeping" || mine[0].Default != "master" {
		t.Errorf("AUTHORED identity: got %q base %q", mine[0].Repo, mine[0].Default)
	}
	// A PR with no checks at all comes back as a null rollup, which must read
	// as "no checks" rather than panicking on the way in.
	if mine[0].Checks != "" {
		t.Errorf("a null rollup read as %q", mine[0].Checks)
	}
	review := set.Of(Requested)
	if len(review) != 1 || review[0].Number != 1564 || review[0].Checks != "PENDING" {
		t.Errorf("REQUESTED: got %+v", review)
	}
	if review[0].URL == "" || review[0].Author != "a-colleague" {
		t.Errorf("REQUESTED fields: url=%q author=%q", review[0].URL, review[0].Author)
	}

	sent, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	query := string(sent)
	// The qualifier inverts the obvious guess and getting it wrong would be
	// invisible: every human review request on this account arrives as a team
	// request, which only the broad qualifier matches.
	for _, want := range []string{
		"is:pr is:open review-requested:@me",
		"is:pr is:open author:@me",
		"defaultBranchRef",
		"mergeStateStatus",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("the request did not carry %q\n%s", want, query)
		}
	}
	if strings.Contains(query, "user-review-requested") {
		t.Error("the request used the narrow qualifier, which matches only dependabot here")
	}
	if strings.Contains(query, "assignee:@me") {
		t.Error("the request asked for assignee:@me, which returns the same rows as author:@me")
	}
}

func TestTheThreeWaysAFetchFails(t *testing.T) {
	for _, c := range []struct {
		says string
		said string
		code int
		want Trouble
	}{
		{"never logged in is gh's own exit code", "", 4, NotLoggedIn},
		{"an expired or revoked token says so on stderr", "gh: HTTP 401: Bad credentials", 1, Unauthorized},
		{"anything else is the network", "dial tcp: lookup api.github.com: no such host", 1, Unreachable},
	} {
		gh, _ := fakeGH(t, "", c.said, c.code)
		_, err := Fetcher{GH: gh}.Fetch(context.Background())
		if err == nil {
			t.Fatalf("%s: fetch succeeded", c.says)
		}
		got, ok := TroubleOf(err)
		if !ok || got != c.want {
			t.Errorf("%s: got %v (%v), want %v", c.says, got, ok, c.want)
		}
	}
}

func TestAMissingGHIsTheLoginYouHaveNotMade(t *testing.T) {
	// A machine with the harness and no gh runs today. Once the section ships
	// gh is a hard runtime dependency, and the body that names `gh auth login`
	// is the right one to send a reader to.
	_, err := Fetcher{GH: "/nonexistent/gh"}.Fetch(context.Background())
	got, ok := TroubleOf(err)
	if !ok || got != NotLoggedIn {
		t.Errorf("got %v (%v), want NotLoggedIn", got, ok)
	}
}

func TestAnAnswerThatIsNotJSONIsUnreachableRatherThanAPanic(t *testing.T) {
	gh, _ := fakeGH(t, "<html>502 Bad Gateway</html>", "", 0)
	_, err := Fetcher{GH: gh}.Fetch(context.Background())
	got, ok := TroubleOf(err)
	if !ok || got != Unreachable {
		t.Errorf("got %v (%v), want Unreachable", got, ok)
	}
}

func TestASecondPassAsksOnlyAboutTheRowsItWasGiven(t *testing.T) {
	const reread = `{"data":{
	  "rateLimit":{"cost":1,"remaining":4992},
	  "p0":{"pullRequest":{"number":979,"title":"Bump guzzle","url":"https://github.com/teamleadercrm/focus-service-bookkeeping/pull/979",
	     "isDraft":false,"baseRefName":"master","headRefName":"dependabot/composer/guzzle-8.2.0","updatedAt":"2026-09-22T06:00:00Z",
	     "mergeable":"MERGEABLE","mergeStateStatus":"BEHIND","reviewDecision":"REVIEW_REQUIRED",
	     "author":{"login":"dependabot"},
	     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
	     "statusCheckRollup":{"state":"FAILURE"}}}}}`

	gh, argsFile := fakeGH(t, reread, "", 0)
	unknown := Pull{
		List: Authored, Repo: "teamleadercrm/focus-service-bookkeeping", Number: 979,
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Review: "REVIEW_REQUIRED",
		Base: "master", Default: "master",
	}

	rows, err := Fetcher{GH: gh}.Reread(context.Background(), []Pull{unknown})
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// The resolved fields land, and the row keeps the list it was already in —
	// the second pass reads a repository, which does not know which search
	// found the row.
	if rows[0].MergeState != "BEHIND" || rows[0].Mergeable != "MERGEABLE" {
		t.Errorf("mergeability did not resolve: %+v", rows[0])
	}
	if rows[0].List != Authored {
		t.Error("the re-read row lost its list")
	}
	if got := rows[0].State(); got != Behind {
		t.Errorf("state after the second pass: got %q, want %q", got, Behind)
	}

	sent, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	query := string(sent)
	for _, want := range []string{"focus-service-bookkeeping", "979", "pullRequest"} {
		if !strings.Contains(query, want) {
			t.Errorf("the second pass did not ask about %q\n%s", want, query)
		}
	}
	// It is a re-read of named rows, not the whole census again.
	if strings.Contains(query, "review-requested:@me") || strings.Contains(query, "author:@me") {
		t.Error("the second pass ran the search again instead of re-reading the rows")
	}
}

func TestASecondPassWithNothingToAskCostsNoRequest(t *testing.T) {
	// A gh that would fail if it ran at all, so a request proves itself.
	rows, err := Fetcher{GH: "/nonexistent/gh"}.Reread(context.Background(), nil)
	if err != nil {
		t.Fatalf("reread of nothing: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want none", len(rows))
	}
}

func TestARowThatNeverResolvesKeepsEverythingButItsState(t *testing.T) {
	const stillUnknown = `{"data":{"rateLimit":{"cost":1,"remaining":4991},
	  "p0":{"pullRequest":{"number":979,"title":"Bump guzzle","url":"https://example.invalid/979",
	     "isDraft":false,"baseRefName":"master","headRefName":"dependabot/composer/guzzle-8.2.0","updatedAt":"2026-09-22T06:00:00Z",
	     "mergeable":"UNKNOWN","mergeStateStatus":"UNKNOWN","reviewDecision":"REVIEW_REQUIRED",
	     "author":{"login":"dependabot"},
	     "repository":{"nameWithOwner":"teamleadercrm/focus-service-bookkeeping","defaultBranchRef":{"name":"master"}},
	     "statusCheckRollup":{"state":"FAILURE"}}}}}`

	gh, _ := fakeGH(t, stillUnknown, "", 0)
	unknown := Pull{
		List: Authored, Repo: "teamleadercrm/focus-service-bookkeeping", Number: 979,
		Mergeable: "UNKNOWN", MergeState: "UNKNOWN", Base: "master", Default: "master",
	}
	rows, err := Fetcher{GH: gh}.Reread(context.Background(), []Pull{unknown})
	if err != nil {
		t.Fatal(err)
	}
	// It keeps its number, repo and marks — all from fields that never go
	// UNKNOWN — and draws no state word. There is no third pass: the next
	// cycle picks it up, and the row is honest meanwhile.
	if got := rows[0].State(); got != Unresolved {
		t.Errorf("got %q, want no state", got)
	}
	if !rows[0].Failing() || rows[0].Number != 979 {
		t.Errorf("a stateless row lost its marks or its identity: %+v", rows[0])
	}
	if rows[0].YourMove() {
		t.Error("a stateless row is Your move")
	}
}
