package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed master_migrations/*.sql
var masterMigrationFiles embed.FS

func runMasterMigrations(database *sql.DB) error {
	migrationFS, err := fs.Sub(masterMigrationFiles, "master_migrations")
	if err != nil {
		return fmt.Errorf("load embedded migration files: %w", err)
	}

	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("configure MySQL migration dialect: %w", err)
	}
	if err := goose.Up(database, "."); err != nil {
		return fmt.Errorf("run pending migrations: %w", err)
	}
	return nil
}
