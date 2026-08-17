package llm

import (
	"errors"
	"strings"
)

// ErrNoJSON means the reply contained nothing that looked like a JSON object.
var ErrNoJSON = errors.New("no JSON object found in the reply")

// ExtractJSON pulls the JSON object out of a model reply.
//
// Models wrap JSON in a markdown fence, prefix it with "Here is the JSON:",
// or append a paragraph of commentary — all of them despite being told not
// to. Rather than fail on that, find the outermost balanced object and use it.
// Braces inside strings and escaped quotes are tracked, so a JSON value that
// itself contains `{` does not end the scan early.
func ExtractJSON(reply string) (string, error) {
	s := stripFence(strings.TrimSpace(reply))

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", ErrNoJSON
	}

	var (
		depth    int
		inString bool
		escaped  bool
	)
	for i := start; i < len(s); i++ {
		ch := s[i]

		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
			if depth < 0 {
				return "", ErrNoJSON
			}
		}
	}
	return "", ErrNoJSON
}

// stripFence removes a surrounding markdown code fence, with or without a
// language tag.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line, which may be ``` or ```json.
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		return s
	}
	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return strings.TrimSpace(s)
}
