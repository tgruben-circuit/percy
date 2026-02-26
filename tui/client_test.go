package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListConversations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversations" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]ConversationWithState{
			{Conversation: Conversation{ConversationID: "c1", Slug: "first"}, Working: true},
			{Conversation: Conversation{ConversationID: "c2", Slug: "second"}, Working: false},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	convos, err := c.ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(convos) != 2 {
		t.Fatalf("got %d conversations", len(convos))
	}
	if convos[0].ConversationID != "c1" {
		t.Errorf("got %q", convos[0].ConversationID)
	}
	if !convos[0].Working {
		t.Error("expected first to be working")
	}
}

func TestNewConversation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversations/new" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Message != "hello" {
			t.Errorf("got message %q", req.Message)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"status":          "accepted",
			"conversation_id": "new-conv-1",
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	id, err := c.NewConversation("hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "new-conv-1" {
		t.Errorf("got id %q", id)
	}
}

func TestSendMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversation/conv-1/chat" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Message != "test message" {
			t.Errorf("got message %q", req.Message)
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	err := c.SendMessage("conv-1", "test message")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCancelConversation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversation/conv-1/cancel" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	err := c.CancelConversation("conv-1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestArchiveConversation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversation/conv-1/archive" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Conversation{ConversationID: "conv-1", Archived: true})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	err := c.ArchiveConversation("conv-1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteConversation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversation/conv-1/delete" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	err := c.DeleteConversation("conv-1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestListModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]ModelInfo{
			{ID: "claude-sonnet-4-20250514", DisplayName: "Claude Sonnet 4", Ready: true, MaxContextTokens: 200000},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	models, err := c.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models", len(models))
	}
	if models[0].ID != "claude-sonnet-4-20250514" {
		t.Errorf("got %q", models[0].ID)
	}
}

func TestGetConversation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/conversation/conv-1" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(StreamResponse{
			Messages:     []APIMessage{{MessageID: "msg-1", Type: "user"}},
			Conversation: Conversation{ConversationID: "conv-1"},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	sr, err := c.GetConversation("conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Messages) != 1 {
		t.Fatalf("got %d messages", len(sr.Messages))
	}
	if sr.Conversation.ConversationID != "conv-1" {
		t.Errorf("got %q", sr.Conversation.ConversationID)
	}
}

func TestClientServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "internal error")
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	_, err := c.ListConversations()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
