package memory

import (
	"path/filepath"
	"testing"
)

func TestTwoTierSearch(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	// Create topic with summary.
	mdb.UpsertTopic(Topic{
		TopicID: "topic_auth", Name: "authentication",
		Summary: "Authentication uses JWT with RS256 signing. Tokens in httpOnly cookies.",
	})

	// Create cells.
	mdb.InsertCell(Cell{
		CellID: "c1", TopicID: "topic_auth", SourceType: "conversation",
		SourceID: "conv1", CellType: "decision", Salience: 0.9,
		Content: "Switched from bcrypt to argon2id for password hashing",
	})
	mdb.InsertCell(Cell{
		CellID: "c2", TopicID: "topic_auth", SourceType: "conversation",
		SourceID: "conv1", CellType: "code_ref", Salience: 0.7,
		Content: "server/auth.go handles JWT authentication validation middleware",
	})

	results, err := mdb.TwoTierSearch("JWT authentication", nil, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// Should have at least one topic_summary result.
	foundSummary := false
	foundCell := false
	for _, r := range results {
		if r.ResultType == "topic_summary" {
			foundSummary = true
		}
		if r.ResultType == "cell" {
			foundCell = true
		}
	}
	if !foundSummary {
		t.Error("expected at least one topic_summary result")
	}
	if !foundCell {
		t.Error("expected at least one cell result")
	}
}
