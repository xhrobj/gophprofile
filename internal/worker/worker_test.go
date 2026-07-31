package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRun_StopsAfterContextCancellation(t *testing.T) {
	var output bytes.Buffer
	lg := testLogger(&output)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, lg)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}

	scanner := bufio.NewScanner(&output)
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

	want := []string{"worker started", "worker stopped"}
	if len(messages) != len(want) {
		t.Fatalf("log messages = %v, want %v", messages, want)
	}
	for index := range want {
		if messages[index] != want[index] {
			t.Errorf("log message %d = %q, want %q", index, messages[index], want[index])
		}
	}
}

func testLogger(output *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(output),
		zapcore.DebugLevel,
	)

	return zap.New(core).With(zap.String("service", "worker"))
}
