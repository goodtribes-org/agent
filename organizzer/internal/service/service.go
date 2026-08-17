// Package service holds the small process-level helpers every stage needs:
// environment reading, a logger, and signal handling. It deliberately mirrors
// postfix-client's package of the same name so the two are read the same way.
package service

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Logger returns the process logger. LOG_LEVEL=debug additionally turns on
// prompt and response dumps in the llm package, which is the only way to see
// why a model returned something unparseable.
func Logger(stage string) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(Env("LOG_LEVEL", "info")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h).With("stage", stage)
}

// Debug reports whether LOG_LEVEL asks for debug output.
func Debug() bool { return strings.EqualFold(Env("LOG_LEVEL", "info"), "debug") }

// Context returns a context cancelled on SIGINT or SIGTERM. A stage in the
// middle of an issue finishes the API call it is on and then stops at the next
// loop boundary; it never abandons work between posting a comment and moving
// the card, because that ordering is what makes a restart safe.
func Context() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// Env reads a variable, falling back to def when unset or empty.
func Env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// EnvInt reads an integer variable, falling back to def when unset, empty or
// unparseable.
func EnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// EnvBool reads a boolean variable. Anything strconv.ParseBool rejects falls
// back to def.
func EnvBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// EnvSeconds reads a duration expressed in whole seconds.
func EnvSeconds(key string, def int) time.Duration {
	return time.Duration(EnvInt(key, def)) * time.Second
}

// EnvList reads a comma-separated list, trimming blanks. Order is preserved —
// it matters for FOUNDRY_SKILLS, where the operator resolves duplicate env
// names to the last entry.
func EnvList(key string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}
