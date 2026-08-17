package service

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Health is the liveness/readiness surface. There is no request traffic to
// serve, so the only thing worth reporting is whether the poll loop is still
// turning: a stage that has not completed a cycle in a long time is wedged on
// something the process itself cannot see.
type Health struct {
	lastLoopUnix atomic.Int64
	stale        time.Duration
}

// NewHealth returns a Health that reports not-ready once a full stale period
// has passed with no completed loop.
func NewHealth(stale time.Duration) *Health {
	h := &Health{stale: stale}
	h.Tick()
	return h
}

// Tick records that a poll loop just completed.
func (h *Health) Tick() { h.lastLoopUnix.Store(time.Now().Unix()) }

// Serve runs the health listener until ctx is cancelled. A failure to bind is
// logged and otherwise ignored: losing the probe endpoint is not a reason to
// stop processing the board.
func (h *Health) Serve(ctx context.Context, addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		age := time.Since(time.Unix(h.lastLoopUnix.Load(), 0))
		if age > h.stale {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("stale: no completed loop in " + age.Truncate(time.Second).String()))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Warn("health listener stopped", "err", err)
	}
}
