package stage

import (
	"context"
	"fmt"
	"os"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
)

// reportDryRun prints what a stage would do to the card it picked, and changes
// nothing.
//
// Output goes to stdout as plain text rather than through the structured
// logger: a dry run is read by a person at a terminal deciding whether to let
// the thing loose, and a JSON log line is the wrong shape for that.
func reportDryRun(ctx context.Context, r *Runner, item board.Item, from, to string, describe func() (string, error)) error {
	w := os.Stdout

	fmt.Fprintf(w, "\n%s  #%d  %s\n", item.NameWithOwner(), item.Number, item.Title)
	fmt.Fprintf(w, "  url        %s\n", item.URL)
	fmt.Fprintf(w, "  fields     phase=%q track=%q size=%q blockedBy=%q\n",
		item.Phase, item.Track, item.Size, item.BlockedBy)
	fmt.Fprintf(w, "  labels     %v\n", item.Labels)
	fmt.Fprintf(w, "  transition %s -> %s\n", from, to)

	if open, err := r.openBlockers(ctx, item); err != nil {
		fmt.Fprintf(w, "  blockers   could not be read: %v\n", err)
	} else if len(open) > 0 {
		fmt.Fprintf(w, "  blockers   still open: %v\n", open)
	} else {
		fmt.Fprintf(w, "  blockers   none\n")
	}

	action, err := describe()
	if err != nil {
		fmt.Fprintf(w, "  action     could not be determined: %v\n", err)
		return nil
	}
	fmt.Fprintf(w, "  action     %s\n\n", action)
	return nil
}
