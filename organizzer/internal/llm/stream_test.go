package llm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

func testClient(t *testing.T, url string) *Client {
	t.Helper()
	return &Client{
		cfg: config.Config{
			BergetBaseURL: url,
			BergetAPIKey:  "sk-test",
			Model:         "zai-org/GLM-5.2",
			MaxTokens:     12000,
			MaxAttempts:   2,
			BergetTimeout: 10 * time.Second,
		},
		http: &http.Client{Timeout: 10 * time.Second},
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func sse(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, l := range lines {
		_, _ = io.WriteString(w, l+"\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// The reply arrives one delta at a time and has to come back out as the text
// the model wrote, with the token counts from the trailing usage chunk — which
// carries no choices and would be skipped by anything looping over them.
func TestStreamedReplyIsReassembled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`data: {"choices":[{"delta":{"content":"## Plan"}}]}`,
			`data: {"choices":[{"delta":{"content":"\n\nStep one"}}]}`,
			`data: {"choices":[{"delta":{"content":"."},"finish_reason":"stop"}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":17,"completion_tokens":2851,"total_tokens":2868}}`,
			`data: [DONE]`,
		)
	}))
	defer srv.Close()

	reply, err := testClient(t, srv.URL).complete(context.Background(), []Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if want := "## Plan\n\nStep one."; reply != want {
		t.Errorf("reply = %q, want %q", reply, want)
	}
}

// The request must actually ask for a stream. Sent without it, berget cuts the
// response at about sixty seconds and every plan worth having dies.
func TestTheRequestAsksForAStream(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		sse(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `data: [DONE]`)
	}))
	defer srv.Close()

	if _, err := testClient(t, srv.URL).complete(context.Background(), []Message{{Role: "user", Content: "go"}}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !strings.Contains(body, `"stream":true`) {
		t.Errorf("request did not ask for a stream: %s", body)
	}
	if !strings.Contains(body, `"include_usage":true`) {
		t.Errorf("request did not ask for usage, so token counts go missing: %s", body)
	}
}

// A stream that stops without [DONE] is a connection that died mid-sentence.
// Returning what arrived would post a half-written plan as though the model had
// meant to stop there.
func TestATruncatedStreamIsAnErrorNotAShortAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`data: {"choices":[{"delta":{"content":"## Plan\n\nStep one"}}]}`,
			`data: {"choices":[{"delta":{"content":", step two"}}]}`,
		)
	}))
	defer srv.Close()

	reply, err := testClient(t, srv.URL).complete(context.Background(), []Message{{Role: "user", Content: "go"}})
	if err == nil {
		t.Fatalf("a stream cut short returned %q instead of an error", reply)
	}
	if !strings.Contains(err.Error(), "[DONE]") {
		t.Errorf("err = %v, want it to name the missing terminator", err)
	}
}

// Hitting the token ceiling is not a transport problem: the reply is a
// half-written object, and asking again produces the same one.
func TestFinishReasonLengthIsReportedAsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`data: {"choices":[{"delta":{"content":"{\"steps\":["}}]}`,
			`data: {"choices":[{"delta":{"content":""},"finish_reason":"length"}]}`,
			`data: [DONE]`,
		)
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).complete(context.Background(), []Message{{Role: "user", Content: "go"}})
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

// A 4xx is a bad request or an exhausted budget. Retrying spends time and
// changes nothing, so it comes straight back with the body for diagnosis.
func TestClientErrorsAreNotRetried(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"max_tokens too large"}}`)
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).complete(context.Background(), []Message{{Role: "user", Content: "go"}})
	if err == nil {
		t.Fatal("a 400 was not reported as an error")
	}
	if calls != 1 {
		t.Errorf("made %d calls, want 1 — a 400 does not improve on retry", calls)
	}
	if !strings.Contains(err.Error(), "max_tokens too large") {
		t.Errorf("err = %v, want the API's own message", err)
	}
}
