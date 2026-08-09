// Package ticket is the JIRA ticket a Session's work is about: an ID and a
// link, and nothing else, ever.
//
// There is no JIRA API here and there is not going to be one. A ticket's title,
// status and assignee are all one browser tab away, and every one of them would
// cost the harness a credential to keep, a network call in the Dashboard's
// redraw, and a way to be wrong about somebody else's system. The ID is what
// tells two Sessions apart at a glance, and the link is what gets you the rest.
package ticket

import "regexp"

// Key is a JIRA ticket's ID — a project and a number, the way JIRA writes it.
// The empty Key is a Session with no ticket, which is a thing a Session is
// allowed to be.
type Key string

// browse is where a ticket is read. The host is Teamleader's, which is the one
// this harness was built for; a second one would be configuration, and there is
// no second one.
const browse = "https://teamleader.atlassian.net/browse/"

// URL is where the ticket is read, which the harness opens rather than fetches.
func (k Key) URL() string { return browse + string(k) }

// named is the shape of a JIRA key: a project starting with a letter and
// carrying letters or digits, then a number.
//
// Upper case is load-bearing, not decoration. Read case-insensitively, this
// pattern finds a ticket in every dependency bump that ever branched —
// renovate/lodash-4.17.21 — and a row confidently showing LODASH-4 is worse
// than one showing no ticket at all.
var named = regexp.MustCompile(`[A-Z][A-Z0-9]*-\d+`)

// In returns the first ticket named in text, and whether there was one. First
// rather than only: a branch can mention a second ticket it reverts or follows,
// and the one the work is about is the one it opens with.
func In(text string) (Key, bool) {
	found := named.FindString(text)
	return Key(found), found != ""
}
