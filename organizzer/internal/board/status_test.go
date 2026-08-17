package board

import (
	"context"
	"errors"
	"log/slog"
	"io"
	"testing"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

func quietClient() *Client {
	return New(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testMeta() ProjectMeta {
	return ProjectMeta{
		ProjectID:     "PVT_test",
		StatusFieldID: "PVTSSF_test",
		StatusOptions: map[string]string{
			config.StatusNew:     "opt-new",
			config.StatusRequest: "opt-request",
			config.StatusPlan:    "opt-plan",
			config.StatusReview:  "opt-review",
			config.StatusApply:   "opt-apply",
			config.StatusTest:    "opt-test",
		},
	}
}

// The two approval transitions belong to a human: moving a card from `request`
// to `plan` approves an outline, and from `review` to `apply` approves a plan.
// The guard lives at the single mutation that changes the board, so a stage
// that grows a bug and asks for `apply` gets an error rather than an approval.
//
// This test makes no network call: the guard has to reject before the request
// is built, or it is not a guard.
func TestSetStatusRefusesHumanCheckpoints(t *testing.T) {
	c := quietClient()
	meta := testMeta()
	item := Item{ID: "PVTI_x", Number: 1, Owner: "goodtribes-org", Repo: "kickfix", Status: "request"}

	for _, target := range []string{config.StatusPlan, config.StatusApply, "PLAN", " Apply "} {
		err := c.SetStatus(context.Background(), meta, item, target)
		var checkpoint *ErrHumanCheckpoint
		if !errors.As(err, &checkpoint) {
			t.Errorf("SetStatus(%q) = %v, want ErrHumanCheckpoint", target, err)
		}
	}
}

func TestIsHumanCheckpoint(t *testing.T) {
	allowed := []string{config.StatusNew, config.StatusRequest, config.StatusReview, config.StatusTest}
	for _, s := range allowed {
		if IsHumanCheckpoint(s) {
			t.Errorf("%q should be a worker-writable status", s)
		}
	}
	for _, s := range []string{config.StatusPlan, config.StatusApply} {
		if !IsHumanCheckpoint(s) {
			t.Errorf("%q must be a human checkpoint", s)
		}
	}
}

func TestSetStatusRejectsUnknownStatus(t *testing.T) {
	c := quietClient()
	item := Item{ID: "PVTI_x", Number: 1}
	err := c.SetStatus(context.Background(), testMeta(), item, "merged")
	if err == nil {
		t.Fatal("SetStatus accepted a status the board does not define")
	}
}

func TestOptionIDIsCaseInsensitive(t *testing.T) {
	meta := testMeta()
	got, err := meta.OptionID("Request")
	if err != nil {
		t.Fatalf("OptionID: %v", err)
	}
	if got != "opt-request" {
		t.Fatalf("OptionID = %q, want opt-request", got)
	}
}
