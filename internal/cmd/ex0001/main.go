package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /sleep3secs", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /sleep30secs", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Second)
		w.Write([]byte("ok"))
	})

	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// ctxSignal は、シグナルをキャッチしたらctxSignal.Done()チャンネルがクローズされる
	ctxSignal, stop := signal.NotifyContext(
		context.Background(),

		// SIGINT はUnix互換OSだけなので、OSの違いが吸収できるos.Interruptを使った方が良い
		// os.Interruptはプログラムの中断シグナル。プログラム実行中に Ctrl+C を叩くと、SIGINT シグナルがプログラムへ送られる。
		os.Interrupt,

		// SIGTERM はUnix互換OSにおけるプログラムの強制終了シグナル
		// Cloud 上のプロセス終了時、このシグナルを送るケースがある
		// 例えば Cloud Run
		// https://docs.cloud.google.com/run/docs/container-contract#instance-shutdown
		syscall.SIGTERM,
	)
	defer stop()

	chServerIsDone := make(chan error)
	go func() {
		fmt.Println("server started")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			chServerIsDone <- err
		}
		close(chServerIsDone)
	}()

	select {
	case <-ctxSignal.Done():
		// シグナルを受信した場合、このパスが実行される
		fmt.Printf("catch signal: %+v\n", context.Cause(ctxSignal))
	case err := <-chServerIsDone:
		// シグナルを受信していないけどなんらかの理由でサーバーがエラー終了した場合、このパスが実行される
		fmt.Printf("failed to server listen and serve: %+v\n", err)
		return 1
	}

	waitSecondsUntilShutdown := 10
	ctxTimeout, cancel := context.WithTimeout(
		context.Background(),

		// シグナルを受信してからサーバーを Graceful shutdown するまでの待ち時間
		// この待ち時間は、クラウドインフラによって様々。
		// https://docs.cloud.google.com/run/docs/container-contract#instance-shutdown
		// Cloud Run では、SIGTERMを送ってから10秒経ってもコンテナが生きている場合、SIGKILLを送る
		time.Duration(waitSecondsUntilShutdown)*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(ctxTimeout); err != nil {
		fmt.Printf("failed to graceful shutdown: %+v\n", err)
		return 2
	}
	fmt.Println("graceful shutdown is ok")

	return 0
}
