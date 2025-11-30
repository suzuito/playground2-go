package gracefulshutdown

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HTTPサーバーのグレースフルシャットダウンが適切に実施されることを確認するテスト
// テストコード中で、HTTPサーバーのソースコードのビルド〜ビルド後のバイナリファイルの起動〜グレースフルシャットダウンの実行
// を通して適切にグレースフルシャットダウンできているか？を確認する
func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

// テスト対象となるバイナリファイルの名前
var testTargetBinNames = []string{"ex0001.cmd", "ex0002.cmd"}

type testCase struct {
	// HTTPリクエストの処理にかかる時間(秒)
	RequestProcSeconds int

	// グレースフルシャットダウンの処理のタイムアウト時間(秒)
	// すなわちシグナル受信〜シャットダウンまでの待ち時間
	GracefulShutdownProcTimeoutSeconds int

	// バイナリファイルへ送られるシグナル
	SignalToSend os.Signal
}

func runTestMain(
	m *testing.M,
) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Printf("failed to get wd: %v", err)
		return 1
	}

	// GoのソースコードからHTTPサーバーをビルドする
	for _, binName := range testTargetBinNames {
		buildCmd := exec.Command("make", binName)
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		buildCmd.Dir = path.Join(wd, "..", "..", "..")
		if err := runCommand(buildCmd); err != nil {
			fmt.Printf("failed to build command: %v", err)
			return 1
		}
	}

	return m.Run()
}

func runCommand(cmd *exec.Cmd) error {
	fmt.Fprintf(cmd.Stdout, "==== Executed command started on %s ====\n", cmd.Dir) //nolint:errcheck
	fmt.Fprintf(cmd.Stdout, "$ %s\n", strings.Join(cmd.Args, " "))                 //nolint:errcheck
	err := cmd.Run()
	fmt.Fprintln(cmd.Stdout, "==== Executed command finished ====") //nolint:errcheck
	return err
}

type startServerParam struct {
	BinName                            string
	Stdout                             *bytes.Buffer
	Stderr                             *bytes.Buffer
	GracefulShutdownProcTimeoutSeconds int
}

func (t startServerParam) String() string {
	s := ""
	s += "**** STDOUT ****\n"
	s += t.Stdout.String()
	s += "\n"
	s += "**** STDERR ****\n"
	s += t.Stderr.String()
	s += "\n"
	return s
}

// サーバー(バイナリファイル)を起動する
func startServer(p startServerParam) (*exec.Cmd, error) {
	testTargetCmd := exec.Command(fmt.Sprintf("../../../%s", p.BinName))
	if p.Stdout != nil {
		testTargetCmd.Stdout = p.Stdout
	}
	if p.Stderr != nil {
		testTargetCmd.Stderr = p.Stderr
	}
	testTargetCmd.Env = append(testTargetCmd.Env, "GRACEFUL_SHUTDOWN_PROC_TIMEOUT_SECONDS="+strconv.Itoa(p.GracefulShutdownProcTimeoutSeconds))

	if err := testTargetCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start target command: %w", err)
	}
	fmt.Println("server started")

	for {
		fmt.Println("waiting for up")
		time.Sleep(time.Second)
		res, err := http.DefaultClient.Get("http://localhost:8080/health")
		if err != nil {
			continue
		}
		res.Body.Close() //nolint:errcheck

		if res.StatusCode == http.StatusOK {
			break
		}
	}
	fmt.Println("server is up")

	return testTargetCmd, nil
}

func TestGsIsOK_処理中のリクエストの完了を待ってからグレースフルシャットダウンする(t *testing.T) {
	testCases := []testCase{
		{
			RequestProcSeconds:                 3,
			GracefulShutdownProcTimeoutSeconds: 10,
			SignalToSend:                       syscall.SIGINT,
		},
		{
			RequestProcSeconds:                 3,
			GracefulShutdownProcTimeoutSeconds: 10,
			SignalToSend:                       syscall.SIGTERM,
		},
	}
	for _, binName := range testTargetBinNames {
		for _, c := range testCases {
			t.Run(fmt.Sprintf("%s-%s", binName, c.SignalToSend.String()), func(t *testing.T) {
				p := startServerParam{
					BinName:                            binName,
					Stdout:                             bytes.NewBufferString(""),
					Stderr:                             bytes.NewBufferString(""),
					GracefulShutdownProcTimeoutSeconds: c.GracefulShutdownProcTimeoutSeconds,
				}
				testTargetCmd, err := startServer(p)
				require.NoError(t, err)

				wg := sync.WaitGroup{}

				wg.Go(func() {
					// シグナルをサーバープロセスへ送信する
					time.Sleep(time.Second)
					require.NoError(t, testTargetCmd.Process.Signal(c.SignalToSend))
				})

				wg.Go(func() {
					// サーバーへリクエストする
					res, err := http.DefaultClient.Get(fmt.Sprintf("http://localhost:8080/sleep?seconds=%d", c.RequestProcSeconds))
					if !assert.NoError(t, err) {
						return
					}
					assert.Equal(t, res.StatusCode, http.StatusOK)
				})

				var execerr *exec.ExitError
				if err := testTargetCmd.Wait(); err != nil && !errors.As(err, &execerr) {
					t.Errorf("failed to wait: %+v", err)
					return
				}

				wg.Wait()

				fmt.Println(p.String())

				require.True(t, testTargetCmd.ProcessState.Exited())
				assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 0)
			})
		}
	}
}

func TestGsIsError_処理中のリクエストが完了しなかった場合は強制シャットダウンする(t *testing.T) {
	testCases := []testCase{
		{
			RequestProcSeconds:                 9999,
			GracefulShutdownProcTimeoutSeconds: 10,
			SignalToSend:                       syscall.SIGINT,
		},
	}
	for _, binName := range testTargetBinNames {
		for _, c := range testCases {
			t.Run(fmt.Sprintf("%s-%s", binName, c.SignalToSend.String()), func(t *testing.T) {
				p := startServerParam{
					BinName:                            binName,
					Stdout:                             bytes.NewBufferString(""),
					Stderr:                             bytes.NewBufferString(""),
					GracefulShutdownProcTimeoutSeconds: c.GracefulShutdownProcTimeoutSeconds,
				}
				testTargetCmd, err := startServer(p)
				require.NoError(t, err)

				wg := sync.WaitGroup{}

				wg.Go(func() {
					// シグナルをサーバープロセスへ送信する
					time.Sleep(time.Second)
					require.NoError(t, testTargetCmd.Process.Signal(c.SignalToSend))
				})

				wg.Go(func() {
					// サーバーへリクエストする
					_, err := http.DefaultClient.Get(fmt.Sprintf("http://localhost:8080/sleep?seconds=%d", c.RequestProcSeconds))
					assert.Error(t, err)
					assert.ErrorIs(t, err, io.EOF)
				})

				var execerr *exec.ExitError
				if err := testTargetCmd.Wait(); err != nil && !errors.As(err, &execerr) {
					t.Errorf("failed to wait: %+v", err)
					return
				}

				wg.Wait()

				fmt.Println(p.String())

				require.True(t, testTargetCmd.ProcessState.Exited())
				assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 2)
			})
		}
	}
}

func TestGsIsOK_シグナルを受信した後はリクエストを受け付けなくなる(t *testing.T) {
	testCases := []testCase{
		{
			RequestProcSeconds:                 9,
			GracefulShutdownProcTimeoutSeconds: 10,
			SignalToSend:                       syscall.SIGINT,
		},
	}
	for _, binName := range testTargetBinNames {
		for _, c := range testCases {
			t.Run(fmt.Sprintf("%s-%s", binName, c.SignalToSend.String()), func(t *testing.T) {
				p := startServerParam{
					BinName:                            binName,
					Stdout:                             bytes.NewBufferString(""),
					Stderr:                             bytes.NewBufferString(""),
					GracefulShutdownProcTimeoutSeconds: c.GracefulShutdownProcTimeoutSeconds,
				}
				testTargetCmd, err := startServer(p)
				require.NoError(t, err)

				wg := sync.WaitGroup{}

				chIsSignalSent := make(chan struct{})

				wg.Go(func() {
					// シグナルをサーバープロセスへ送信する
					time.Sleep(time.Second)
					require.NoError(t, testTargetCmd.Process.Signal(c.SignalToSend))
					close(chIsSignalSent)
				})

				wg.Go(func() {
					// サーバーへリクエストする
					_, err := http.DefaultClient.Get(fmt.Sprintf("http://localhost:8080/sleep?seconds=%d", c.RequestProcSeconds))
					assert.NoError(t, err)
				})

				wg.Go(func() {
					<-chIsSignalSent
					time.Sleep(time.Second)
					_, err := http.DefaultClient.Get("http://localhost:8080/sleep?seconds=0")
					assert.Error(t, err)
					var urlerr *url.Error
					assert.ErrorAs(t, err, &urlerr)
				})

				var execerr *exec.ExitError
				if err := testTargetCmd.Wait(); err != nil && !errors.As(err, &execerr) {
					t.Errorf("failed to wait: %+v", err)
					return
				}

				wg.Wait()

				fmt.Println(p.String())

				require.True(t, testTargetCmd.ProcessState.Exited())
				assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 0)
			})
		}
	}
}
