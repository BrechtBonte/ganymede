// Command pullsmock is a THROWAWAY mock for issue #75 — the Pulls section's
// row anatomy. It is not part of the harness and nothing imports it.
//
// It draws the whole 40x45 sidepanel, so the section is judged against the real
// density above it, in the harness's own palette, with the Pulls this account
// actually had open. Three variants disagree about what a 40-column row spends
// itself on:
//
//	A  identity left, state right          — the shape every other row uses
//	B  a state column on the left          — the eye runs down whose move it is
//	C  two lines per Pull, title on line 2 — the title survives, at half the census
//
// Run: go run ./cmd/pullsmock                     every case, every variant
//
//	go run ./cmd/pullsmock -variant B -case scrolling
//	go run ./cmd/pullsmock -marks literal          #70's mark table as written
//	go run ./cmd/pullsmock > mock.ansi && cat mock.ansi
package main

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// ─── the harness's own palette and helpers, copied out of internal/dashboard ──

var (
	quietStyle           = lipgloss.NewStyle().Faint(true)
	ruleStyle            = lipgloss.NewStyle().Faint(true)
	repoStyle            = lipgloss.NewStyle().Bold(true)
	brandStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff"))
	ticketColour         = lipgloss.NewStyle().Foreground(lipgloss.Color("#a5d6ff"))
	cautionStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	claimedStyle         = cautionStyle
	selectedStyle        = lipgloss.NewStyle().Reverse(true)
	blurredSelectedStyle = selectedStyle.Faint(true)

	blockedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	readyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	workingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
)

const (
	width  = 40
	height = 45

	caution = "⚠"
	frozen  = "❄"

	// Pull marks. None of these collide with the glyphs already spoken on this
	// panel: █ ● ⠿ ○ ❯ ⚠ ❄ ⇡ ⏵ ▣ ⚑ ▢.
	checksFailed  = "✗"
	checksRunning = "◷"
	checksPassed  = "✓"
	approvalMark  = "✓"
	stackMark     = "↳"
	moveMark      = "◆" // #72's Your-move mark, drawn in the tree
	// A row whose mergeability never resolved has no state word (#73). The
	// absence is drawn, so it reads as "we could not tell" rather than as a
	// column that failed to render. It is not a tenth word.
	noState   = "—"
	aboveMark = "▴"
	belowMark = "▾"
)

func spread(left, right string, w int) string {
	if room := w - lipgloss.Width(right) - 1; lipgloss.Width(left) > room {
		left = truncate(left, max(0, room))
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return truncate(right, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func joined(parts ...string) string {
	said := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			said = append(said, part)
		}
	}
	return strings.Join(said, " ")
}

func rendered(style lipgloss.Style, text string) string {
	if text == "" {
		return ""
	}
	return style.Render(text)
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "")
}

func elide(name string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(name, w, "…")
}

func tailOf(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if over := ansi.StringWidth(s) - w; over >= 0 {
		return ansi.TruncateLeft(s, over+1, "…")
	}
	return s
}

func fitKeys(keys []string, w int) string {
	var line string
	hints := make([]string, 0, len(keys))
	for _, key := range keys {
		next := key
		if line != "" {
			next = line + " · " + key
		}
		if lipgloss.Width(next) > w {
			break
		}
		line = next
		hints = append(hints, hinted(key))
	}
	return strings.Join(hints, quietStyle.Render(" · "))
}

func hinted(key string) string {
	char, label, ok := strings.Cut(key, " ")
	if !ok {
		return char
	}
	return char + " " + quietStyle.Render(label)
}

func fill(lines []string, space int) []string {
	for len(lines) < space {
		lines = append(lines, "")
	}
	return lines
}

func clip(lines []string, space int) []string {
	if len(lines) <= space {
		return lines
	}
	return lines[:max(0, space)]
}

// ─── Pulls ───────────────────────────────────────────────────────────────────

type pull struct {
	repo     string // nameWithOwner, minus the owner
	number   int
	title    string
	draft    bool
	merge    string // MERGEABLE · CONFLICTING · UNKNOWN
	state    string // mergeStateStatus
	review   string // APPROVED · CHANGES_REQUESTED · REVIEW_REQUIRED · ""
	checks   string // SUCCESS · FAILURE · PENDING · ""
	base     string
	head     string
	deflt    string
	authored bool
}

// word is the Pull's state, by #70's chain in #71's vocabulary. An AUTHORED row
// whose mergeability never resolved has no word at all (#73).
func (p pull) word() string {
	if p.draft {
		return "Draft"
	}
	if !p.authored {
		if p.review == "APPROVED" {
			return "Approved"
		}
		return "Yours"
	}
	if p.review == "CHANGES_REQUESTED" {
		return "Rework"
	}
	if p.merge == "UNKNOWN" || p.state == "UNKNOWN" {
		return ""
	}
	if p.merge == "CONFLICTING" || p.state == "DIRTY" {
		return "Conflicted"
	}
	if p.state == "BEHIND" {
		return "Behind"
	}
	if (p.state == "CLEAN" || p.state == "HAS_HOOKS") && p.base == p.deflt {
		return "Landable"
	}
	return "Sent"
}

func (p pull) yourMove() bool {
	switch p.word() {
	case "Rework", "Conflicted", "Behind", "Landable", "Yours":
		return true
	}
	return false
}

// said is the state as the row draws it: the word, or the mark that stands for
// its absence.
func (p pull) said() string {
	if p.word() == "" {
		return noState
	}
	return p.word()
}

// wordStyle: the states that are Your move read in the colour the rail already
// keeps for a row wanting something, and the rest in its quiet.
func (p pull) wordStyle() lipgloss.Style {
	if p.yourMove() {
		return readyStyle
	}
	return quietStyle
}

// markSet is the three readings of #70's mark table this mock puts side by side.
// "literal" draws the table as written, which lands two ticks on one row.
// "quiet" spends no column on a passing check. "failures" draws only what is
// exceptional: 6 of today's 11 rows are PENDING and 3 of the full 17 are
// SUCCESS, so both resting states are marks the eye stops seeing.
var markSet = "failures"

func (p pull) marks() string {
	var said []string
	switch p.checks {
	case "FAILURE", "ERROR":
		said = append(said, checksFailed)
	case "PENDING", "EXPECTED":
		if markSet != "failures" {
			said = append(said, checksRunning)
		}
	case "SUCCESS":
		if markSet == "literal" {
			said = append(said, checksPassed)
		}
	}
	if p.authored && p.review == "APPROVED" {
		if markSet == "literal" {
			said = append(said, "★")
		} else {
			said = append(said, approvalMark)
		}
	}
	return strings.Join(said, " ")
}

func (p pull) markStyle() lipgloss.Style {
	if p.checks == "FAILURE" || p.checks == "ERROR" {
		return blockedStyle
	}
	return quietStyle
}

// stackMarks is how much the stack mark says. "bare" is one column: the lists
// sort by repo and number, so a stacked Pull's parent is the row above it three
// times in four and the mark only has to say that this one is not on the default
// branch. "number" is #70's own wording — the row says what it is stacked on —
// and costs 7 columns off every row in the section, since the identity column is
// measured against the widest tail in it.
var stackMarks = "bare"

// parent is the stack mark: the Pull this one sits on, by the free local join
// #70 measured — every other row's head branch against this row's base. Three
// times in four that is a short #number; the fourth is a 30–56 column branch
// name, which no 40-column row can carry, so the mark stands alone.
func (p pull) parent(all []pull) string {
	if p.base == "" || p.base == p.deflt {
		return ""
	}
	if stackMarks == "bare" {
		return stackMark
	}
	for _, other := range all {
		if other.repo == p.repo && other.head == p.base {
			return stackMark + "#" + strconv.Itoa(other.number)
		}
	}
	return stackMark
}

// ─── the data ────────────────────────────────────────────────────────────────

// live is every Pull this account had open at 2026-09-22 08:12, read with the
// query #68 settled — two aliased search fields, cost 1, 4.6 s.
func live() []pull {
	return []pull{
		{repo: "focus-service-bookkeeping", number: 1010, authored: true,
			title: "[FIRE-3137] Read the invoice-fetching batch size per integration link",
			draft: true, merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "FIRE-3137/invoice-fetching-batch-size-read-path"},

		{repo: "focus-service-ai-credit-usage", number: 279,
			title: "[FIRE-3075] Close the proration open point in the diagram and ADRs",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "feature/FIRE-3075/close-the-open-point"},
		{repo: "focus-frontend", number: 7430,
			title: "[FIRE-3072] Show the monthly credit balance in the back office",
			merge: "MERGEABLE", state: "BLOCKED", review: "APPROVED", checks: "PENDING",
			base: "master", deflt: "master", head: "FIRE-3072/tl-admin-ai-usage"},
		{repo: "focus-service-ai-assistant", number: 1104,
			title: "Resolve relative dates with a tool instead of the calendar block",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "resolve-dates-tool"},
		{repo: "api-internal", number: 1564,
			title: "[PWR-237] Document the external-products/products.list endpoint",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "PWR-237/document-external-products-list"},
		{repo: "focus-service-bookkeeping", number: 1005,
			title: "Bump teamleader/focus-php-cs-fixer-rules from 2.2.0 to 3.0.0",
			merge: "MERGEABLE", state: "BLOCKED", review: "APPROVED", checks: "FAILURE",
			base: "master", deflt: "master", head: "dependabot/composer/teamleader/focus-php-cs-fixer-rules-3.0.0"},
		{repo: "focus-service-developer-portal-frontend", number: 956,
			title: "build(deps): Bump @teamleader/ahoy from 3.5.0 to 3.6.0",
			merge: "MERGEABLE", state: "UNSTABLE", review: "APPROVED", checks: "FAILURE",
			base: "master", deflt: "master", head: "dependabot/npm_and_yarn/teamleader/ahoy-3.6.0"},
		{repo: "focus-service-developer-portal-frontend", number: 953,
			title: "build(deps-dev): Bump the other-dev-dependencies group across 1 directory with 3 updates",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "FAILURE",
			base: "master", deflt: "master", head: "dependabot/npm_and_yarn/other-dev-dependencies-4478f31446"},
		{repo: "focus-service-ai-assistant", number: 1066,
			title: "Bump ioredis from 5.11.1 to 6.0.0",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "dependabot/npm_and_yarn/ioredis-6.0.0"},
		{repo: "focus-service-bookkeeping", number: 979,
			title: "Bump guzzlehttp/guzzle from 7.15.2 to 8.2.0",
			merge: "UNKNOWN", state: "UNKNOWN", review: "REVIEW_REQUIRED", checks: "FAILURE",
			base: "master", deflt: "master", head: "dependabot/composer/guzzlehttp/guzzle-8.2.0"},
		{repo: "focus-service-insights-datasources", number: 946,
			title: "Bump guzzlehttp/guzzle from 7.15.5 to 8.2.0",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "master", deflt: "master", head: "dependabot/composer/guzzlehttp/guzzle-8.2.0"},
	}
}

// full is the 17 rows the section was budgeted for: today's 11, plus 6 recorded
// on the measuring days by #70 and #72 — the four-deep stack, and the authored
// rows a checkout is sitting on. Titles for those six are reconstructed from
// their branch names; every field the drawing depends on is as measured.
func full() []pull {
	return append(live(), []pull{
		{repo: "teamleader-agent-steering", number: 82, authored: true,
			title: "Add a setup health check to the skill",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "SUCCESS",
			base: "main", deflt: "main", head: "skill/setup-health-check"},
		{repo: "focus-service-developer-portal-frontend", number: 963, authored: true,
			title: "Bump fast-uri to 3.1.8",
			merge: "MERGEABLE", state: "BEHIND", review: "REVIEW_REQUIRED", checks: "SUCCESS",
			base: "master", deflt: "master", head: "security/bump-fast-uri-3.1.8"},
		{repo: "core", number: 48032, authored: true,
			title: "[PHX-4335] Decode the search term for the subscription title",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "PHX-4335-decode-search-term-for-invoice-title", deflt: "master",
			head: "PHX-4335-decode-search-term-for-subscription-title"},
		{repo: "core", number: 48033, authored: true,
			title: "[PHX-4335] Decode the search term for company custom fields",
			merge: "MERGEABLE", state: "BLOCKED", review: "REVIEW_REQUIRED", checks: "PENDING",
			base: "PHX-4335-decode-search-term-for-subscription-title", deflt: "master",
			head: "PHX-4335-decode-search-term-for-company-custom-fields"},
		{repo: "core", number: 48034, authored: true,
			title: "[PHX-4335] Decode the search term for new custom field storage",
			merge: "MERGEABLE", state: "CLEAN", review: "APPROVED", checks: "SUCCESS",
			base: "PHX-4335-decode-search-term-for-company-custom-fields", deflt: "master",
			head: "PHX-4335-decode-search-term-for-new-custom-field-storage"},
		{repo: "prqa", number: 200,
			title: "Read the installation id from Redis",
			merge: "MERGEABLE", state: "CLEAN", review: "REVIEW_REQUIRED", checks: "SUCCESS",
			base: "fix/installation-id-from-redis", deflt: "main",
			head: "fix/installation-id-from-redis-follow-up"},
	}...)
}

// stateless is today's authored Pull with its mergeability still UNKNOWN after
// the cycle's second pass — constructed, since #73's case has never been
// observed. Every other field is live.
func stateless() []pull {
	rows := live()
	rows[0].draft = false
	rows[0].merge = "UNKNOWN"
	rows[0].state = "UNKNOWN"
	return rows
}

// ─── the tree above it ───────────────────────────────────────────────────────

type treeRow struct {
	repo    string
	root    string // ▣ in use · ⚑ claimed · ▢ free
	branch  string
	dirty   bool
	session string
	glyph   string
	style   lipgloss.Style
	ticket  string
	age     string
	move    bool // #72's Your-move mark
	active  bool // the Session the working pane is showing
}

// workingSet is a plausible rail read off this machine: the repos with recent
// harness activity in state.json, on the branches their clones are actually on.
// move lights #72's mark, which today's real working set leaves dark — no
// checkout is sitting on the head branch of any open Pull.
func workingSet(move bool) []treeRow {
	return []treeRow{
		{repo: "ganymede", root: "▣", branch: "proto/pulls-row-anatomy", dirty: true},
		{session: "ganymede-4f", glyph: "⠹", style: workingStyle, age: "2m", active: !move},
		{repo: "focus-service-bookkeeping", root: "▣", branch: "revert/FIRE-3108-data-layer-header"},
		{session: "FIRE-3137/invoice-fetching-batch-size-read-path", glyph: "█", style: blockedStyle,
			ticket: "F-3137", age: "8m", move: move, active: move},
		{session: "focus-service-bookkeeping-b2", glyph: "○", style: quietStyle, age: "1h"},
		{repo: "focus-service-ai-credit-usage", root: "▣"},
		{session: "focus-service-ai-credit-usage-c9", glyph: "●", style: readyStyle, ticket: "F-2943", age: "12m"},
		{repo: "api-internal", root: "⚑", branch: "refactor/openapi", dirty: true},
		{repo: "core", root: "▢", branch: "FIRE-3065/bookkeeping-payment-terms-flag"},
		{repo: "focus-frontend", root: "▢", branch: "FIRE-3135/remove-beta-labels"},
		{repo: "focus-service-ai-assistant", root: "▢"},
		{repo: "focus-service-developer-portal-frontend", root: "▢", branch: "security/bump-fast-uri-3.1.8"},
		{repo: "prqa", root: "▢"},
		{repo: "teamleader-agent-steering", root: "▢", branch: "skill/setup-health-check"},
	}
}

func treeLines(rows []treeRow) []string {
	var lines []string
	for _, r := range rows {
		if r.session == "" {
			lines = append(lines, spread(repoStyle.Render(r.repo), rendered(rootStyleOf(r.root), r.root), width))
			if said := carrying(r.branch, r.dirty); said != "" {
				lines = append(lines, " "+cautionStyle.Render(elide(said, width-1)))
			}
			continue
		}
		move := ""
		if r.move {
			move = moveMark
		}
		plain := joined(r.ticket, r.age, move)
		tail := joined(rendered(ticketColour, r.ticket), rendered(quietStyle, r.age), rendered(readyStyle, move))
		label := elide(r.session, width-lipgloss.Width("  "+r.glyph+" ")-lipgloss.Width(plain)-1)
		if r.active {
			lines = append(lines, blurredSelectedStyle.Width(width).
				Render(spread("  "+r.glyph+" "+label, plain, width)))
			continue
		}
		lines = append(lines, spread("  "+r.style.Render(r.glyph)+" "+label, tail, width))
	}
	return lines
}

func rootStyleOf(glyph string) lipgloss.Style {
	switch glyph {
	case "▣":
		return workingStyle
	case "⚑":
		return claimedStyle
	}
	return quietStyle
}

func carrying(branch string, dirty bool) string {
	switch {
	case branch != "" && dirty:
		return caution + " " + branch + " · dirty"
	case branch != "":
		return caution + " " + branch
	case dirty:
		return caution + " dirty"
	}
	return ""
}

// ─── the section ─────────────────────────────────────────────────────────────

// entry is one drawable thing in the section: a list heading, or a Pull.
type entry struct {
	heading string
	count   int
	p       pull
	isPull  bool
}

type section struct {
	variant string
	pulls   []pull
	cursor  int    // index into the entry list; -1 for a cursor still in the tree
	body    string // "", "fetching", "empty", "auth", "network"
	fetched string
	offset  int    // the first entry the window shows
	active  string // repo|head of the Session the working pane is showing (#72)
}

// columns is where the row's parts start, measured once over the whole section
// rather than per row. A repo elided to whatever that row's own tail left over
// reads as a different repo on the row above — #1005 and #979 in one repo came
// out as `focus-service-bookkeep…` and `focus-service-bookkeeping`.
func (s section) columns() (repo, number int) {
	for _, p := range s.pulls {
		if w := lipgloss.Width(joined(p.parent(s.pulls), p.said(), p.marks())); w > repo {
			repo = w
		}
		if w := lipgloss.Width("#" + strconv.Itoa(p.number)); w > number {
			number = w
		}
	}
	return width - 1 - repo - 1 - number, number
}

func (s section) entries() []entry {
	authored, requested := split(s.pulls)
	list := []entry{{heading: "AUTHORED", count: len(authored)}}
	for _, p := range authored {
		list = append(list, entry{p: p, isPull: true})
	}
	list = append(list, entry{heading: "REQUESTED", count: len(requested)})
	for _, p := range requested {
		list = append(list, entry{p: p, isPull: true})
	}
	return list
}

// panel is the 20 lines the section is given: the key legend on the last, and
// 19 for the lists — 2 headings and 17 rows on a full day (#68, #74). It also
// says how many rows sit above and below the window, which the chrome line
// draws: the legend is 39 of the 40 columns and has nowhere to put them.
func (s section) panel(space int) (lines []string, above, below int) {
	if s.body == "network" {
		// The rows stay. They are the last good answer and their own timestamp
		// says how old it is; what the section adds is why it is not newer.
		space--
		lines, above, below = s.rows(space)
		return append([]string{cautionStyle.Render(truncate(caution+" Unreachable — retrying 5m", width))}, lines...), above, below
	}
	if s.body != "" {
		return fill(clip(s.failure(space), space), space), 0, 0
	}

	return s.rows(space)
}

func (s section) rows(space int) (lines []string, above, below int) {
	list := s.entries()
	drawn := make([][]string, len(list))
	for i, e := range list {
		if !e.isPull {
			drawn[i] = []string{s.heading(e.heading, e.count)}
			continue
		}
		drawn[i] = s.rowLines(e.p, i == s.cursor)
	}

	room := space - 1 // the legend has the last line
	first := min(max(s.offset, 0), len(list)-1)
	if first > 0 {
		// The heading of the list the window opened inside sticks, so a
		// scrolled section never shows rows whose list is off the top.
		name, count, hidden := opening(list, first)
		lines = append(lines, s.heading(name, count))
		above = hidden
		room--
	}
	used, last := 0, first
	for ; last < len(list); last++ {
		if used+len(drawn[last]) > room {
			break
		}
		lines = append(lines, drawn[last]...)
		used += len(drawn[last])
	}
	for _, e := range list[last:] {
		if e.isPull {
			below++
		}
	}
	return append(fill(lines, space-1), s.legend()), above, below
}

// opening is the list the window's first entry belongs to, and how many of that
// list's rows are above the window.
func opening(list []entry, first int) (string, int, int) {
	hidden := 0
	for i := first - 1; i >= 0; i-- {
		if !list[i].isPull {
			return list[i].heading, list[i].count, hidden
		}
		hidden++
	}
	return "", 0, 0
}

func (s section) heading(name string, count int) string {
	return quietStyle.Render(truncate(name+" "+strconv.Itoa(count), width))
}

func (s section) legend() string {
	return fitKeys([]string{"⏎ jump", "o open", "r refresh", "esc close"}, width)
}

// chrome is the section's label and, at the far end, when the set was fetched —
// the shape header() uses for the clock, and what says "two hours old" without a
// word for it (#73). What is out of sight above and below rides here too: it is
// the one line in the section that costs no row and is never scrolled away.
func (s section) chrome(above, below int) string {
	scroll := ""
	if above > 0 {
		scroll = aboveMark + strconv.Itoa(above)
	}
	if below > 0 {
		scroll = joined(scroll, belowMark+strconv.Itoa(below))
	}
	return spread(quietStyle.Render("PULLS"), rendered(quietStyle, joined(scroll, s.fetched)), width)
}

func (s section) failure(space int) []string {
	var lines []string
	switch s.body {
	case "fetching":
		lines = []string{quietStyle.Render("Fetching…")}
	case "empty":
		lines = []string{
			quietStyle.Render("Nothing of yours is open, and"),
			quietStyle.Render("nobody has asked for a review."),
		}
	case "auth":
		lines = []string{
			cautionStyle.Render(caution + " Not logged in to GitHub."),
			quietStyle.Render("Run gh auth login; r retries."),
		}
	}
	return append(fill(lines, space-1), s.legend())
}

func split(pulls []pull) (authored, requested []pull) {
	for _, p := range pulls {
		if p.authored {
			authored = append(authored, p)
		} else {
			requested = append(requested, p)
		}
	}
	byRepo := func(rows []pull) func(i, j int) bool {
		return func(i, j int) bool {
			if rows[i].repo != rows[j].repo {
				return rows[i].repo < rows[j].repo
			}
			return rows[i].number < rows[j].number
		}
	}
	sort.SliceStable(authored, byRepo(authored))
	sort.SliceStable(requested, byRepo(requested))
	return authored, requested
}

// ─── the three row anatomies ─────────────────────────────────────────────────

func (s section) rowLines(p pull, cursor bool) []string {
	lines := s.drawRow(p, cursor)
	if cursor || s.active == "" || s.active != p.repo+"|"+p.head {
		return lines
	}
	// The Pull the working pane's Session is sitting on, in the weaker of the
	// rail's two inversions — the same pair line() already draws, so the panel
	// and the tree say "here you are" and "here you were" the one way.
	for i, line := range lines {
		lines[i] = blurredSelectedStyle.Width(width).Render(ansi.Strip(line))
	}
	return lines
}

func (s section) drawRow(p pull, cursor bool) []string {
	switch s.variant {
	case "B":
		return []string{s.stateColumn(p, cursor)}
	case "C":
		return s.twoLine(p, cursor)
	default:
		return []string{s.identityFirst(p, cursor)}
	}
}

// A — identity left, state right. The shape every other row on the Dashboard
// uses: spread() right-aligns the tail, so the state lands in the same column
// on every row and the repo gives way. No title.
func (s section) identityFirst(p pull, cursor bool) string {
	room, _ := s.columns()
	left := " " + tailOf(p.repo, room) + "#" + strconv.Itoa(p.number)
	tail := joined(p.parent(s.pulls), p.said(), p.marks())
	if cursor {
		return selectedStyle.Width(width).Render(spread(left, tail, width))
	}
	styled := joined(
		rendered(quietStyle, p.parent(s.pulls)),
		rendered(p.wordStyle(), p.said()),
		rendered(p.markStyle(), p.marks()))
	return spread(" "+quietStyle.Render(tailOf(p.repo, room))+"#"+strconv.Itoa(p.number), styled, width)
}

// B — the state leads, in a fixed column, and identity fills what is left. The
// eye runs down whose move it is, the way it runs down the state glyph in the
// tree; the repo keeps its tail, which is the end that differs.
func (s section) stateColumn(p pull, cursor bool) string {
	const column = 10 // Conflicted, the longest of the nine
	word := p.said()
	marks := p.marks()
	head := " " + word + strings.Repeat(" ", max(0, column-lipgloss.Width(word))) + " "
	head += marks + strings.Repeat(" ", max(0, 3-lipgloss.Width(marks)))
	_, numbers := s.columns()
	room := width - lipgloss.Width(head) - numbers - lipgloss.Width(p.parent(s.pulls))
	body := tailOf(p.repo, room) + "#" + strconv.Itoa(p.number) + p.parent(s.pulls)
	if cursor {
		return selectedStyle.Width(width).Render(truncate(head+body, width))
	}
	styledHead := " " + p.wordStyle().Render(word) + strings.Repeat(" ", max(0, column-lipgloss.Width(word))) + " " +
		rendered(p.markStyle(), marks) + strings.Repeat(" ", max(0, 3-lipgloss.Width(marks)))
	return truncate(styledHead+quietStyle.Render(body), width)
}

// C — two lines per Pull: identity and state on the first, the title on the
// second, the way a repo header's caution hangs under it. The title survives —
// and the section holds eight Pulls where A and B hold seventeen.
func (s section) twoLine(p pull, cursor bool) []string {
	room, _ := s.columns()
	left := " " + tailOf(p.repo, room) + "#" + strconv.Itoa(p.number)
	tail := joined(p.parent(s.pulls), p.said(), p.marks())
	title := "   " + elide(p.title, width-4)
	if cursor {
		return []string{
			selectedStyle.Width(width).Render(spread(left, tail, width)),
			selectedStyle.Width(width).Render(truncate(title, width)),
		}
	}
	styled := joined(rendered(quietStyle, p.parent(s.pulls)), rendered(p.wordStyle(), p.said()), rendered(p.markStyle(), p.marks()))
	return []string{
		spread(" "+quietStyle.Render(tailOf(p.repo, room))+"#"+strconv.Itoa(p.number), styled, width),
		quietStyle.Render(truncate(title, width)),
	}
}

// ─── the whole sidepanel ─────────────────────────────────────────────────────

// dashboard draws the Dock's left pane at its real size: the header and its
// rule, the tree, then the rule and the PULLS label, then the section. chrome is
// 4 lines and the section takes 20, so the tree keeps 21 — #67's floor.
func dashboard(tree []treeRow, s section, open bool) string {
	rule := ruleStyle.Render(strings.Repeat("─", width))
	head := spread(brandStyle.Render("GANYMEDE"),
		joined(blockedStyle.Render("█ 1"), readyStyle.Render("● 1"), quietStyle.Render("16:45")), width)

	detail, label := selectedBox(), quietStyle.Render("SELECTED")
	if open {
		lines, above, below := s.panel(20)
		detail, label = lines, s.chrome(above, below)
	}
	space := height - 4 - len(detail)
	lines := append([]string{head, rule}, fill(clip(treeLines(tree), space), space)...)
	lines = append(lines, rule, label)
	return strings.Join(append(lines, detail...), "\n")
}

// selectedBox is what the foot holds while Pulls is closed — the row the cursor
// is on, which is what the section takes the place of.
func selectedBox() []string {
	return []string{
		blockedStyle.Render("█") + " " + truncate("Blocked · 8m", width-2),
		elide("focus-service-bookkeeping", width),
		ticketColour.Render(truncate("FIRE-3137", width)),
		blockedStyle.Render(truncate("Run the migration against staging?", width)),
		quietStyle.Render(truncate("~/Projects/teamleadercrm/focus-ser…", width)),
		fitKeys([]string{"⏎ jump", "t ticket", "o open"}, width),
	}
}

// ─── the cases ───────────────────────────────────────────────────────────────

// matched is the checkout the tree's Blocked Session is sitting in, in #72's
// own terms: origin's nameWithOwner plus the head branch.
const matched = "focus-service-bookkeeping|FIRE-3137/invoice-fetching-batch-size-read-path"

type mockCase struct {
	name string
	says string
	draw func(variant string) string
}

func cases() []mockCase {
	return []mockCase{
		{"today", "The 11 Pulls this account had open at 08:12. Six of the 20 lines spare.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, pulls: live(), cursor: -1, fetched: "08:12"}, true)
			}},
		{"full", "17 rows — the day the section was budgeted for. Exactly full, nothing scrolled.",
			func(v string) string {
				return dashboard(workingSet(true), section{variant: v, pulls: full(), cursor: -1,
					fetched: "14:31", active: matched}, true)
			}},
		{"scrolling", "The same 17, the cursor in the section, the window past the top.",
			func(v string) string {
				return dashboard(workingSet(true), section{variant: v, pulls: full(), cursor: 12, offset: 10, fetched: "14:31"}, true)
			}},
		{"stateless", "The authored row still UNKNOWN after the second pass: marks, no state word.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, pulls: stateless(), cursor: -1, fetched: "08:12"}, true)
			}},
		{"cursor", "Both highlights: the cursor's row, and the Pull the working pane is sitting on.",
			func(v string) string {
				return dashboard(workingSet(true), section{variant: v, pulls: live(), cursor: 4,
					fetched: "08:12", active: matched}, true)
			}},
		{"collapsed", "esc, and the tree has its 21 lines and the SELECTED box back.",
			func(v string) string {
				return dashboard(workingSet(true), section{variant: v, pulls: live(), cursor: -1, fetched: "08:12"}, false)
			}},
		{"fetching", "The first cycle, roughly twelve seconds. Nothing is remembered across restarts.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, body: "fetching"}, true)
			}},
		{"empty", "A fetch that worked and found nothing — the time on the chrome line proves it.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, body: "empty", fetched: "08:12"}, true)
			}},
		{"auth", "gh exit 4, or exit 1 with HTTP 401.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, body: "auth"}, true)
			}},
		{"network", "gh exit 1 without 401: the last good rows stay, under their own timestamp.",
			func(v string) string {
				return dashboard(workingSet(false), section{variant: v, pulls: live(), cursor: -1,
					body: "network", fetched: "14:31"}, true)
			}},
	}
}

func main() {
	variant := flag.String("variant", "", "A, B or C; empty draws all three")
	only := flag.String("case", "", "one case by name; empty draws all")
	marks := flag.String("marks", "failures", "failures, quiet or literal — three readings of #70's mark table")
	stack := flag.String("stack", "bare", "bare or number — how much the stack mark says")
	check := flag.Bool("check", false, "report any line wider than the sidepanel instead of drawing")
	flag.Parse()
	markSet, stackMarks = *marks, *stack
	// Piped to a file, lipgloss would drop every colour it is here to show.
	lipgloss.SetColorProfile(termenv.TrueColor)

	if *check {
		overrun := 0
		for _, v := range []string{"A", "B", "C"} {
			for _, c := range cases() {
				for i, line := range strings.Split(c.draw(v), "\n") {
					if w := ansi.StringWidth(line); w > width {
						overrun++
						fmt.Printf("%s/%s line %d: %d columns\n%s\n", v, c.name, i+1, w, line)
					}
				}
			}
		}
		fmt.Printf("%d lines over %d columns\n", overrun, width)
		return
	}

	variants := []string{"A", "B", "C"}
	if *variant != "" {
		variants = []string{strings.ToUpper(*variant)}
	}
	titles := map[string]string{
		"A": "A — identity left, state right (no title)",
		"B": "B — a state column on the left (no title)",
		"C": "C — two lines per Pull, title on the second",
	}

	for _, v := range variants {
		fmt.Printf("\n%s\n%s\n", brandStyle.Render(titles[v]), ruleStyle.Render(strings.Repeat("═", width)))
		for _, c := range cases() {
			if *only != "" && c.name != *only {
				continue
			}
			fmt.Printf("\n%s  %s\n%s\n%s\n",
				repoStyle.Render(c.name), quietStyle.Render(c.says),
				ruleStyle.Render(strings.Repeat("╌", width)), c.draw(v))
		}
	}
}
