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

func TestHandleEditMessage(t *testing.T) {
	h := NewTestHarness(t)
	defer h.cleanup()

	ctx := context.Background()
	model := "predictable"
	conv, err := h.db.CreateConversation(ctx, nil, true, nil, &model)
	if err != nil {
		t.Fatal(err)
	}

	// Create 4 messages: user, agent, user, agent
	for i := 0; i < 4; i++ {
		msgType := db.MessageTypeUser
		if i%2 == 1 {
			msgType = db.MessageTypeAgent
		}
		msg := llm.Message{
			Role:    llm.MessageRoleUser,
			Content: []llm.Content{{Type: llm.ContentTypeText, Text: "msg"}},
		}
		_, err := h.db.CreateMessage(ctx, db.CreateMessageParams{
			ConversationID: conv.ConversationID,
			Type:           msgType,
			LLMData:        msg,
			UserData:       map[string]string{"text": "msg"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verify we have 4 messages
	msgs, err := h.db.ListMessages(ctx, conv.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	// Edit at sequence 3 (the second user message)
	reqBody, _ := json.Marshal(EditMessageRequest{
		SequenceID: 3,
		Message:    "edited message",
	})

	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/edit", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// After edit: messages 3 and 4 should be deleted, and a new user message "edited message" should be added
	// The exact count depends on timing (agent may have responded), but there should be at least 3:
	// original msg 1, original msg 2, and the new edited message
	msgs, err = h.db.ListMessages(ctx, conv.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages after edit, got %d", len(msgs))
	}
	// First two messages should be unchanged
	if msgs[0].SequenceID != 1 || msgs[1].SequenceID != 2 {
		t.Fatalf("first two messages should be unchanged")
	}
}

func TestHandleEditMessage_BadRequest(t *testing.T) {
	h := NewTestHarness(t)
	defer h.cleanup()

	ctx := context.Background()
	conv, err := h.db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := h.server.conversationMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/edit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
