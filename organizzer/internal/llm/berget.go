// Package llm talks to berget.ai, which serves an OpenAI-compatible chat
// completions API. It is deliberately small: one call shape, JSON in, JSON out.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// Message is one turn in the conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float64   `json:"temperature"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`

	// Always true. berget cuts a non-streaming response off at about sixty
	// seconds — the socket is simply reset, with no status and no body, which
	// surfaces as "connection reset by peer" or a client timeout depending on
	// where the clock lands. A plan takes minutes to generate, so every plan
	// long enough to be worth having died that way and no client-side timeout
	// could have helped. Streaming keeps bytes moving and the same request
	// completes: measured 168s and 4878 chunks against a 60s wall.
	Stream       bool          `json:"stream"`
	StreamOption *streamOption `json:"stream_options,omitempty"`
}

// streamOption asks for the usage block, which a stream otherwise omits. It
// arrives in a final chunk carrying no choices.
type streamOption struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatChunk is one server-sent event from a streamed completion.
type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Client is a berget.ai chat client.
type Client struct {
	cfg  config.Config
	http *http.Client
	log  *slog.Logger
}

// New returns a Client. The timeout is long because the plan stage sends a
// whole repository tree and asks for twelve thousand tokens back.
func New(cfg config.Config, log *slog.Logger) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.BergetTimeout},
		log:  log,
	}
}

// ErrTruncated means the model stopped because it hit the token limit. The
// reply is almost certainly a half-written JSON object, and raising
// LLM_MAX_TOKENS_* is the fix — retrying the same call is not.
var ErrTruncated = fmt.Errorf("model output truncated at the token limit")

// JSON sends a system and user prompt and unmarshals the model's reply into
// out. It retries on a reply that is not valid JSON, feeding the failure back
// so the model can correct itself; that is far cheaper than failing the issue
// and re-running the whole stage fifteen minutes later.
func (c *Client) JSON(ctx context.Context, system, user string, out any) error {
	messages := []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	var lastErr error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		reply, err := c.complete(ctx, messages)
		if err != nil {
			// A transport or API failure is not something the model can fix.
			return err
		}

		raw, err := ExtractJSON(reply)
		if err == nil {
			if err = json.Unmarshal([]byte(raw), out); err == nil {
				return nil
			}
		}
		lastErr = err
		c.log.Warn("model did not return usable JSON",
			"attempt", attempt, "err", err, "reply_prefix", prefix(reply, 200))

		if attempt == c.cfg.MaxAttempts {
			break
		}
		// Show the model its own reply and the parse error. Re-sending the
		// original prompt unchanged would just reproduce the same mistake.
		messages = append(messages,
			Message{Role: "assistant", Content: reply},
			Message{Role: "user", Content: fmt.Sprintf(
				"That reply could not be parsed: %v\n\nReturn the same information as a single "+
					"valid JSON object. No prose, no markdown fence. Start with { and end with }.", err)})
	}
	return fmt.Errorf("model returned unusable JSON after %d attempts: %w", c.cfg.MaxAttempts, lastErr)
}

// complete performs one chat completion.
func (c *Client) complete(ctx context.Context, messages []Message) (string, error) {
	body := chatRequest{
		Model:        c.cfg.Model,
		Messages:     messages,
		Temperature:  c.cfg.Temperature,
		MaxTokens:    c.cfg.MaxTokens,
		Stream:       true,
		StreamOption: &streamOption{IncludeUsage: true},
	}
	if c.cfg.JSONMode {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if debug(c.log) {
		c.log.Debug("llm request", "model", c.cfg.Model, "messages", len(messages),
			"prompt_prefix", prefix(messages[len(messages)-1].Content, 800))
	}

	// Two attempts only, and only for transport errors and 5xx. A 4xx is a bad
	// request and a 429 here means the account is out of budget — neither
	// improves by being asked again.
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.cfg.BergetBaseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.cfg.BergetAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "organizzer")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			sleep(ctx, time.Duration(attempt)*3*time.Second)
			continue
		}

		// An error is delivered as an ordinary JSON body, not as a stream, so
		// it is read whole before any SSE parsing is attempted.
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("berget %d: %s", resp.StatusCode, prefix(string(raw), 300))
				sleep(ctx, time.Duration(attempt)*3*time.Second)
				continue
			}
			return "", fmt.Errorf("berget %d: %s", resp.StatusCode, prefix(string(raw), 300))
		}

		reply, finish, used, err := readStream(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			// A stream cut in the middle is a transport failure like any other:
			// worth one more attempt, and never worth handing a half-written
			// plan to the stage as if it were complete.
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			sleep(ctx, time.Duration(attempt)*3*time.Second)
			continue
		}

		c.log.Info("llm call complete",
			"model", c.cfg.Model,
			"prompt_tokens", used.PromptTokens,
			"completion_tokens", used.CompletionTokens,
			"finish", finish)

		if finish == "length" {
			return "", ErrTruncated
		}
		if strings.TrimSpace(reply) == "" {
			return "", fmt.Errorf("berget returned an empty completion")
		}
		return reply, nil
	}
	return "", fmt.Errorf("berget request failed: %w", lastErr)
}

// usage is the token accounting from a completed stream.
type usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// readStream consumes a server-sent event stream and reassembles the reply.
//
// The wire format is one `data: <json>` line per chunk, blank lines between,
// and a final `data: [DONE]`. That marker is what separates a finished
// generation from a connection that died mid-sentence — without it a truncated
// stream would look exactly like a short answer, and a half-written plan would
// be posted as though the model had meant to stop there.
func readStream(body io.Reader) (string, string, usage, error) {
	var (
		out    strings.Builder
		finish string
		u      usage
		done   bool
	)

	sc := bufio.NewScanner(body)
	// Chunks are small, but a single delta carrying a long line of a plan can
	// exceed the default 64KiB limit, and that would fail the whole call.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			done = true
			break
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return "", "", u, fmt.Errorf("decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return "", "", u, fmt.Errorf("berget: %s", chunk.Error.Message)
		}
		// The usage chunk carries no choices, and the last content chunk
		// carries the finish reason.
		if chunk.Usage != nil {
			u = usage{chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens}
		}
		for _, ch := range chunk.Choices {
			out.WriteString(ch.Delta.Content)
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", u, fmt.Errorf("read stream: %w", err)
	}
	if !done {
		return "", "", u, fmt.Errorf("stream ended without [DONE] after %d bytes", out.Len())
	}
	return out.String(), finish, u, nil
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func debug(log *slog.Logger) bool { return log.Enabled(context.Background(), slog.LevelDebug) }

func prefix(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
