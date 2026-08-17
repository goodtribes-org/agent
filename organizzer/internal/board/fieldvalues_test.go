package board

import (
	"encoding/json"
	"testing"
)

func names() fieldNames {
	return fieldNames{
		Status: "Status", Phase: "Phase", Track: "Track", Size: "Size", BlockedBy: "Blocked by",
	}
}

// A real item as ProjectsV2 returns it: field values are a union keyed by a
// nested field name, and the fields come back in no particular order.
const realItemJSON = `{
  "id": "PVTI_lADOAeXyl84BfP4Ezg1IXV0",
  "isArchived": false,
  "fieldValues": { "nodes": [
    {},
    { "text": "ready", "field": { "name": "Blocked by" } },
    { "name": "M0 Foundation", "field": { "name": "Phase" } },
    { "name": "review", "field": { "name": "Status" } },
    { "name": "S", "field": { "name": "Size" } },
    { "name": "Foundation", "field": { "name": "Track" } }
  ]},
  "content": {
    "id": "I_abc",
    "number": 9,
    "title": "Wire the BFF proxy",
    "url": "https://github.com/goodtribes-org/kickfix/issues/9",
    "state": "OPEN",
    "repository": { "name": "kickfix", "owner": { "login": "goodtribes-org" } },
    "labels": { "nodes": [ { "name": "track:foundation" }, { "name": "size:s" } ] }
  }
}`

func TestFlattenRealItem(t *testing.T) {
	var raw rawItem
	if err := json.Unmarshal([]byte(realItemJSON), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	it := flatten(raw, names())

	if !it.IsIssue {
		t.Fatal("item backed by an issue was not recognised as one")
	}
	checks := map[string]struct{ got, want string }{
		"status":    {it.Status, "review"},
		"phase":     {it.Phase, "M0 Foundation"},
		"track":     {it.Track, "Foundation"},
		"size":      {it.Size, "S"},
		"blockedBy": {it.BlockedBy, "ready"},
		"repo":      {it.Repo, "kickfix"},
		"owner":     {it.Owner, "goodtribes-org"},
		"title":     {it.Title, "Wire the BFF proxy"},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
	if it.Number != 9 {
		t.Errorf("number = %d, want 9", it.Number)
	}
	if it.NameWithOwner() != "goodtribes-org/kickfix" {
		t.Errorf("NameWithOwner = %q", it.NameWithOwner())
	}
	if !it.HasLabel("SIZE:S") {
		t.Error("HasLabel should be case-insensitive")
	}
	if !it.HasLabelPrefix("track:") {
		t.Error("HasLabelPrefix failed on a label that does start with the prefix")
	}
}

// Draft cards and pull requests come back with an empty content object. They
// must not look like issues, or the workers would try to comment on nothing.
func TestFlattenSkipsNonIssueContent(t *testing.T) {
	const draft = `{"id":"PVTI_draft","isArchived":false,"fieldValues":{"nodes":[
		{"name":"new","field":{"name":"Status"}}]},"content":{}}`

	var raw rawItem
	if err := json.Unmarshal([]byte(draft), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	it := flatten(raw, names())
	if it.IsIssue {
		t.Fatal("a draft card was treated as an issue")
	}
	if it.Status != "new" {
		t.Errorf("field values should still flatten for a draft, got status %q", it.Status)
	}
}

// The board is hand-edited; a field renamed from "Blocked by" to "Blocked By"
// should not stop the workers.
func TestFlattenMatchesFieldNamesCaseInsensitively(t *testing.T) {
	const item = `{"id":"x","fieldValues":{"nodes":[
		{"text":"asylguiden.se#1","field":{"name":"BLOCKED BY"}}]},"content":{}}`

	var raw rawItem
	if err := json.Unmarshal([]byte(item), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := flatten(raw, names()).BlockedBy; got != "asylguiden.se#1" {
		t.Fatalf("BlockedBy = %q, want the value matched despite the casing", got)
	}
}

func TestLatestCommentContainingScansBackwards(t *testing.T) {
	is := Issue{Comments: []Comment{
		{ID: "1", Body: "plan v1 SENTINEL"},
		{ID: "2", Body: "some chatter"},
		{ID: "3", Body: "plan v2 SENTINEL"},
	}}
	got, ok := is.LatestCommentContaining("SENTINEL")
	if !ok {
		t.Fatal("sentinel not found")
	}
	if got.ID != "3" {
		t.Fatalf("got comment %s, want the most recent one (3)", got.ID)
	}
}
