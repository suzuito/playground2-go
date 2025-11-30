package gracefulshutdownexample

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep3secs", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep30secs", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Second)
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("GET /sleep", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		secondsString := q.Get("seconds")
		seconds, err := strconv.Atoi(secondsString)
		if err != nil {
			seconds = 0
		}
		time.Sleep(time.Duration(seconds) * time.Second)
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	return mux
}
