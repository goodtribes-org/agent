package board

import "strings"

// The raw shapes GraphQL returns. They exist only to be flattened into Item;
// nothing outside this file should have to know that a board field value is a
// union of three types keyed by a nested field name.

type rawFieldValue struct {
	Text   *string  `json:"text"`
	Name   *string  `json:"name"`
	Number *float64 `json:"number"`
	Field  struct {
		Name string `json:"name"`
	} `json:"field"`
}

type rawContent struct {
	ID         string `json:"id"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

type rawItem struct {
	ID          string `json:"id"`
	IsArchived  bool   `json:"isArchived"`
	FieldValues struct {
		Nodes []rawFieldValue `json:"nodes"`
	} `json:"fieldValues"`
	Content rawContent `json:"content"`
}

// flatten turns one raw item into an Item. Field lookup is by name and
// case-insensitive, matching how the slash-commands did it — the board is
// hand-edited and a field renamed from "Blocked by" to "Blocked By" should
// not stop the workers.
func flatten(r rawItem, names fieldNames) Item {
	it := Item{
		ID:       r.ID,
		Archived: r.IsArchived,
	}

	// content is an empty object for draft items and pull requests. A number
	// of zero is the reliable tell — issue numbers start at one.
	if r.Content.Number > 0 && r.Content.ID != "" {
		it.IsIssue = true
		it.IssueID = r.Content.ID
		it.Number = r.Content.Number
		it.Title = r.Content.Title
		it.URL = r.Content.URL
		it.State = r.Content.State
		it.Repo = r.Content.Repository.Name
		it.Owner = r.Content.Repository.Owner.Login
		for _, l := range r.Content.Labels.Nodes {
			it.Labels = append(it.Labels, l.Name)
		}
	}

	for _, fv := range r.FieldValues.Nodes {
		value := ""
		switch {
		case fv.Name != nil:
			value = *fv.Name
		case fv.Text != nil:
			value = *fv.Text
		default:
			continue
		}
		switch {
		case strings.EqualFold(fv.Field.Name, names.Status):
			it.Status = value
		case strings.EqualFold(fv.Field.Name, names.Phase):
			it.Phase = value
		case strings.EqualFold(fv.Field.Name, names.Track):
			it.Track = value
		case strings.EqualFold(fv.Field.Name, names.Size):
			it.Size = value
		case strings.EqualFold(fv.Field.Name, names.BlockedBy):
			it.BlockedBy = value
		}
	}

	return it
}

// fieldNames is the configured name of each board field the workers read.
type fieldNames struct {
	Status    string
	Phase     string
	Track     string
	Size      string
	BlockedBy string
}
