package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

var Master *sql.DB

var masterDatabaseNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func ConnectMaster() error {
	host := envOrDefault("DB_HOST", "127.0.0.1")
	port := envOrDefault("DB_PORT", "3306")
	user := envOrDefault("DB_USER", "root")
	password := os.Getenv("DB_PASSWORD")
	database := envOrDefault("DB_MASTER_NAME", "assistant")
	if !masterDatabaseNamePattern.MatchString(database) {
		return fmt.Errorf("invalid DB_MASTER_NAME %q: only letters, numbers, and underscores are allowed", database)
	}

		config := mysql.Config{
			User:                 user,
			Passwd:               password,
			Net:                  "tcp",
			Addr:                 net.JoinHostPort(host, port),
			ParseTime:            true,
			Timeout:              5 * time.Second,
			AllowNativePasswords: true,
			Params:               map[string]string{"charset": "utf8mb4"},
		}

	ctx := context.Background()
	bootstrap, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return fmt.Errorf("open MySQL bootstrap connection: %w", err)
	}
	if err := bootstrap.PingContext(ctx); err != nil {
		bootstrap.Close()
		return fmt.Errorf("connect to MySQL at %s: %w", net.JoinHostPort(host, port), err)
	}
	if _, err := bootstrap.ExecContext(
		ctx,
		"CREATE DATABASE IF NOT EXISTS `"+database+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
	); err != nil {
		bootstrap.Close()
		return fmt.Errorf("create master database %q: %w", database, err)
	}
	if err := bootstrap.Close(); err != nil {
		return fmt.Errorf("close MySQL bootstrap connection: %w", err)
	}

	config.DBName = database
	Master, err = sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return fmt.Errorf("open master database connection: %w", err)
	}
	if err := Master.PingContext(ctx); err != nil {
		Master.Close()
		Master = nil
		return fmt.Errorf("connect to master database %q: %w", database, err)
	}
	if err := runMasterMigrations(Master); err != nil {
		Master.Close()
		Master = nil
		return fmt.Errorf("apply master database migrations: %w", err)
	}

	log.Println("Master DB connected; migrations are up to date")
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
