package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/migration"
	"github.com/xhrobj/gophprofile/internal/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadMigration()
	if err != nil {
		return fmt.Errorf("load migration config: %w", err)
	}

	pool, err := postgres.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()

	if err := migration.Run(pool); err != nil {
		return fmt.Errorf("run PostgreSQL migrations: %w", err)
	}

	return nil
}
