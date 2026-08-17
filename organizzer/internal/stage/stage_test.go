package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/llm"
	"github.com/goodtribes-org/agent/organizzer/internal/render"
	"github.com/goodtribes-org/agent/organizzer/internal/verdict"
)

func card(status string) board.Item {
	return board.Item{
		ID: "PVTI_x", IsIssue: true, State: "OPEN", IssueID: "I_x",
		Owner: "goodtribes-org", Repo: "kickfix", Number: 42,
		Title: "Add keyboard navigation", Status: status,
		Phase: "M2 Mail client", Track: "Mail", Size: "S", BlockedBy: "ready",
	}
}

func issueWith(comments ...string) board.Issue {
	is := board.Issue{ID: "I_x", Number: 42, Title: "Add keyboard navigation",
		URL: "https://github.com/goodtribes-org/kickfix/issues/42", State: "OPEN"}
	for i, c := range comments {
		is.Comments = append(is.Comments, board.Comment{ID: string(rune('a' + i)), Body: c})
	}
	return is
}

func goodRequestVerdict() map[string]any {
	return map[string]any{
		"sub_project": "kickfix",
		"scope": map[string]any{
			"too_large": false, "estimated_files": 2, "label": "Small",
		},
		"sensitive_data":  map[string]any{"found": false},
		"stack_check":     map[string]any{"passes": true},
		"invariant_check": map[string]any{"passes": true},
		"context":         "The inbox is mouse-only.",
		"steps":           []string{"add a handler"},
		"files":           []map[string]string{{"path": "src/app/inbox/page.tsx", "why": "the list"}},
		"testing":         []string{"npm run lint && npm run build"},
	}
}

func goodPlanVerdict() map[string]any {
	return map[string]any{
		"sub_project": "kickfix", "scope_label": "Small", "scope_files": 1,
		"background": "The inbox is mouse-only.",
		"steps": []map[string]any{
			{"file": "src/app/inbox/page.tsx", "change": "add a keydown handler"},
		},
		"verification": []string{"npm run lint && npm run build"},
	}
}

// --- request ---------------------------------------------------------------

func TestRequestHappyPathCommentsThenMoves(t *testing.T) {
	gh := &fakeBoard{issue: issueWith(), repoTree: []string{"src/app/inbox/page.tsx"}}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: goodRequestVerdict()}

	if err := (Request{}).Process(context.Background(), r, card(config.StatusNew)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !gh.commentsContain(render.SentinelOutline) {
		t.Error("no outline was posted")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want request", got)
	}
	if !gh.hasLabel(board.LabelRequest) {
		t.Error("the request label was not added")
	}
}

// The comment has to be live before the card moves. A crash in between then
// leaves the card in its input column with the artifact already posted, and
// the sentinel makes the redo free. The reverse order can strand a card in a
// column with no artifact and no worker that reads it.
func TestRequestPostsTheOutlineBeforeMovingTheCard(t *testing.T) {
	gh := &fakeBoard{issue: issueWith(), failStatus: true}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: goodRequestVerdict()}

	err := (Request{}).Process(context.Background(), r, card(config.StatusNew))
	if err == nil {
		t.Fatal("Process succeeded despite the status update failing")
	}
	if !gh.commentsContain(render.SentinelOutline) {
		t.Error("the outline should already be posted when the move fails")
	}
	if gh.lastStatus() != "" {
		t.Errorf("card moved to %q even though the update failed", gh.lastStatus())
	}
}

// A crashed run left the outline but not the move. The redo must finish the
// transition without paying for the model again.
func TestRequestIsIdempotentViaTheSentinel(t *testing.T) {
	gh := &fakeBoard{issue: issueWith("## Request Outline\n\n" + render.SentinelOutline)}
	r := newTestRunner(config.StageRequest, gh)
	llm := &fakeLLM{reply: goodRequestVerdict()}
	r.LLM = llm

	if err := (Request{}).Process(context.Background(), r, card(config.StatusNew)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("the model was called %d times for work already done", llm.calls)
	}
	if len(gh.comments) != 0 {
		t.Errorf("a second outline was posted: %v", gh.comments)
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want the transition completed", got)
	}
}

// A rejection has to stick. Without the needs-human label the selector picks
// the card up on the next poll, reaches the same conclusion and comments
// again — every ten seconds, spending tokens each time.
func TestRequestTooLargeStaysInNewAndMarksForAHuman(t *testing.T) {
	v := goodRequestVerdict()
	v["scope"] = map[string]any{
		"too_large": true, "label": "Medium",
		"reasons":          []string{"touches the whole store layer"},
		"split_suggestion": []string{"split the migration out first"},
	}
	gh := &fakeBoard{issue: issueWith()}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: v}

	if err := (Request{}).Process(context.Background(), r, card(config.StatusNew)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !gh.commentsContain("Too large for one issue") {
		t.Error("no rejection comment was posted")
	}
	if !gh.hasLabel("needs-human") {
		t.Error("a rejected card must carry needs-human or it will be re-rejected forever")
	}
	// The card is already in `new`; there is nothing to move.
	if gh.lastStatus() != "" {
		t.Errorf("card moved to %q, want it left in new", gh.lastStatus())
	}
}

func TestRequestUnknownRepoIsRejected(t *testing.T) {
	gh := &fakeBoard{issue: issueWith()}
	r := newTestRunner(config.StageRequest, gh)
	llm := &fakeLLM{reply: goodRequestVerdict()}
	r.LLM = llm

	item := card(config.StatusNew)
	item.Repo = "some-other-repo"

	if err := (Request{}).Process(context.Background(), r, item); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if llm.calls != 0 {
		t.Error("the repository check should happen before the model is called")
	}
	if !gh.commentsContain("Unknown sub-project") {
		t.Error("no rejection comment was posted")
	}
	if !gh.hasLabel("needs-human") {
		t.Error("needs-human was not applied")
	}
}

// An unusable model reply is transient. Nothing is posted, nothing moves, and
// the loop's backoff brings the card round again later.
func TestRequestBadVerdictChangesNothing(t *testing.T) {
	bad := goodRequestVerdict()
	bad["steps"] = []string{} // fails validation

	gh := &fakeBoard{issue: issueWith()}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: bad}

	err := (Request{}).Process(context.Background(), r, card(config.StatusNew))
	if err == nil {
		t.Fatal("Process accepted an invalid verdict")
	}
	if len(gh.comments) != 0 || gh.lastStatus() != "" {
		t.Errorf("an unusable verdict changed the issue: comments=%v status=%q",
			gh.comments, gh.lastStatus())
	}
}

func TestRequestLLMErrorChangesNothing(t *testing.T) {
	gh := &fakeBoard{issue: issueWith()}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{err: errors.New("berget is down")}

	if err := (Request{}).Process(context.Background(), r, card(config.StatusNew)); err == nil {
		t.Fatal("Process succeeded despite the model being unreachable")
	}
	if len(gh.comments) != 0 || gh.lastStatus() != "" {
		t.Error("a model outage changed the issue")
	}
}

// --- plan ------------------------------------------------------------------

func TestPlanHappyPath(t *testing.T) {
	gh := &fakeBoard{
		issue:    issueWith("## Request Outline\n\n" + render.SentinelOutline),
		repoTree: []string{"src/app/inbox/page.tsx"},
	}
	r := newTestRunner(config.StagePlan, gh)
	r.LLM = &fakeLLM{reply: goodPlanVerdict()}

	if err := (Plan{}).Process(context.Background(), r, card(config.StatusPlan)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !gh.commentsContain(render.SentinelPlan) {
		t.Error("no plan was posted")
	}
	if got := gh.lastStatus(); got != config.StatusReview {
		t.Errorf("card ended at %q, want review", got)
	}
}

// A human can drag a blocked card straight into `plan`, so the check runs
// again there. Unlike the request stage it comments — a person made that move
// and should be told why it bounced.
func TestPlanBouncesABlockedCardBackToRequest(t *testing.T) {
	gh := &fakeBoard{
		issue:       issueWith(),
		blockerOpen: map[string]bool{"asylguiden.se#1": true},
	}
	r := newTestRunner(config.StagePlan, gh)
	llm := &fakeLLM{reply: goodPlanVerdict()}
	r.LLM = llm

	item := card(config.StatusPlan)
	item.BlockedBy = "asylguiden.se#1"

	if err := (Plan{}).Process(context.Background(), r, item); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if llm.calls != 0 {
		t.Error("the model was called for a blocked card")
	}
	if !gh.commentsContain("Still blocked") {
		t.Error("no explanation was posted")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want request", got)
	}
}

func TestPlanBlockedVerdictReturnsTheCard(t *testing.T) {
	gh := &fakeBoard{issue: issueWith(), repoTree: []string{"src/app/inbox/page.tsx"}}
	r := newTestRunner(config.StagePlan, gh)
	r.LLM = &fakeLLM{reply: map[string]any{
		"blocked": true, "blocked_reason": "the issue does not say which mailbox",
	}}

	if err := (Plan{}).Process(context.Background(), r, card(config.StatusPlan)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !gh.commentsContain("Cannot write a plan") {
		t.Error("no explanation was posted")
	}
	if !gh.hasLabel("needs-human") {
		t.Error("needs-human was not applied")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want request", got)
	}
}

// A plan that runs out of tokens is the model's own decision, not a hiccup.
// Treated as a transient error the card backs off, is picked up again, produces
// the same overlong answer and fails identically — forever, at plan-model
// prices, until a person happens to look at the log.
func TestPlanTooLongIsHandedToAHumanNotRetriedForever(t *testing.T) {
	c := card(config.StatusPlan)
	gh := &fakeBoard{
		items:    []board.Item{c},
		issue:    issueWith(render.SentinelOutline),
		repoTree: []string{"src/app/inbox/page.tsx"},
	}
	r := newTestRunner(config.StagePlan, gh)
	r.LLM = &fakeLLM{err: llm.ErrTruncated}

	if err := (Plan{}).Process(context.Background(), r, c); err != nil {
		t.Fatalf("truncation surfaced as an error instead of a decision: %v", err)
	}
	if !gh.commentsContain("did not fit") {
		t.Error("nothing was posted to say why the plan never appeared")
	}
	if !gh.hasLabel(r.Cfg.NeedsHumanLabel) {
		t.Error("no needs-human label, so the selector picks the card up again next poll")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want it handed back to request", got)
	}
	if gh.commentsContain(render.SentinelPlan) {
		t.Error("a truncated plan was posted as though it were finished")
	}
}

func TestPlanDropsStepsNamingFilesThatDoNotExist(t *testing.T) {
	v := goodPlanVerdict()
	v["steps"] = []map[string]any{
		{"file": "src/app/inbox/page.tsx", "change": "add a keydown handler"},
		{"file": "src/invented.ts", "change": "edit it"},
	}
	gh := &fakeBoard{issue: issueWith(), repoTree: []string{"src/app/inbox/page.tsx"}}
	r := newTestRunner(config.StagePlan, gh)
	r.LLM = &fakeLLM{reply: v}

	if err := (Plan{}).Process(context.Background(), r, card(config.StatusPlan)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	posted := strings.Join(gh.comments, "\n")
	if strings.Contains(posted, "src/invented.ts") {
		t.Error("a step naming a file that does not exist reached the plan")
	}
	if !strings.Contains(posted, "src/app/inbox/page.tsx") {
		t.Error("the real step was dropped too")
	}
}

// --- apply -----------------------------------------------------------------

func TestApplyDispatchesAndMovesToTest(t *testing.T) {
	gh := &fakeBoard{issue: issueWith("## Implementation Plan\n\nstep one\n\n" + render.SentinelPlan)}
	r := newTestRunner(config.StageApply, gh)
	fnd := &fakeFoundry{}
	r.Fnd = fnd

	if err := (Apply{}).Process(context.Background(), r, card(config.StatusApply)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(fnd.submits) != 1 {
		t.Fatalf("submitted %d tasks, want 1", len(fnd.submits))
	}
	if !strings.Contains(fnd.submits[0].Task, "step one") {
		t.Error("the approved plan did not reach the agent")
	}
	if !gh.commentsContain(render.SentinelHandoff) {
		t.Error("no hand-off comment was posted")
	}
	if got := gh.lastStatus(); got != config.StatusTest {
		t.Errorf("card ended at %q, want test", got)
	}
}

// The webhook names every task task-<unixMilli>, so a duplicate submission is
// a second agent run rather than something the server can reject: two
// sandboxes, two pull requests, twice the spend. The sentinel is the only
// guard, which makes this the most load-bearing test here.
func TestApplyNeverDispatchesTwice(t *testing.T) {
	gh := &fakeBoard{issue: issueWith(
		"## Implementation Plan\n\nstep one\n\n"+render.SentinelPlan,
		"## Implementation dispatched\n\n"+render.SentinelHandoff,
	)}
	r := newTestRunner(config.StageApply, gh)
	fnd := &fakeFoundry{}
	r.Fnd = fnd

	if err := (Apply{}).Process(context.Background(), r, card(config.StatusApply)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(fnd.submits) != 0 {
		t.Fatalf("dispatched %d more agent runs for work already sent", len(fnd.submits))
	}
	if got := gh.lastStatus(); got != config.StatusTest {
		t.Errorf("card ended at %q, want the transition completed", got)
	}
}

func TestApplyWithNoPlanReturnsTheCardToReview(t *testing.T) {
	gh := &fakeBoard{issue: issueWith("just some chatter")}
	r := newTestRunner(config.StageApply, gh)
	fnd := &fakeFoundry{}
	r.Fnd = fnd

	if err := (Apply{}).Process(context.Background(), r, card(config.StatusApply)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(fnd.submits) != 0 {
		t.Error("something was dispatched without an approved plan")
	}
	if !gh.commentsContain("No implementation plan found") {
		t.Error("no explanation was posted")
	}
	if got := gh.lastStatus(); got != config.StatusReview {
		t.Errorf("card ended at %q, want review", got)
	}
}

func TestApplyDispatchFailureIsReportedAndNotRetried(t *testing.T) {
	gh := &fakeBoard{issue: issueWith("## Implementation Plan\n\nstep one\n\n" + render.SentinelPlan)}
	r := newTestRunner(config.StageApply, gh)
	r.Fnd = &fakeFoundry{err: errors.New("connection refused")}

	if err := (Apply{}).Process(context.Background(), r, card(config.StatusApply)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !gh.commentsContain("Could not dispatch the implementation") {
		t.Error("the failure was not reported on the issue")
	}
	if !gh.commentsContain("No agent was started") {
		t.Error("the comment must say plainly that nothing was started")
	}
	if !gh.hasLabel("needs-human") {
		t.Error("needs-human was not applied, so the card would be retried unattended")
	}
	if got := gh.lastStatus(); got != config.StatusReview {
		t.Errorf("card ended at %q, want review", got)
	}
}

// --- the human checkpoints -------------------------------------------------

// Moving a card from `request` to `plan` approves an outline; from `review` to
// `apply` approves a plan. No worker may do either. This asserts it at the
// level that matters: run every stage over every column and check that neither
// value is ever a target.
func TestNoStageEverTargetsAHumanCheckpoint(t *testing.T) {
	stages := []Stage{Request{}, Plan{}, Apply{}}

	for _, s := range stages {
		for _, from := range config.AllStatuses {
			gh := &fakeBoard{
				issue: issueWith(
					"## Request Outline\n\n"+render.SentinelOutline,
					"## Implementation Plan\n\nstep one\n\n"+render.SentinelPlan,
				),
				repoTree: []string{"src/app/inbox/page.tsx"},
			}
			r := newTestRunner(s.Name(), gh)
			r.LLM = &fakeLLM{reply: goodPlanVerdict()}
			r.Fnd = &fakeFoundry{}
			if s.Name() == config.StageRequest {
				r.LLM = &fakeLLM{reply: goodRequestVerdict()}
			}

			_ = s.Process(context.Background(), r, card(from))

			for _, target := range gh.statusSet {
				if board.IsHumanCheckpoint(target) {
					t.Errorf("%s stage moved a card from %q to %q, which is a human checkpoint",
						s.Name(), from, target)
				}
			}
		}
	}
}

func TestStagesReadTheColumnsTheyShould(t *testing.T) {
	tests := []struct {
		stage       Stage
		from        string
		ordered     bool
		skipBlocked bool
	}{
		{Request{}, config.StatusNew, true, true},
		{Plan{}, config.StatusPlan, false, false},
		{Apply{}, config.StatusApply, false, false},
	}
	for _, tc := range tests {
		if got := tc.stage.From(); got != tc.from {
			t.Errorf("%s reads %q, want %q", tc.stage.Name(), got, tc.from)
		}
		if got := tc.stage.Ordered(); got != tc.ordered {
			t.Errorf("%s Ordered() = %v, want %v", tc.stage.Name(), got, tc.ordered)
		}
		if got := tc.stage.SkipBlocked(); got != tc.skipBlocked {
			t.Errorf("%s SkipBlocked() = %v, want %v", tc.stage.Name(), got, tc.skipBlocked)
		}
	}
}

// A verdict is only ever acted on after Go has checked it. This pins the
// boundary: the stage packages must not reach into a verdict without Validate.
func TestVerdictValidationIsWiredIn(t *testing.T) {
	var v verdict.RequestVerdict
	if err := v.Validate("kickfix"); err == nil {
		t.Fatal("an empty verdict validated; the stages rely on this rejecting")
	}
}

// --- the loop ---------------------------------------------------------------

// A blocked card at the head of the queue must not starve the stage. The
// ordering is stable, so stopping at the first blocked card means the same
// card sits at the head on every poll and nothing behind it is ever worked.
func TestBlockedCardAtTheHeadDoesNotStarveTheQueue(t *testing.T) {
	blocked := card(config.StatusNew)
	blocked.Number = 5
	blocked.Size = "S" // sorts first
	blocked.BlockedBy = "kickfix#4"

	ready := card(config.StatusNew)
	ready.ID = "PVTI_ready"
	ready.Number = 7
	ready.Size = "M" // sorts second
	ready.BlockedBy = "ready"

	gh := &fakeBoard{
		items:       []board.Item{blocked, ready},
		issue:       issueWith(),
		repoTree:    []string{"src/app/inbox/page.tsx"},
		blockerOpen: map[string]bool{"kickfix#4": true},
	}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: goodRequestVerdict()}

	worked, err := r.cycle(context.Background(), Request{})
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if !worked {
		t.Fatal("the cycle did no work; the blocked card starved the queue")
	}
	if !gh.commentsContain(render.SentinelOutline) {
		t.Error("the unblocked card behind the blocked one was not outlined")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want request", got)
	}
}

// The bound on blocker checks must not become the starvation point in its own
// right. This is the live board: 101 candidates in `new`, the first twenty
// blocked, the workable card behind them. One poll cannot reach it — but the
// scan window has to move so the next one does, rather than re-checking the
// same twenty until a human closes something.
func TestBlockedRunLongerThanTheCheckBudgetDoesNotStarveTheQueue(t *testing.T) {
	var items []board.Item
	for i := 0; i < 20; i++ {
		c := card(config.StatusNew)
		c.ID = fmt.Sprintf("PVTI_blocked_%02d", i)
		c.Number = 100 + i
		c.BlockedBy = "kickfix#4"
		items = append(items, c)
	}
	ready := card(config.StatusNew)
	ready.ID = "PVTI_ready"
	ready.Number = 900 // sorts last, behind every blocked card
	ready.BlockedBy = "ready"
	items = append(items, ready)

	gh := &fakeBoard{
		items:       items,
		issue:       issueWith(),
		repoTree:    []string{"src/app/inbox/page.tsx"},
		blockerOpen: map[string]bool{"kickfix#4": true},
	}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: goodRequestVerdict()}

	// First poll: the whole check budget goes on blocked cards.
	worked, err := r.cycle(context.Background(), Request{})
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if worked {
		t.Fatal("first cycle claimed work; the budget should have run out first")
	}
	if !r.sweeping {
		t.Error("the stage did not record that candidates were left unexamined")
	}
	if r.scanFrom == 0 {
		t.Fatal("the scan window did not move; the next poll would re-check the same cards")
	}

	// Second poll: resumes past them and finds the workable card.
	worked, err = r.cycle(context.Background(), Request{})
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if !worked {
		t.Fatal("the card behind the blocked run was never reached")
	}
	if !gh.commentsContain(render.SentinelOutline) {
		t.Error("no outline was posted for the workable card")
	}
	if r.scanFrom != 0 {
		t.Errorf("scan window stayed at %d after a pick, want a reset to priority order", r.scanFrom)
	}
}

// A column where everything is blocked is not a busy one. Once the sweep has
// been all the way round without finding anything, it has to drop back to the
// idle interval — otherwise it re-checks twenty blockers every ten seconds
// forever, spending the rate limit the actual work needs.
func TestAFullSweepWithNothingWorkableGoesBackToIdle(t *testing.T) {
	var items []board.Item
	for i := 0; i < 25; i++ {
		c := card(config.StatusNew)
		c.ID = fmt.Sprintf("PVTI_blocked_%02d", i)
		c.Number = 100 + i
		c.BlockedBy = "kickfix#4"
		items = append(items, c)
	}

	gh := &fakeBoard{
		items:       items,
		issue:       issueWith(),
		blockerOpen: map[string]bool{"kickfix#4": true},
	}
	r := newTestRunner(config.StageRequest, gh)
	r.LLM = &fakeLLM{reply: goodRequestVerdict()}

	// First poll covers 20 of 25 — more to look at, so keep the busy pace.
	if _, err := r.cycle(context.Background(), Request{}); err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if !r.sweeping {
		t.Fatal("stage went idle with candidates still unexamined")
	}

	// Second poll closes the lap: every card has now been seen and none is
	// workable.
	if _, err := r.cycle(context.Background(), Request{}); err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if r.sweeping {
		t.Error("stage kept the busy interval after a full sweep found nothing")
	}
	if len(gh.comments) != 0 {
		t.Error("a blocked card was worked anyway")
	}
}

func TestEveryCandidateBlockedIsIdleNotAnError(t *testing.T) {
	c := card(config.StatusNew)
	c.BlockedBy = "kickfix#4"

	gh := &fakeBoard{
		items:       []board.Item{c},
		issue:       issueWith(),
		blockerOpen: map[string]bool{"kickfix#4": true},
	}
	r := newTestRunner(config.StageRequest, gh)
	llm := &fakeLLM{reply: goodRequestVerdict()}
	r.LLM = llm

	worked, err := r.cycle(context.Background(), Request{})
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if worked {
		t.Error("cycle reported work despite every candidate being blocked")
	}
	if llm.calls != 0 || len(gh.comments) != 0 {
		t.Error("a blocked card was worked anyway")
	}
}

// The plan and apply stages must not skip blocked cards in the loop — they
// handle them inside Process, by commenting and handing the card back. A card
// silently skipped there would sit in an approval column with nobody told why.
func TestLaterStagesDoNotSilentlySkipBlockedCards(t *testing.T) {
	c := card(config.StatusPlan)
	c.BlockedBy = "kickfix#4"

	gh := &fakeBoard{
		items:       []board.Item{c},
		issue:       issueWith(),
		blockerOpen: map[string]bool{"kickfix#4": true},
	}
	r := newTestRunner(config.StagePlan, gh)
	r.LLM = &fakeLLM{reply: goodPlanVerdict()}

	if _, err := r.cycle(context.Background(), Plan{}); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if !gh.commentsContain("Still blocked") {
		t.Error("a blocked card in plan was skipped instead of being explained and returned")
	}
	if got := gh.lastStatus(); got != config.StatusRequest {
		t.Errorf("card ended at %q, want request", got)
	}
}
