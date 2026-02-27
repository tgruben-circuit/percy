package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tgruben-circuit/percy/db"
	"github.com/tgruben-circuit/percy/llm"
)

func TestExtractTouchedFiles(t *testing.T) {
	h := NewTestHarness(t)
	defer h.cleanup()

	ctx := context.Background()
	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Agent message with patch tool use
	agentMsg := llm.Message{
		Role: llm.MessageRoleAssistant,
		Content: []llm.Content{
			{
				Type:      llm.ContentTypeToolUse,
				ToolName:  "patch",
				ToolInput: json.RawMessage(`{"path": "server/handlers.go", "patches": []}`)},
			{
				Type:      llm.ContentTypeToolUse,
				ToolName:  "read_file",
				ToolInput: json.RawMessage(`{"path": "server/server.go"}`),
			},
			{
				Type:      llm.ContentTypeToolUse,
				ToolName:  "read_file",
				ToolInput: json.RawMessage(`{"path": "server/server.go"}`),
			},
		},
	}
	_, err = h.db.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ConversationID,
		Type:           db.MessageTypeAgent,
		LLMData:        agentMsg,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+conv.ConversationID+"/files", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var files []TouchedFile
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Should be sorted alphabetically
	if files[0].Path != "server/handlers.go" {
		t.Fatalf("expected server/handlers.go, got %s", files[0].Path)
	}
	if files[0].Operation != "patch" {
		t.Fatalf("expected patch operation, got %s", files[0].Operation)
	}
	if files[1].Path != "server/server.go" {
		t.Fatalf("expected server/server.go, got %s", files[1].Path)
	}
	if files[1].Count != 2 {
		t.Fatalf("expected count 2, got %d", files[1].Count)
	}
}
