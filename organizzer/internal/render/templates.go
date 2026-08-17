// Package render produces the markdown the workers post on issues.
//
// The two artifact comments reproduce the slash-commands' formats exactly.
// That is not nostalgia: the trailing sentinel line of each is a contract.
// The apply stage finds the approved plan by searching for SentinelPlan, and
// every stage decides whether it has already run by looking for its own
// sentinel. Change one of these strings and a card silently gets worked twice.
package render

// Sentinels. Byte-exact, em dashes and all.
const (
	// SentinelOutline closes a request outline.
	SentinelOutline = "*Outline written by /gh-request — move card to 'plan' to approve and trigger detailed planning.*"

	// SentinelPlan closes an implementation plan. The apply stage searches
	// issue comments for exactly this string to find the approved plan.
	SentinelPlan = "*Plan written by /gh-plan — move card to 'apply' to begin implementation.*"

	// SentinelHandoff closes the comment posted when work has been dispatched
	// to the implementation agent. It is the apply stage's own idempotency
	// marker — and the only one, because the webhook generates its own task
	// name and so cannot reject a duplicate submission.
	SentinelHandoff = "*Implementation dispatched by organizzer — card moved to 'test'.*"
)

// Headings the stages search for when reading each other's work.
const (
	HeadingOutline = "## Request Outline"
	HeadingPlan    = "## Implementation Plan"
)
