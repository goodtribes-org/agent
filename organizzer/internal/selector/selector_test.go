package selector

import (
	"testing"

	"github.com/goodtribes-org/agent/organizzer/internal/board"
	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

func testCfg() config.Config {
	return config.Config{
		AllowedRepos:    []string{"kickfix", "asylguiden.se"},
		PriorityTracks:  []string{"Foundation", "Infra"},
		NeedsHumanLabel: "needs-human",
	}
}

func TestParseBlockedBy(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantRefs []board.BlockerRef
		wantBad  []string
	}{
		{name: "empty means ready", value: ""},
		{name: "the literal ready", value: "ready"},
		{name: "ready is case insensitive", value: "Ready"},
		{
			name:  "one blocker in another repo",
			value: "asylguiden.se#1",
			wantRefs: []board.BlockerRef{
				{Owner: "goodtribes-org", Repo: "asylguiden.se", Number: 1},
			},
		},
		{
			name:  "several, with the spacing a human types",
			value: "asylguiden.se#1, asylguiden.se#3 ,kickfix#12",
			wantRefs: []board.BlockerRef{
				{Owner: "goodtribes-org", Repo: "asylguiden.se", Number: 1},
				{Owner: "goodtribes-org", Repo: "asylguiden.se", Number: 3},
				{Owner: "goodtribes-org", Repo: "kickfix", Number: 12},
			},
		},
		{
			name:     "fully qualified owner/repo",
			value:    "other-org/thing#7",
			wantRefs: []board.BlockerRef{{Owner: "other-org", Repo: "thing", Number: 7}},
		},
		{
			name:     "bare hash means this repo",
			value:    "#42",
			wantRefs: []board.BlockerRef{{Owner: "goodtribes-org", Repo: "kickfix", Number: 42}},
		},
		// The field is typed by hand. A typo must surface as "unparsable", not
		// as "nothing is blocking this" — the second reading would release a
		// card that is genuinely blocked.
		{name: "missing hash", value: "asylguiden.se 1", wantBad: []string{"asylguiden.se 1"}},
		{name: "non-numeric issue", value: "asylguiden.se#one", wantBad: []string{"asylguiden.se#one"}},
		{name: "zero is not an issue number", value: "asylguiden.se#0", wantBad: []string{"asylguiden.se#0"}},
		{
			name:     "one good one bad reports both",
			value:    "asylguiden.se#1, garbage",
			wantRefs: []board.BlockerRef{{Owner: "goodtribes-org", Repo: "asylguiden.se", Number: 1}},
			wantBad:  []string{"garbage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refs, bad := ParseBlockedBy("goodtribes-org", "kickfix", tc.value)
			if len(refs) != len(tc.wantRefs) {
				t.Fatalf("refs = %v, want %v", refs, tc.wantRefs)
			}
			for i := range refs {
				if refs[i] != tc.wantRefs[i] {
					t.Errorf("refs[%d] = %v, want %v", i, refs[i], tc.wantRefs[i])
				}
			}
			if len(bad) != len(tc.wantBad) {
				t.Fatalf("bad = %v, want %v", bad, tc.wantBad)
			}
			for i := range bad {
				if bad[i] != tc.wantBad[i] {
					t.Errorf("bad[%d] = %q, want %q", i, bad[i], tc.wantBad[i])
				}
			}
		})
	}
}

func item(number int, status, phase, track, size string, labels ...string) board.Item {
	return board.Item{
		ID: "PVTI_" + string(rune('a'+number)), IsIssue: true, State: "OPEN",
		Owner: "goodtribes-org", Repo: "kickfix", Number: number,
		Status: status, Phase: phase, Track: track, Size: size, Labels: labels,
	}
}

func TestCandidatesFiltering(t *testing.T) {
	cfg := testCfg()

	draft := board.Item{ID: "d", Status: "new"}
	archived := item(1, "new", "M0 Foundation", "Foundation", "S")
	archived.Archived = true
	closed := item(2, "new", "M0 Foundation", "Foundation", "S")
	closed.State = "CLOSED"

	items := []board.Item{
		draft,
		archived,
		closed,
		item(3, "plan", "M0 Foundation", "Foundation", "S"),          // wrong column
		item(4, "new", "M0 Foundation", "Foundation", "S", "needs-human"), // decided rejection
		item(5, "new", "M0 Foundation", "Foundation", "S", "picked-by-laptop"),
		item(6, "NEW", "M0 Foundation", "Foundation", "S"), // status match is case-insensitive
	}

	got := Candidates(items, "new", cfg, nil)
	if len(got) != 1 || got[0].Number != 6 {
		t.Fatalf("Candidates kept %d items %v, want only #6", len(got), numbers(got))
	}
}

func TestCandidatesHonoursSkip(t *testing.T) {
	cfg := testCfg()
	items := []board.Item{
		item(1, "new", "M0 Foundation", "Foundation", "S"),
		item(2, "new", "M0 Foundation", "Foundation", "S"),
	}
	got := Candidates(items, "new", cfg, func(i board.Item) bool { return i.Number == 1 })
	if len(got) != 1 || got[0].Number != 2 {
		t.Fatalf("got %v, want only #2", numbers(got))
	}
}

// The ordering is the one gh-request step 1.2 described: groundwork lands
// before what is built on top of it, and the same board always yields the same
// pick.
func TestOrder(t *testing.T) {
	cfg := testCfg()
	items := []board.Item{
		item(50, "new", "M3 Compose", "Mail", "S"),
		item(10, "new", "M0 Foundation", "Mail", "S"),      // later phase wins over track
		item(20, "new", "M0 Foundation", "Foundation", "L"),
		item(30, "new", "M0 Foundation", "Foundation", "S"),
		item(40, "new", "M0 Foundation", "Infra", "S"),
		item(5, "new", "", "Mail", "S"), // no phase sorts last
	}

	Order(items, cfg)

	want := []int{30, 40, 20, 10, 50, 5}
	if got := numbers(items); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestOrderSizeBeatsIssueNumber(t *testing.T) {
	cfg := testCfg()
	items := []board.Item{
		item(1, "new", "M1 Deliverability", "Mail", "L"),
		item(9, "new", "M1 Deliverability", "Mail", "S"),
		item(5, "new", "M1 Deliverability", "Mail", "M"),
	}
	Order(items, cfg)
	if got := numbers(items); !equal(got, []int{9, 5, 1}) {
		t.Fatalf("order = %v, want [9 5 1]", got)
	}
}

func numbers(items []board.Item) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.Number
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
