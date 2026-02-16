package memory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tgruben-circuit/percy/llm"
)

func TestIndexConversation(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	messages := []MessageText{
		{Role: "user", Text: "How do I implement JWT authentication in Go?"},
		{Role: "assistant", Text: "You can use the golang-jwt library to create and verify JWT tokens."},
		{Role: "user", Text: "Can you show me middleware for HTTP handlers?"},
		{Role: "assistant", Text: "Here is an authentication middleware that extracts and validates the bearer token."},
	}

	ctx := context.Background()
	err = mdb.IndexConversation(ctx, "conv-abc", "Auth Discussion", messages, nil, nil)
	if err != nil {
		t.Fatalf("IndexConversation: %v", err)
	}

	// Verify TwoTierSearch finds the indexed content.
	results, err := mdb.TwoTierSearch("authentication", nil, "conversation", 10)
	if err != nil {
		t.Fatalf("TwoTierSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results for 'authentication', got 0")
	}

	// Verify source info is correct.
	for _, r := range results {
		if r.ResultType == "cell" {
			if r.SourceName != "Auth Discussion" {
				t.Errorf("expected source_name='Auth Discussion', got %q", r.SourceName)
			}
			if r.SourceID != "conv-abc" {
				t.Errorf("expected source_id='conv-abc', got %q", r.SourceID)
			}
		}
	}
}

func TestIndexConversationSkipsIfUnchanged(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	messages := []MessageText{
		{Role: "user", Text: "Tell me about Kubernetes deployments."},
		{Role: "assistant", Text: "Kubernetes deployments manage replica sets and pod rollouts."},
	}

	ctx := context.Background()

	// First index.
	if err := mdb.IndexConversation(ctx, "conv-1", "K8s Chat", messages, nil, nil); err != nil {
		t.Fatalf("first IndexConversation: %v", err)
	}

	// Second index with same content should succeed (skip).
	if err := mdb.IndexConversation(ctx, "conv-1", "K8s Chat", messages, nil, nil); err != nil {
		t.Fatalf("second IndexConversation: %v", err)
	}

	// Verify content is still searchable after the skip.
	results, err := mdb.TwoTierSearch("Kubernetes", nil, "conversation", 10)
	if err != nil {
		t.Fatalf("TwoTierSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results after skip, got 0")
	}
}

func TestIndexFile(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	content := `# Authentication Module

This module provides JWT-based authentication for the API server.

## Token Generation

Tokens are generated using HMAC-SHA256 with a configurable secret key.

## Middleware

The HTTP middleware validates incoming bearer tokens and rejects expired ones.
`

	ctx := context.Background()
	err = mdb.IndexFile(ctx, "/src/auth.go", "auth.go", content, nil)
	if err != nil {
		t.Fatalf("IndexFile: %v", err)
	}

	// Verify SearchCellsFTS finds the indexed file content.
	results, err := mdb.SearchCellsFTS("authentication", "file", 10)
	if err != nil {
		t.Fatalf("SearchCellsFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results for 'authentication' in files, got 0")
	}

	for _, r := range results {
		if r.SourceType != "file" {
			t.Errorf("expected source_type='file', got %q", r.SourceType)
		}
		if r.SourceName != "auth.go" {
			t.Errorf("expected source_name='auth.go', got %q", r.SourceName)
		}
	}
}

// mockLLMForIndex is a mock LLM service that returns a fixed response.
type mockLLMForIndex struct {
	response string
}

func (m *mockLLMForIndex) Do(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	return &llm.Response{
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: m.response}},
	}, nil
}

func (m *mockLLMForIndex) TokenContextWindow() int { return 128000 }
func (m *mockLLMForIndex) MaxImageDimension() int  { return 0 }

func TestIndexConversationWithExtraction(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	messages := []MessageText{
		{Role: "user", Text: "How do I implement JWT authentication in Go?"},
		{Role: "assistant", Text: "You can use the golang-jwt library. Here is middleware for HTTP handlers that validates bearer tokens."},
		{Role: "user", Text: "What about storing sessions in Redis?"},
		{Role: "assistant", Text: "For session storage, use github.com/go-redis/redis with a TTL of 24 hours."},
	}

	// Build a valid JSON response that ExtractCells will parse.
	cells := []ExtractedCell{
		{
			CellType:  "fact",
			Salience:  0.9,
			Content:   "JWT authentication in Go uses the golang-jwt library",
			TopicHint: "authentication",
		},
		{
			CellType:  "decision",
			Salience:  0.8,
			Content:   "Session storage uses Redis with go-redis client and 24h TTL",
			TopicHint: "session management",
		},
	}
	cellsJSON, err := json.Marshal(cells)
	if err != nil {
		t.Fatal(err)
	}

	mockSvc := &mockLLMForIndex{response: string(cellsJSON)}
	ctx := context.Background()

	err = mdb.IndexConversation(ctx, "conv-v2-1", "Auth Chat", messages, nil, mockSvc)
	if err != nil {
		t.Fatalf("IndexConversation: %v", err)
	}

	// Verify cells are searchable via TwoTierSearch.
	results, err := mdb.TwoTierSearch("authentication", nil, "", 10)
	if err != nil {
		t.Fatalf("TwoTierSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected TwoTierSearch results for 'authentication', got 0")
	}

	// Verify topics were created.
	topics, err := mdb.AllTopics()
	if err != nil {
		t.Fatalf("AllTopics: %v", err)
	}
	if len(topics) == 0 {
		t.Fatal("expected topics to be created, got 0")
	}

	// Verify re-indexing with same content is a no-op (hash match).
	err = mdb.IndexConversation(ctx, "conv-v2-1", "Auth Chat", messages, nil, mockSvc)
	if err != nil {
		t.Fatalf("re-index IndexConversation: %v", err)
	}

	// Verify still searchable after no-op re-index.
	results2, err := mdb.TwoTierSearch("authentication", nil, "", 10)
	if err != nil {
		t.Fatalf("TwoTierSearch after re-index: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("expected TwoTierSearch results after re-index, got 0")
	}
}

func TestIndexConversationFallbackWithoutLLM(t *testing.T) {
	dir := t.TempDir()
	mdb, err := Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	messages := []MessageText{
		{Role: "user", Text: "Tell me about Kubernetes pod scheduling."},
		{Role: "assistant", Text: "Kubernetes uses a scheduler to assign pods to nodes based on resource requirements and constraints."},
	}

	ctx := context.Background()

	// Pass nil for svc — should fall back to chunk-based cell creation.
	err = mdb.IndexConversation(ctx, "conv-v2-fallback", "K8s Scheduling", messages, nil, nil)
	if err != nil {
		t.Fatalf("IndexConversation fallback: %v", err)
	}

	// Verify cells are searchable via TwoTierSearch.
	results, err := mdb.TwoTierSearch("Kubernetes", nil, "", 10)
	if err != nil {
		t.Fatalf("TwoTierSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected TwoTierSearch results for 'Kubernetes' with fallback indexing, got 0")
	}

	// Verify at least one cell exists with conversation source.
	found := false
	for _, r := range results {
		if r.SourceType == "conversation" && r.SourceID == "conv-v2-fallback" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a cell result with source_type='conversation' and source_id='conv-v2-fallback'")
	}
}
