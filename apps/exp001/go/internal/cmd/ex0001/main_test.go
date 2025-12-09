package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dummySigTerm = errors.New("DUMMY_SIG_TERM")
var dummyServerPort = 8080

func waitUntilServerUp() {
	for {
		fmt.Println("waitUntilServerUp is checking ...")

		res, err := http.DefaultClient.Get(fmt.Sprintf("http://127.0.0.1:%d/health", dummyServerPort))
		if err != nil {
		} else {
			if res.StatusCode == http.StatusOK {
				break
			}
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func Test(t *testing.T) {
	testHandler := http.NewServeMux()
	testHandler.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	testHandler.HandleFunc("GET /sleep", func(w http.ResponseWriter, r *http.Request) {
		secondsString := r.URL.Query().Get("seconds")
		seconds, err := strconv.Atoi(secondsString)
		if err != nil || seconds < 0 {
			seconds = 0
		}

		ctx := r.Context()
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)      //nolint:errcheck
			fmt.Fprintf(w, "failed to sleep: %+v", ctx.Err()) // nolint:errcheck
			return
		}

		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	testHandler.HandleFunc("GET /sleep_infinity", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Duration(60) * time.Second)
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})

	testCases := []struct {
		desc                            string
		opts                            options
		executionBeforeGracefulShutdown func()
		expectedExitCode                exitCode
	}{
		{
			desc: `アクティブなコネクションがない状態でシグナルを受信した
					       グレースフルシャットダウンは成功する`,
			opts: options{
				WaitSecondsUntilGracefulShutdownIsStarted:   1,
				GracefulShutdownTimeoutSeconds:              10,
				ForcefullyRequestCancellationTimeoutSeconds: 10,
			},
			executionBeforeGracefulShutdown: func() {},
			expectedExitCode:                shutodownGracefully,
		},
		{
			desc: `アクティブなコネクションがある状態でシグナルを受信した
					       時間内に処理を終えることができ、グレースフルシャットダウンに成功した場合`,
			opts: options{
				WaitSecondsUntilGracefulShutdownIsStarted:   1,
				GracefulShutdownTimeoutSeconds:              10,
				ForcefullyRequestCancellationTimeoutSeconds: 10,
			},
			executionBeforeGracefulShutdown: func() {
				res, err := http.DefaultClient.Get("http://127.0.0.1:8080/sleep?seconds=5")
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, res.StatusCode)
			},
			expectedExitCode: shutodownGracefully,
		},
		{
			desc: `アクティブなコネクションがある状態でシグナルを受信した
					       時間内に処理を終えることができないので、グレースフルシャットダウンに失敗した場合
						   リクエストハンドラーが、伝搬されたキャンセルに従ってキャンセル処理を遂行し、503を返した`,
			opts: options{
				WaitSecondsUntilGracefulShutdownIsStarted:   1,
				GracefulShutdownTimeoutSeconds:              10,
				ForcefullyRequestCancellationTimeoutSeconds: 10,
			},
			executionBeforeGracefulShutdown: func() {
				res, err := http.DefaultClient.Get("http://127.0.0.1:8080/sleep?seconds=9999")
				require.NoError(t, err)
				require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
			},
			expectedExitCode: shutodownForcefully,
		},
		{
			desc: `アクティブなコネクションがある状態でシグナルを受信した
				       時間内に処理を終えることができないので、グレースフルシャットダウンに失敗した場合
					   リクエストハンドラーが、伝搬されたキャンセルに従ってキャンセル処理を遂行しなかった
					   強制終了された`,
			opts: options{
				WaitSecondsUntilGracefulShutdownIsStarted:   1,
				GracefulShutdownTimeoutSeconds:              10,
				ForcefullyRequestCancellationTimeoutSeconds: 10,
			},
			executionBeforeGracefulShutdown: func() {
				res, err := http.DefaultClient.Get("http://127.0.0.1:8080/sleep_infinity")
				require.NoError(t, err)
				require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
			},
			expectedExitCode: shutodownForcefully,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			catchedShutdownSignal := atomic.Bool{}

			wg := sync.WaitGroup{}

			mockCtx, cancelMockCtx := context.WithCancelCause(context.Background())
			defer cancelMockCtx(nil)

			wg.Go(func() {
				// サーバーを起動する
				actual := runHandlerWithGracefulShutdown(
					mockCtx,
					testHandler,
					dummyServerPort,
					tC.opts,
					&catchedShutdownSignal,
				)

				assert.Equal(t, tC.expectedExitCode, actual)
			})

			waitUntilServerUp()

			wg.Go(func() {
				tC.executionBeforeGracefulShutdown()
			})
			time.Sleep(time.Second)

			// シャットダウンシグナル受信（の模倣）
			beforeRecieveShutdownSignal := time.Now()
			cancelMockCtx(dummySigTerm)
			// シグナル受信後、catchedShutdownSignal が即座に true となるか
			for !catchedShutdownSignal.Load() {
				time.Sleep(time.Duration(100) * time.Millisecond)
			}
			afterHealthCheckWasDown := time.Now()
			// シャットダウンシグナル受信後、即座に GET /health が 503 となったか？の確認
			// 「即座に」が1秒後で良いのか？という問題はあるが...
			assert.Less(t, afterHealthCheckWasDown.Sub(beforeRecieveShutdownSignal), time.Duration(1)*time.Second)

			wg.Wait()
		})
	}
}
