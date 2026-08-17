// Package selector decides which card a stage works on next.
package selector

import (
	"strconv"
	"strings"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
)

// ParseBlockedBy reads the board's "Blocked by" field.
//
// The field is free text a human types, in the form `ready` (or empty) for
// nothing blocking, or a comma-separated list like
// `postfix-client#1, postfix-client#3`. Anything that does not parse is
// returned in bad so the caller can warn about it: a typo must not silently
// read as "nothing is blocking this".
func ParseBlockedBy(defaultOwner, defaultRepo, value string) (refs []board.BlockerRef, bad []string) {
	v := strings.TrimSpace(value)
	if v == "" || strings.EqualFold(v, "ready") || strings.EqualFold(v, "none") {
		return nil, nil
	}

	for _, part := range strings.Split(v, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		// Accept `repo#N`, `owner/repo#N` and a bare `#N` meaning this repo.
		hash := strings.LastIndex(entry, "#")
		if hash < 0 {
			bad = append(bad, entry)
			continue
		}
		left := strings.TrimSpace(entry[:hash])
		right := strings.TrimSpace(entry[hash+1:])

		number, err := strconv.Atoi(right)
		if err != nil || number <= 0 {
			bad = append(bad, entry)
			continue
		}

		owner, repo := defaultOwner, left
		if slash := strings.Index(left, "/"); slash >= 0 {
			owner, repo = left[:slash], left[slash+1:]
		}
		if strings.TrimSpace(repo) == "" {
			// A bare `#N` means an issue in the card's own repository.
			repo = defaultRepo
		}
		if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
			bad = append(bad, entry)
			continue
		}
		refs = append(refs, board.BlockerRef{Owner: owner, Repo: repo, Number: number})
	}
	return refs, bad
}
