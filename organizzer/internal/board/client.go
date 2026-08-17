package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// Client talks to GitHub over both APIs: GraphQL for ProjectsV2 (there is no
// REST equivalent) and REST v3 for labels (where GraphQL would need a node id
// lookup per label for no benefit).
type Client struct {
	cfg  config.Config
	http *http.Client
	log  *slog.Logger
}

// New returns a Client. The timeout is generous because a project item page
// with field values is a slow query on a board this size.
func New(cfg config.Config, log *slog.Logger) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 60 * time.Second},
		log:  log,
	}
}

// ErrNotFound is returned for a 404 on a REST call.
var ErrNotFound = errors.New("not found")

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type graphQLEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// GraphQLErrors carries the errors array from a 200 response. GitHub returns
// 200 with a populated errors array for most failures, so a naive status-code
// check would treat a failed mutation as a success.
type GraphQLErrors struct{ Errors []graphQLError }

func (e *GraphQLErrors) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, x := range e.Errors {
		if x.Type != "" {
			parts = append(parts, x.Type+": "+x.Message)
		} else {
			parts = append(parts, x.Message)
		}
	}
	return "graphql: " + strings.Join(parts, "; ")
}

// StaleSchema reports whether the errors look like the board's field or option
// ids have moved underneath us, which is the signal to re-run discovery.
func (e *GraphQLErrors) StaleSchema() bool {
	for _, x := range e.Errors {
		m := strings.ToLower(x.Message)
		if strings.Contains(m, "could not resolve") ||
			strings.Contains(m, "does not exist") ||
			strings.Contains(m, "not a valid option") ||
			strings.Contains(m, "invalid option") {
			return true
		}
	}
	return false
}

// graphql posts a query and unmarshals the data field into out.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}

	body, err := c.do(ctx, http.MethodPost, c.cfg.GitHubAPIURL+"/graphql", payload, "application/json")
	if err != nil {
		return err
	}

	var env graphQLEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	if len(env.Errors) > 0 {
		return &GraphQLErrors{Errors: env.Errors}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode graphql data: %w", err)
	}
	return nil
}

// rest performs a REST v3 call and returns the raw body.
func (c *Client) rest(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var buf []byte
	if payload != nil {
		var err error
		if buf, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	return c.do(ctx, method, c.cfg.GitHubAPIURL+path, buf, "application/vnd.github+json")
}

// do runs one HTTP request with retries. Retries cover the two things that
// genuinely recover on their own — secondary rate limits and 5xx — and
// nothing else. A 4xx is a bug in the request and retrying it just makes the
// same mistake more times.
func (c *Client) do(ctx context.Context, method, url string, body []byte, accept string) ([]byte, error) {
	const maxAttempts = 4

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.cfg.GitHubToken)
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "organizzer")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			c.sleep(ctx, backoff(attempt))
			continue
		}

		payload, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			c.sleep(ctx, backoff(attempt))
			continue
		}

		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
			// Primary rate limit exhaustion and secondary limits both land
			// here. Honour the server's own advice when it gives any.
			wait := rateLimitWait(resp.Header, attempt)
			lastErr = fmt.Errorf("github %d: %s", resp.StatusCode, snippet(payload))
			c.log.Warn("rate limited by github", "wait", wait.String(), "attempt", attempt)
			c.sleep(ctx, wait)
			continue
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("github %d: %s", resp.StatusCode, snippet(payload))
			c.sleep(ctx, backoff(attempt))
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("github %d: %s", resp.StatusCode, snippet(payload))
		}
		return payload, nil
	}
	return nil, fmt.Errorf("github request failed after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * time.Second // 2s, 4s, 8s, 16s
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// rateLimitWait reads Retry-After, then x-ratelimit-reset, then falls back to
// plain backoff.
func rateLimitWait(h http.Header, attempt int) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if v := h.Get("X-RateLimit-Reset"); v != "" && h.Get("X-RateLimit-Remaining") == "0" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Until(time.Unix(unix, 0)); d > 0 && d < 15*time.Minute {
				return d + time.Second
			}
		}
	}
	return backoff(attempt)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 512 {
		return s[:512] + "…"
	}
	return s
}
