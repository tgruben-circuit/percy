package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tgruben-circuit/percy/db"
	"github.com/tgruben-circuit/percy/llm"
)

func TestHandleExportMarkdown(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	ctx := context.Background()

	// Create conversation
	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add a user message
	userMsg := llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "Hello world"}},
	}
	userData := map[string]string{"text": "Hello world"}
	_, err = h.db.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ConversationID,
		Type:           db.MessageTypeUser,
		LLMData:        userMsg,
		UserData:       userData,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add an agent message
	agentMsg := llm.Message{
		Role:    llm.MessageRoleAssistant,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "Hi there!"}},
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
	req := httptest.NewRequest("GET", "/"+conv.ConversationID+"/export?format=markdown", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("expected markdown content type, got %s", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello world") {
		t.Fatalf("expected user message in export, got: %s", body)
	}
	if !strings.Contains(body, "Hi there!") {
		t.Fatalf("expected agent message in export, got: %s", body)
	}
	if !strings.Contains(body, "## User") {
		t.Fatalf("expected User header in export, got: %s", body)
	}
	if !strings.Contains(body, "## Assistant") {
		t.Fatalf("expected Assistant header in export, got: %s", body)
	}
}

func TestHandleExportJSON(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	ctx := context.Background()

	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	userData := map[string]string{"text": "test message"}
	_, err = h.db.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ConversationID,
		Type:           db.MessageTypeUser,
		UserData:       userData,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+conv.ConversationID+"/export?format=json", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected json content type, got %s", rec.Header().Get("Content-Type"))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["conversation_id"] != conv.ConversationID {
		t.Fatalf("wrong conversation_id: %v", result["conversation_id"])
	}
	msgs, ok := result["messages"].([]interface{})
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got: %v", result["messages"])
	}
}

func TestHandleExportDefaultFormat(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	ctx := context.Background()

	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Export without format parameter should default to markdown
	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+conv.ConversationID+"/export", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("expected markdown content type, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestHandleExportInvalidFormat(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	ctx := context.Background()

	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+conv.ConversationID+"/export?format=xml", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
