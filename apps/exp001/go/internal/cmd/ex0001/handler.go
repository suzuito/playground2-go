package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// サンプルハンドラー
func newHandler(
	catchedShutdownSignal *atomic.Bool,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if catchedShutdownSignal != nil && catchedShutdownSignal.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "graceful shutdown is started") // nolint:errcheck
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
		} else if seconds < 0 {
			seconds = 0
		} else if seconds > 120 {
			seconds = 120
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
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
