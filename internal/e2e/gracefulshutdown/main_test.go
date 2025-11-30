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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m, buildCommand{
		Name: "make",
		Args: []string{"ex0001.cmd"},
	}))
	os.Exit(runTestMain(m, buildCommand{
		Name: "make",
		Args: []string{"ex0002.cmd"},
	}))
}

type buildCommand struct {
	Name string
	Args []string
}

func runTestMain(
	m *testing.M,
	bc buildCommand,
) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Printf("failed to get wd: %v", err)
		return 1
	}

	buildCmd := exec.Command(bc.Name, bc.Args...)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	buildCmd.Dir = path.Join(wd, "..", "..", "..")
	if err := runCommand(buildCmd); err != nil {
		fmt.Printf("failed to build command: %v", err)
		return 1
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
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (t startServerParam) String() string {
	s := ""
	s += "**** STDOUT ****\n"
	s += t.stdout.String()
	s += "\n"
	s += "**** STDERR ****\n"
	s += t.stderr.String()
	s += "\n"
	return s
}

func startServer(p startServerParam) (*exec.Cmd, error) {
	testTargetCmd := exec.Command("../../../ex0001.cmd")
	if p.stdout != nil {
		testTargetCmd.Stdout = p.stdout
	}
	if p.stderr != nil {
		testTargetCmd.Stderr = p.stderr
	}

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
	testCases := []struct {
		signalToSend os.Signal
	}{
		{signalToSend: syscall.SIGINT},
		{signalToSend: syscall.SIGTERM},
	}
	for _, c := range testCases {
		t.Run(c.signalToSend.String(), func(t *testing.T) {
			startServerParam := startServerParam{
				stdout: bytes.NewBufferString(""),
				stderr: bytes.NewBufferString(""),
			}
			testTargetCmd, err := startServer(startServerParam)
			require.NoError(t, err)

			wg := sync.WaitGroup{}

			wg.Go(func() {
				time.Sleep(time.Second)
				require.NoError(t, testTargetCmd.Process.Signal(c.signalToSend))
			})

			wg.Go(func() {
				res, err := http.DefaultClient.Get("http://localhost:8080/sleep3secs")
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

			fmt.Println(startServerParam.String())

			require.True(t, testTargetCmd.ProcessState.Exited())
			assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 0)
		})
	}
}

func TestGsIsOK_処理中のリクエストが完了しなかった場合は強制シャットダウンする(t *testing.T) {
	testCases := []struct {
		signalToSend os.Signal
	}{
		{signalToSend: syscall.SIGINT},
	}
	for _, c := range testCases {
		t.Run(c.signalToSend.String(), func(t *testing.T) {
			startServerParam := startServerParam{
				stdout: bytes.NewBufferString(""),
				stderr: bytes.NewBufferString(""),
			}
			testTargetCmd, err := startServer(startServerParam)
			require.NoError(t, err)

			wg := sync.WaitGroup{}

			wg.Go(func() {
				time.Sleep(time.Second)
				require.NoError(t, testTargetCmd.Process.Signal(c.signalToSend))
			})

			wg.Go(func() {
				_, err := http.DefaultClient.Get("http://localhost:8080/sleep30secs")
				assert.Error(t, err)
				assert.ErrorIs(t, err, io.EOF)
			})

			var execerr *exec.ExitError
			if err := testTargetCmd.Wait(); err != nil && !errors.As(err, &execerr) {
				t.Errorf("failed to wait: %+v", err)
				return
			}

			wg.Wait()

			fmt.Println(startServerParam.String())

			require.True(t, testTargetCmd.ProcessState.Exited())
			assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 2)
		})
	}
}

func TestGsIsOK_シグナルを受信した後はリクエストを受け付けなくなる(t *testing.T) {
	testCases := []struct {
		signalToSend os.Signal
	}{
		{signalToSend: syscall.SIGINT},
	}
	for _, c := range testCases {
		t.Run(c.signalToSend.String(), func(t *testing.T) {
			startServerParam := startServerParam{
				stdout: bytes.NewBufferString(""),
				stderr: bytes.NewBufferString(""),
			}
			testTargetCmd, err := startServer(startServerParam)
			require.NoError(t, err)

			wg := sync.WaitGroup{}

			chIsSignalSent := make(chan struct{})

			wg.Go(func() {
				time.Sleep(time.Second)
				require.NoError(t, testTargetCmd.Process.Signal(c.signalToSend))
				close(chIsSignalSent)
			})

			wg.Go(func() {
				_, err := http.DefaultClient.Get("http://localhost:8080/sleep30secs")
				assert.Error(t, err)
				assert.ErrorIs(t, err, io.EOF)
			})

			wg.Go(func() {
				<-chIsSignalSent
				time.Sleep(time.Second)
				_, err := http.DefaultClient.Get("http://localhost:8080/sleep30secs")
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

			fmt.Println(startServerParam.String())

			require.True(t, testTargetCmd.ProcessState.Exited())
			assert.Equal(t, testTargetCmd.ProcessState.ExitCode(), 2)
		})
	}
}
