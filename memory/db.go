package memory

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	db   *sql.DB
	path string
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("memory: create dir: %w", err)
		}
	}

	dsn := path + "?_journal_mode=WAL"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memory: open: %w", err)
	}
	sqldb.SetMaxOpenConns(1)

	if _, err := sqldb.Exec(schemaSQL); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("memory: migrate: %w", err)
	}

	return &DB{db: sqldb, path: path}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func MemoryDBPath(percyDBPath string) string {
	dir := filepath.Dir(percyDBPath)
	return filepath.Join(dir, "memory.db")
}
