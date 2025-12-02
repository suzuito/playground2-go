package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/suzuito/playground2-go/internal/gracefulshutdownexample"
)

func main() {
	isGracefulShutdownProcStarted := atomic.Bool{}
	handler := gracefulshutdownexample.NewHandler(&isGracefulShutdownProcStarted)
	server := http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	os.Exit(runHandlerWithGracefulShutdown(
		context.Background(),
		options{
			GracefulShutdownProcTimeoutSeconds:              10,
			ForcefullyHttpRequestCancellationTimeoutSeconds: 3,
			IsGracefulShutdownProcStarted:                   &isGracefulShutdownProcStarted,
		},
		&server,
	))
}

type options struct {
	// シグナル受信後、処理中であるHTTPリクエストが終了するまで待つ時間(秒)
	GracefulShutdownProcTimeoutSeconds int

	// 強制的なHTTPリクエストキャンセルが終了するまで待つ時間(秒)
	ForcefullyHttpRequestCancellationTimeoutSeconds int

	// グレースフルシャットダウンが開始されたら true となる
	IsGracefulShutdownProcStarted *atomic.Bool
}

// グレースフルシャットダウン付HTTPサーバーのサンプル実装
func runHandlerWithGracefulShutdown(
	ctx context.Context,
	opts options,
	server *http.Server,
) int {

	ctxBaseRequest, cancelCtxBaseRequest := context.WithCancel(context.Background())
	defer cancelCtxBaseRequest()
	server.BaseContext = func(l net.Listener) context.Context {
		return ctxBaseRequest
	}

	// シグナルハンドラーの登録
	// ctxSignal は、シグナルをキャッチしたらctxSignal.Done()チャンネルがクローズされる
	ctxSignal, stop := signal.NotifyContext(
		ctx,

		// キャッチするシグナルの種類を指定する

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

	// サーバーの起動
	chTCPListenIsDone := make(chan error)
	go func() {
		fmt.Println("server started")

		// リスン状態を開始する
		// 意図的なリスン状態の終了(Server.Shutdown または Server.Close が実行されたことによる終了)においては
		// ListenAndServeメソッドは ErrServerClosed エラーを返す
		// そうでない場合においては、そのエラー内容を返す
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("server finished with error: %+v\n", err)
			chTCPListenIsDone <- err
		} else {
			fmt.Println("server finished")
		}

		close(chTCPListenIsDone)
	}()

	// サーバーの終了、または、シグナルの受信、を待つ
	select {
	case err := <-chTCPListenIsDone:
		// サーバーの終了

		if err != nil {
			// シグナルを受信していないけどなんらかの理由でサーバーがエラー終了した場合、このパスが実行される
			fmt.Printf("server listen is finished with error: %+v\n", err)
			return 1
		}

		// シグナルを受信していないけどサーバーが正常終了した場合、このパスが実行される
		// シグナルを受信するまでサーバーが正常終了することはありえないため
		// 理論上、このパスを通ることは考えられないが
		// もしこのパスを通るとしたら、意味としては
		// エラーなくリスン状態を終了したことを意味する
		fmt.Println("server listen is finished")
		return 0
	case <-ctxSignal.Done():
		// シグナルを受信

		// シグナルハンドラーを解除するために stop 関数を実行する
		stop()

		if opts.IsGracefulShutdownProcStarted != nil {
			opts.IsGracefulShutdownProcStarted.Store(true)
		}
	}

	// シグナル受信後の処理をここから下に書く

	fmt.Printf("catch signal: %+v\n", context.Cause(ctxSignal))

	// シグナル受信後の待ち時間設定
	// シグナルを受信してからサーバーを Graceful shutdown するまでの待ち時間
	// この待ち時間をどの程度の幅にするか？は、いろいろ考えらえる
	ctxTimeout, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(opts.GracefulShutdownProcTimeoutSeconds)*time.Second,
	)
	defer cancel()

	// グレースフルシャットダウンの実行
	// contextのキャンセルが発生した場合(例えば、シグナル受信後の待ち時間以内にグレースフルシャットダウンできなかった、など)
	// Shutdownメソッドはエラーを返す
	err := server.Shutdown(ctxTimeout)
	if err != nil {
		cancelCtxBaseRequest()
		fmt.Printf("failed to graceful shutdown: %+v\n", err)
		fmt.Println("waiting to cancel http request is started")
		time.Sleep(time.Duration(opts.ForcefullyHttpRequestCancellationTimeoutSeconds) * time.Second)
		fmt.Println("waiting to cancel http request is finished")
		fmt.Println("exit server forcefully")
		return 2
	}

	fmt.Println("graceful shutdown is ok")
	return 0
}
