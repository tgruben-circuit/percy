package memory

import (
	"path/filepath"
	"testing"
)

func TestFTS(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	// Insert 3 conversation chunks and 1 file chunk.
	chunks := []struct {
		id, srcType, srcID, srcName string
		index                       int
		text                        string
	}{
		{"conv-1-0", "conversation", "conv-1", "Auth Discussion", 0,
			"We need to implement authentication using JWT tokens for the API gateway"},
		{"conv-1-1", "conversation", "conv-1", "Auth Discussion", 1,
			"The authentication middleware should validate tokens and extract user claims"},
		{"conv-2-0", "conversation", "conv-2", "Database Design", 0,
			"The database schema uses PostgreSQL with separate tables for users and sessions"},
		{"file-1-0", "file", "file-1", "auth.go", 0,
			"Package auth provides authentication helpers including JWT token verification"},
	}

	for _, c := range chunks {
		if err := mdb.InsertChunk(c.id, c.srcType, c.srcID, c.srcName, c.index, c.text, nil); err != nil {
			t.Fatalf("InsertChunk(%s): %v", c.id, err)
		}
	}

	// 1. Search for "authentication" — should return results.
	results, err := mdb.SearchFTS("authentication", "", 10)
	if err != nil {
		t.Fatalf("SearchFTS(authentication): %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'authentication', got 0")
	}
	t.Logf("search 'authentication' returned %d results", len(results))
	for _, r := range results {
		t.Logf("  chunk=%s source_type=%s score=%.4f", r.ChunkID, r.SourceType, r.Score)
	}

	// 2. Search with source_type filter "file" — should return exactly 1 result.
	fileResults, err := mdb.SearchFTS("authentication", "file", 10)
	if err != nil {
		t.Fatalf("SearchFTS(authentication, file): %v", err)
	}
	if len(fileResults) != 1 {
		t.Fatalf("expected 1 file result, got %d", len(fileResults))
	}
	if fileResults[0].SourceType != "file" {
		t.Errorf("expected source_type=file, got %s", fileResults[0].SourceType)
	}

	// 3. Search for "kubernetes" — should return 0 results.
	noResults, err := mdb.SearchFTS("kubernetes", "", 10)
	if err != nil {
		t.Fatalf("SearchFTS(kubernetes): %v", err)
	}
	if len(noResults) != 0 {
		t.Fatalf("expected 0 results for 'kubernetes', got %d", len(noResults))
	}
}
