package selector

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// ClaimPrefix is the label prefix the old slash-commands used to claim an
// issue. The workers do not claim anything — one replica per stage and
// disjoint statuses mean there is nothing to race against — but they do stand
// aside for a claim, so a human running /gh-plan by hand is not trampled.
const ClaimPrefix = "picked-by-"

// Candidates filters the board down to the items a stage may work on, without
// the blocked-by check. That check costs an API call per card, so it runs
// separately, on the survivors only.
func Candidates(items []board.Item, status string, cfg config.Config, skip func(board.Item) bool) []board.Item {
	var out []board.Item
	for _, it := range items {
		switch {
		case !it.IsIssue:
			continue // draft card or pull request
		case it.Archived:
			continue
		case !strings.EqualFold(it.Status, status):
			continue
		case !strings.EqualFold(it.State, "OPEN"):
			continue
		case it.HasLabel(cfg.NeedsHumanLabel):
			// A decided rejection. It stays out until a human clears the
			// label; without this the worker would re-reject and re-comment on
			// the same card on every poll, forever.
			continue
		case it.HasLabelPrefix(ClaimPrefix):
			continue
		case skip != nil && skip(it):
			continue
		}
		out = append(out, it)
	}
	return out
}

var phaseNum = regexp.MustCompile(`^[Mm](\d+)`)

// phaseRank turns "M3 Compose" into 3. An unset or unrecognised phase sorts
// last, behind every numbered milestone.
func phaseRank(phase string) int {
	m := phaseNum.FindStringSubmatch(strings.TrimSpace(phase))
	if m == nil {
		return 1 << 30
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 1 << 30
	}
	return n
}

// sizeRank orders S before M before L. Anything else sorts last.
func sizeRank(size string) int {
	switch strings.ToUpper(strings.TrimSpace(size)) {
	case "S":
		return 0
	case "M":
		return 1
	case "L":
		return 2
	default:
		return 3
	}
}

// Order sorts candidates the way gh-request step 1.2 did: earliest milestone
// first, then the foundational tracks ahead of feature tracks, then the
// smallest card, then the lowest issue number as a stable tiebreak.
//
// The point is that groundwork lands before the things built on top of it,
// and that a run is reproducible: the same board always yields the same pick.
func Order(items []board.Item, cfg config.Config) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]

		if ra, rb := phaseRank(a.Phase), phaseRank(b.Phase); ra != rb {
			return ra < rb
		}
		pa, pb := cfg.IsPriorityTrack(a.Track), cfg.IsPriorityTrack(b.Track)
		if pa != pb {
			return pa
		}
		if ra, rb := sizeRank(a.Size), sizeRank(b.Size); ra != rb {
			return ra < rb
		}
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		return a.NameWithOwner() < b.NameWithOwner()
	})
}
