package migration

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgx5 "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/xhrobj/gophprofile/migrations"
)

// Run применяет все ещё не выполненные миграции PostgreSQL.
func Run(pool *pgxpool.Pool) error {
	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("create embedded migration source: %w", err)
	}

	database := stdlib.OpenDBFromPool(pool)
	databaseDriver, err := pgx5.WithInstance(database, &pgx5.Config{
		SchemaName: "public",
	})
	if err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()

		return fmt.Errorf("create postgres migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance(
		"iofs",
		sourceDriver,
		"pgx5",
		databaseDriver,
	)
	if err != nil {
		_ = sourceDriver.Close()
		_ = databaseDriver.Close()

		return fmt.Errorf("create migrator: %w", err)
	}

	migrationErr := migrator.Up()
	sourceCloseErr, databaseCloseErr := migrator.Close()

	if migrationErr != nil && !errors.Is(migrationErr, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", migrationErr)
	}

	if sourceCloseErr != nil {
		return fmt.Errorf("close migration source: %w", sourceCloseErr)
	}

	if databaseCloseErr != nil {
		return fmt.Errorf("close migration database: %w", databaseCloseErr)
	}

	return nil
}
