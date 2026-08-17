package foundry

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
)

// Client submits tasks to the kube-foundry webhook.
type Client struct {
	cfg  config.Config
	http *http.Client
	log  *slog.Logger
}

// New returns a Client.
//
// The webhook's own server timeouts are ten seconds, so there is no point
// waiting much longer than that; thirty gives room for a slow scheduler
// without hanging the loop.
func New(cfg config.Config, log *slog.Logger) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
		log:  log,
	}
}

// URL is the full endpoint this client posts to.
func (c *Client) URL() string { return c.cfg.WebhookURL + c.cfg.WebhookPath }

// Submit creates one task.
//
// There is deliberately no retry. The webhook generates a fresh task name per
// submission, so a retried create is a second agent run against the same
// issue — two sandboxes, two sets of commits, two pull requests, and twice the
// spend. A failure here is reported to the issue and left for a human.
func (c *Client) Submit(ctx context.Context, req TaskRequest) (TaskResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return TaskResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(), bytes.NewReader(payload))
	if err != nil {
		return TaskResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "organizzer")

	// The submit route is unauthenticated in kube-foundry v0.1.0 — only the
	// GitHub route verifies a signature. The header is sent anyway when a
	// secret is configured, so that putting a proxy in front of the webhook
	// later needs no change here.
	if c.cfg.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(c.cfg.WebhookSecret))
		mac.Write(payload)
		httpReq.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return TaskResponse{}, fmt.Errorf("post to %s: %w", c.URL(), err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return TaskResponse{}, fmt.Errorf("read response from %s: %w", c.URL(), readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Errors come back as plain text from http.Error, not JSON.
		return TaskResponse{}, fmt.Errorf("kube-foundry %d from %s: %s",
			resp.StatusCode, c.URL(), trim(string(body), 512))
	}

	var out TaskResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return TaskResponse{}, fmt.Errorf("decode kube-foundry response: %w (body: %s)",
			err, trim(string(body), 256))
	}
	if out.Name == "" {
		return TaskResponse{}, fmt.Errorf("kube-foundry accepted the task but returned no name (body: %s)",
			trim(string(body), 256))
	}

	c.log.Info("task dispatched", "task", out.Name, "namespace", out.Namespace, "repo", req.Repo)
	return out, nil
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
