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

func TestHandleForkConversation(t *testing.T) {
	h := NewTestHarness(t)
	defer h.cleanup()

	ctx := context.Background()
	model := "predictable"
	conv, err := h.db.CreateConversation(ctx, nil, true, nil, &model)
	if err != nil {
		t.Fatal(err)
	}

	// Create 5 messages
	for i := 0; i < 5; i++ {
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

	// Fork at sequence 3
	body := ForkConversationRequest{
		SourceConversationID: conv.ConversationID,
		AtSequenceID:         3,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/conversations/fork", strings.NewReader(string(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.server.handleForkConversation(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	newConvID := resp["conversation_id"]
	if newConvID == "" {
		t.Fatal("expected conversation_id in response")
	}
	if newConvID == conv.ConversationID {
		t.Fatal("fork should create a different conversation")
	}

	// Verify the forked conversation has 3 messages
	msgs, err := h.db.ListMessages(ctx, newConvID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	for i, m := range msgs {
		if m.SequenceID != int64(i+1) {
			t.Fatalf("expected sequence_id %d, got %d", i+1, m.SequenceID)
		}
	}
}

func TestHandleForkConversation_BadRequest(t *testing.T) {
	h := NewTestHarness(t)
	defer h.cleanup()

	req := httptest.NewRequest("POST", "/api/conversations/fork", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.server.handleForkConversation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
