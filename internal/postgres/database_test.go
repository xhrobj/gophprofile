package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOpen_ReturnsParseError(t *testing.T) {
	pool, err := Open(context.Background(), "://")
	if pool != nil {
		t.Cleanup(pool.Close)
	}

	if err == nil {
		t.Fatal("Open() error = nil, want DSN parsing error")
	}

	if pool != nil {
		t.Fatal("Open() pool is not nil after error")
	}

	if !strings.Contains(err.Error(), "create PostgreSQL pool") {
		t.Fatalf("Open() error = %q, want create pool context", err)
	}
}

func TestOpen_ReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pool, err := Open(
		ctx,
		"postgres://user:password@localhost:5432/database?sslmode=disable",
	)
	if pool != nil {
		t.Cleanup(pool.Close)
	}

	if err == nil {
		t.Fatal("Open() error = nil, want context cancellation error")
	}

	if pool != nil {
		t.Fatal("Open() pool is not nil after error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
}

func TestOpen_ReturnsErrorForEmptyDSN(t *testing.T) {
	pool, err := Open(context.Background(), "")
	if err == nil {
		t.Fatal("Open() error = nil, want empty DSN error")
	}

	if pool != nil {
		t.Cleanup(pool.Close)
		t.Fatal("Open() pool is not nil after error")
	}

	if !strings.Contains(err.Error(), "database DSN is empty") {
		t.Fatalf("Open() error = %q, want empty DSN context", err)
	}
}
