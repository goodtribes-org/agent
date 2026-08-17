package llm

import (
	"encoding/json"
	"testing"
)

// Models wrap JSON in fences and pad it with commentary despite being told not
// to. Every case here is a shape that has to survive rather than fail the
// issue and cost a fifteen-minute backoff.
func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "bare object",
			reply: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "fenced with a language tag",
			reply: "```json\n{\"a\":1}\n```",
			want:  `{"a":1}`,
		},
		{
			name:  "fenced without a tag",
			reply: "```\n{\"a\":1}\n```",
			want:  `{"a":1}`,
		},
		{
			name:  "prose before and after",
			reply: "Here is the JSON you asked for:\n{\"a\":1}\nHope that helps!",
			want:  `{"a":1}`,
		},
		{
			name:  "nested objects",
			reply: `{"a":{"b":{"c":2}},"d":3}`,
			want:  `{"a":{"b":{"c":2}},"d":3}`,
		},
		{
			// A brace inside a string must not end the scan. This is the case
			// that matters most in practice: plan steps quote code.
			name:  "braces inside strings",
			reply: `{"change":"add a func() { return nil } helper"}`,
			want:  `{"change":"add a func() { return nil } helper"}`,
		},
		{
			name:  "escaped quotes inside strings",
			reply: `{"why":"it said \"no\" and then }"}`,
			want:  `{"why":"it said \"no\" and then }"}`,
		},
		{
			name:  "leading whitespace",
			reply: "\n\n   {\"a\":1}",
			want:  `{"a":1}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractJSON(tc.reply)
			if err != nil {
				t.Fatalf("ExtractJSON: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
			// Whatever comes out must actually parse — that is the only thing
			// the caller cares about.
			var sink map[string]any
			if err := json.Unmarshal([]byte(got), &sink); err != nil {
				t.Fatalf("extracted text does not parse: %v", err)
			}
		})
	}
}

func TestExtractJSONFailures(t *testing.T) {
	for _, reply := range []string{
		"",
		"I could not do that.",
		"{\"a\": 1",      // truncated at the token limit
		"} stray close ", // no opening brace
	} {
		if got, err := ExtractJSON(reply); err == nil {
			t.Errorf("ExtractJSON(%q) = %q, want an error", reply, got)
		}
	}
}
