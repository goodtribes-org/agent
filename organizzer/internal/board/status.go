package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// humanCheckpoints are the two transitions a human owns: moving a card from
// `request` to `plan` approves an outline, and moving it from `review` to
// `apply` approves a plan. No worker may ever set a card to either value.
//
// This is enforced here, at the single mutation that changes the board, rather
// than trusted to each stage. A stage that grows a bug and asks for `apply`
// gets an error, not an approval.
var humanCheckpoints = map[string]bool{
	config.StatusPlan:  true,
	config.StatusApply: true,
}

// IsHumanCheckpoint reports whether a status is one only a human may move a
// card into. Exported so tests and fakes can assert the same rule the real
// client enforces.
func IsHumanCheckpoint(status string) bool {
	return humanCheckpoints[strings.ToLower(strings.TrimSpace(status))]
}

// ErrHumanCheckpoint is returned when something asks to move a card into a
// column only a human may move it into.
type ErrHumanCheckpoint struct{ Target string }

func (e *ErrHumanCheckpoint) Error() string {
	return fmt.Sprintf("refusing to move a card to %q: that transition is a human checkpoint", e.Target)
}

// SetStatus moves a card to a column.
//
// It returns ErrHumanCheckpoint for the two approval columns, and an error for
// any status the board does not define. A caller that gets an error has not
// moved anything.
func (c *Client) SetStatus(ctx context.Context, meta ProjectMeta, item Item, target string) error {
	target = strings.ToLower(strings.TrimSpace(target))

	if IsHumanCheckpoint(target) {
		return &ErrHumanCheckpoint{Target: target}
	}
	optionID, err := meta.OptionID(target)
	if err != nil {
		return err
	}

	err = c.graphql(ctx, mSetStatus, map[string]any{
		"project": meta.ProjectID,
		"item":    item.ID,
		"field":   meta.StatusFieldID,
		"option":  optionID,
	}, nil)
	if err != nil {
		return fmt.Errorf("move %s to %s: %w", item.Key(), target, err)
	}

	c.log.Info("card moved", "issue", item.Key(), "from", item.Status, "to", target)
	return nil
}
