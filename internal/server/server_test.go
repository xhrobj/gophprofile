package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRun_StopsAfterContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var output bytes.Buffer
	lg := testLogger(&output)
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, listener, httpServer, time.Second, lg)
	}()

	waitForServer(t, listener.Addr().String())
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}

	messages := logMessages(t, &output)
	want := []string{"server started", "server shutdown started", "server stopped"}
	if len(messages) != len(want) {
		t.Fatalf("log messages = %v, want %v", messages, want)
	}
	for index := range want {
		if messages[index] != want[index] {
			t.Errorf("log message %d = %q, want %q", index, messages[index], want[index])
		}
	}
}

func TestRun_ForcesCloseAfterShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	requestStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	t.Cleanup(func() {
		close(releaseHandler)
	})

	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/block" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			close(requestStarted)
			<-releaseHandler
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	const shutdownTimeout = 50 * time.Millisecond
	go func() {
		done <- Run(ctx, listener, httpServer, shutdownTimeout, testLogger(&bytes.Buffer{}))
	}()

	waitForServer(t, listener.Addr().String())

	requestErrors := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, requestErr := client.Get("http://" + listener.Addr().String() + "/block")
		if requestErr == nil {
			requestErr = response.Body.Close()
		}
		requestErrors <- requestErr
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("blocking request did not reach the handler")
	}

	startedAt := time.Now()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want %v", err, context.DeadlineExceeded)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run() did not force-close the server after shutdown timeout")
	}

	if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
		t.Fatalf("Run() stopped after %s, want less than 500ms", elapsed)
	}

	select {
	case err := <-requestErrors:
		if err == nil {
			t.Fatal("blocking request completed without an error after forced close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("forced close did not interrupt the active connection")
	}
}

func TestRun_ReturnsServeError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = Run(
		context.Background(),
		listener,
		&http.Server{ReadHeaderTimeout: time.Second},
		time.Second,
		testLogger(&bytes.Buffer{}),
	)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Run() error = %v, want %v", err, net.ErrClosed)
	}
}

func testLogger(output *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(output),
		zapcore.DebugLevel,
	)

	return zap.New(core).With(zap.String("service", "server"))
}

func waitForServer(t *testing.T, address string) {
	t.Helper()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(time.Second)
	url := "http://" + address

	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Fatalf("close response body: %v", closeErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("server did not start on %s", address)
}

func logMessages(t *testing.T, output *bytes.Buffer) []string {
	t.Helper()

	scanner := bufio.NewScanner(output)
	var messages []string
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		message, _ := entry["msg"].(string)
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log output: %v", err)
	}

	return messages
}
