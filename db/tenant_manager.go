package db

import (
	"database/sql"
	"log"
	"net"
	"os"
	"sync"

	mysql "github.com/go-sql-driver/mysql"
)

var (
	tenantDBMap = make(map[string]*sql.DB)
	mu          sync.Mutex
)

// =========================
// GET TENANT DB (WITH POOLING)
// =========================
func GetTenantDB(dbName string) (*sql.DB, error) {

	mu.Lock()
	defer mu.Unlock()

	// kalau sudah ada → pakai lagi
	if conn, ok := tenantDBMap[dbName]; ok {
		return conn, nil
	}

	// kalau belum → buat koneksi baru
	host := envOrDefault("DB_HOST", "127.0.0.1")
	port := envOrDefault("DB_PORT", "3306")
	user := envOrDefault("DB_USER", "root")
	password := os.Getenv("DB_PASSWORD")
		config := mysql.Config{
		User:                 user,
		Passwd:               password,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(host, port),
		DBName:               dbName,
		ParseTime:            true,
		AllowNativePasswords: true,
		Params:               map[string]string{"charset": "utf8mb4"},
	}
	dsn := config.FormatDSN()

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	if err := RunTenantSchema(conn); err != nil {
		conn.Close()
		return nil, err
	}
	if err := MigrateTenantRoles(conn); err != nil {
		conn.Close()
		return nil, err
	}

	// connection pooling setting
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(0)

	tenantDBMap[dbName] = conn

	log.Println("✅ Connected tenant DB:", dbName)

	return conn, nil
}

func CloseTenantDB(dbName string) {
	mu.Lock()
	defer mu.Unlock()
	if conn, ok := tenantDBMap[dbName]; ok {
		if err := conn.Close(); err != nil {
			log.Printf("failed to close tenant database %s: %v", dbName, err)
		}
		delete(tenantDBMap, dbName)
	}
}
