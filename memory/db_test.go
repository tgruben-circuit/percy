package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	mdb, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file not created: %v", err)
	}

	_, err = mdb.db.Exec(`INSERT INTO chunks (chunk_id, source_type, source_id, source_name, chunk_index, text)
		VALUES ('test1', 'conversation', 'c1', 'test', 0, 'hello world')`)
	if err != nil {
		t.Fatalf("chunks table not created: %v", err)
	}

	_, err = mdb.db.Exec(`INSERT INTO index_state (source_type, source_id, indexed_at, hash)
		VALUES ('conversation', 'c1', datetime('now'), 'abc123')`)
	if err != nil {
		t.Fatalf("index_state table not created: %v", err)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	mdb1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mdb1.Close()

	mdb2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mdb2.Close()
}
