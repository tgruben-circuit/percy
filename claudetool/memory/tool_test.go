package memory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	memdb "github.com/tgruben-circuit/percy/memory"
)

func TestMemorySearchTool(t *testing.T) {
	dir := t.TempDir()
	db, err := memdb.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a topic with summary.
	db.UpsertTopic(memdb.Topic{
		TopicID: "topic_auth", Name: "authentication",
		Summary: "JWT authentication for the API gateway with token validation.",
	})

	// Seed the DB with cells.
	cells := []memdb.Cell{
		{CellID: "c1", TopicID: "topic_auth", SourceType: "conversation", SourceID: "conv-1",
			SourceName: "Auth Discussion", CellType: "decision", Salience: 0.9,
			Content: "We decided to use JWT authentication for the API gateway"},
		{CellID: "c2", TopicID: "topic_auth", SourceType: "conversation", SourceID: "conv-1",
			SourceName: "Auth Discussion", CellType: "code_ref", Salience: 0.7,
			Content: "The authentication middleware validates tokens and extracts user claims"},
		{CellID: "c3", TopicID: "topic_auth", SourceType: "file", SourceID: "file-1",
			SourceName: "auth.go", CellType: "code_ref", Salience: 0.6,
			Content: "Package auth provides authentication helpers including token verification"},
	}
	for _, c := range cells {
		if err := db.InsertCell(c); err != nil {
			t.Fatalf("InsertCell(%s): %v", c.CellID, err)
		}
	}

	tool := NewMemorySearchTool(db, nil)
	input, err := json.Marshal(searchInput{Query: "authentication"})
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}
	result := tool.Run(context.Background(), input)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.LLMContent) == 0 {
		t.Fatal("expected non-empty LLMContent")
	}
	text := result.LLMContent[0].Text
	if text == "" {
		t.Fatal("expected non-empty text in LLMContent")
	}
	if !strings.Contains(text, "relevant memories") {
		t.Errorf("expected results header, got: %s", text)
	}
	// Should contain a topic summary.
	if !strings.Contains(text, "Topic Summary") {
		t.Errorf("expected 'Topic Summary' in output, got: %s", text)
	}
	t.Logf("result: %s", text)
}

func TestMemorySearchToolNoDatabase(t *testing.T) {
	tool := NewMemorySearchTool(nil, nil)
	input, err := json.Marshal(searchInput{Query: "anything"})
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}
	result := tool.Run(context.Background(), input)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.LLMContent) == 0 {
		t.Fatal("expected non-empty LLMContent")
	}
	text := result.LLMContent[0].Text
	if !strings.Contains(text, "No memory index found") {
		t.Errorf("expected helpful nil-db message, got: %s", text)
	}
}

func TestMemorySearchToolEmptyResults(t *testing.T) {
	dir := t.TempDir()
	db, err := memdb.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tool := NewMemorySearchTool(db, nil)
	input, err := json.Marshal(searchInput{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}
	result := tool.Run(context.Background(), input)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.LLMContent) == 0 {
		t.Fatal("expected non-empty LLMContent")
	}
	text := result.LLMContent[0].Text
	if !strings.Contains(text, "No relevant memories found") {
		t.Errorf("expected no-results message, got: %s", text)
	}
}

func TestMemorySearchToolSummaryOnly(t *testing.T) {
	dir := t.TempDir()
	db, err := memdb.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.UpsertTopic(memdb.Topic{
		TopicID: "topic_auth", Name: "authentication",
		Summary: "JWT authentication for the API gateway.",
	})
	db.InsertCell(memdb.Cell{
		CellID: "c1", TopicID: "topic_auth", SourceType: "conversation",
		SourceID: "conv-1", CellType: "decision", Salience: 0.9,
		Content: "We decided to use JWT",
	})

	tool := NewMemorySearchTool(db, nil)
	input, _ := json.Marshal(searchInput{Query: "authentication", DetailLevel: "summary"})
	result := tool.Run(context.Background(), input)

	text := result.LLMContent[0].Text
	if !strings.Contains(text, "Topic Summary") {
		t.Errorf("expected topic summary, got: %s", text)
	}
	// Should NOT contain individual cell results.
	if strings.Contains(text, "[decision]") {
		t.Errorf("summary mode should not include individual cells, got: %s", text)
	}
}
