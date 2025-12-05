package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

func newHandler(
	isGracefulShutdownProcStarted *atomic.Bool,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if isGracefulShutdownProcStarted != nil && isGracefulShutdownProcStarted.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "graceful shutdown is started") // nolint:errcheck
			return
		}
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep3secs", func(w http.ResponseWriter, r *http.Request) {
		if err := sleep(r.Context(), 3*time.Second); err != nil {
			w.WriteHeader(http.StatusInternalServerError) //nolint:errcheck
			fmt.Fprintf(w, "failed to sleep: %+v", err)   // nolint:errcheck
			return
		}
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep30secs", func(w http.ResponseWriter, r *http.Request) {
		if err := sleep(r.Context(), 30*time.Second); err != nil {
			w.WriteHeader(http.StatusInternalServerError) //nolint:errcheck
			fmt.Fprintf(w, "failed to sleep: %+v", err)   // nolint:errcheck
			return
		}
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		secondsString := q.Get("seconds")
		seconds, err := strconv.Atoi(secondsString)
		if err != nil {
			seconds = 0
		}

		if err := sleep(r.Context(), time.Duration(seconds)*time.Second); err != nil {
			w.WriteHeader(http.StatusInternalServerError) //nolint:errcheck
			fmt.Fprintf(w, "failed to sleep: %+v", err)   // nolint:errcheck
			return
		}

		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	return mux
}

func sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-time.After(duration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
