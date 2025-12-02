package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/suzuito/playground2-go/internal/gracefulshutdownexample"
)

func main() {
	gracefulShutdownProcTimeoutSecondsString := os.Getenv("GRACEFUL_SHUTDOWN_PROC_TIMEOUT_SECONDS")
	gracefulShutdownProcTimeoutSeconds, err := strconv.Atoi(gracefulShutdownProcTimeoutSecondsString)
	if err != nil {
		panic(err)
	}
	os.Exit(runHandlerWithGracefulShutdown(gracefulShutdownProcTimeoutSeconds, gracefulshutdownexample.NewHandler()))
}

// グレースフルシャットダウン付HTTPサーバーのサンプル実装
func runHandlerWithGracefulShutdown(
	gracefulShutdownProcTimeoutSeconds int,
	handler http.Handler,
) int {

	server := http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	// シグナルハンドラーの登録
	// ctxSignal は、シグナルをキャッチしたらctxSignal.Done()チャンネルがクローズされる
	ctxSignal, stop := signal.NotifyContext(
		context.Background(),

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
		// シグナルの受信

		// シグナルハンドラーを解除するために stop 関数を実行する
		stop()
	}

	// シグナル受信後の処理をここから下に書く

	fmt.Printf("catch signal: %+v\n", context.Cause(ctxSignal))

	// シグナル受信後の待ち時間設定
	// シグナルを受信してからサーバーを Graceful shutdown するまでの待ち時間
	// この待ち時間をどの程度の幅にするか？は、いろいろ考えらえる
	ctxTimeout, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(gracefulShutdownProcTimeoutSeconds)*time.Second,
	)
	defer cancel()

	// グレースフルシャットダウンの実行
	// contextのキャンセルが発生した場合(例えば、シグナル受信後の待ち時間以内にグレースフルシャットダウンできなかった、など)
	// Shutdownメソッドはエラーを返す
	if err := server.Shutdown(ctxTimeout); err != nil {
		fmt.Printf("failed to graceful shutdown: %+v\n", err)
		return 2
	}

	fmt.Println("graceful shutdown is ok")
	return 0
}
