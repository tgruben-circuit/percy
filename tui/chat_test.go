package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type mockChatClient struct {
	sentMessage    string
	cancelled      bool
	newConvoID     string
	newConvoModel  string
	newConvoCwd    string
	newConvoMsg    string
	models         []ModelInfo
	modelsErr      error
}

func (m *mockChatClient) SendMessage(id, message string) error {
	m.sentMessage = message
	return nil
}

func (m *mockChatClient) CancelConversation(id string) error {
	m.cancelled = true
	return nil
}

func (m *mockChatClient) GetConversation(id string) (StreamResponse, error) {
	return StreamResponse{
		Conversation: Conversation{ConversationID: id, Cwd: "/home/user/project"},
		Messages: []APIMessage{
			{MessageID: "msg-1", SequenceID: 1, Type: "user"},
			{MessageID: "msg-2", SequenceID: 2, Type: "agent"},
		},
	}, nil
}

func (m *mockChatClient) NewConversation(message, model, cwd string) (string, error) {
	m.newConvoMsg = message
	m.newConvoModel = model
	m.newConvoCwd = cwd
	if m.newConvoID == "" {
		return "new-conv-1", nil
	}
	return m.newConvoID, nil
}

func (m *mockChatClient) ListModels() ([]ModelInfo, error) {
	if m.models != nil {
		return m.models, m.modelsErr
	}
	return []ModelInfo{
		{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", Ready: true},
		{ID: "claude-haiku-4", DisplayName: "Claude Haiku 4", Ready: true},
		{ID: "broken-model", DisplayName: "Broken", Ready: false},
	}, m.modelsErr
}

func TestChatModelInit(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil init command")
	}
}

func TestChatModelReceiveMessages(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24

	// Simulate receiving initial conversation data
	updated, _ := m.Update(chatHistoryMsg{
		response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Messages: []APIMessage{
				{MessageID: "msg-1", SequenceID: 1, Type: "user"},
				{MessageID: "msg-2", SequenceID: 2, Type: "agent"},
			},
		},
	})
	cm := updated.(ChatModel)
	if len(cm.messages) != 2 {
		t.Fatalf("got %d messages", len(cm.messages))
	}
}

func TestChatModelIdempotentMerge(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24

	// First batch
	updated, _ := m.Update(chatHistoryMsg{
		response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Messages: []APIMessage{
				{MessageID: "msg-1", SequenceID: 1, Type: "user"},
				{MessageID: "msg-2", SequenceID: 2, Type: "agent"},
			},
		},
	})
	cm := updated.(ChatModel)

	// Second batch with overlap
	updated2, _ := cm.Update(sseEventMsg{event: StreamEvent{
		Response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Messages: []APIMessage{
				{MessageID: "msg-2", SequenceID: 2, Type: "agent"},
				{MessageID: "msg-3", SequenceID: 3, Type: "tool"},
			},
		},
	}})
	cm = updated2.(ChatModel)

	if len(cm.messages) != 3 {
		t.Fatalf("got %d messages, want 3 (idempotent merge)", len(cm.messages))
	}
}

func TestChatModelSSEUpdate(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24

	// Receive SSE event
	m2, _ := m.Update(sseEventMsg{event: StreamEvent{
		Response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Messages: []APIMessage{
				{MessageID: "msg-1", SequenceID: 1, Type: "agent"},
			},
			ConversationState: &ConversationState{Working: true, Model: "claude-sonnet-4-20250514"},
			ContextWindowSize: 50000,
		},
	}})
	cm := m2.(ChatModel)
	if !cm.working {
		t.Error("expected working=true")
	}
	if cm.model != "claude-sonnet-4-20250514" {
		t.Errorf("got model %q", cm.model)
	}
	if cm.contextWindowSize != 50000 {
		t.Errorf("got context window %d", cm.contextWindowSize)
	}
}

func TestChatModelView(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestChatModelSendMessage(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "conv-1")
	m.width = 80
	m.height = 24

	// Type a message
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	cm := m2.(ChatModel)
	_ = cm

	// Verify the input field is active (just check it doesn't panic)
}

func TestChatModelBack(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24

	// Press escape to go back
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc press")
	}
	msg := cmd()
	if _, ok := msg.(BackToListMsg); !ok {
		t.Fatalf("expected BackToListMsg, got %T", msg)
	}
}

func TestChatModelBackAfterDisconnect(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "conv-1")
	m.width = 80
	m.height = 24
	m.serverURL = "http://localhost:9999"

	// Simulate history load → SSE start
	updated, _ := m.Update(chatHistoryMsg{
		response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Messages:     []APIMessage{{MessageID: "msg-1", SequenceID: 1, Type: "user"}},
		},
	})
	cm := updated.(ChatModel)

	// Simulate SSE error (disconnect)
	updated2, _ := cm.Update(sseEventMsg{event: StreamEvent{Err: fmt.Errorf("connection lost")}})
	cm = updated2.(ChatModel)
	if cm.connected {
		t.Error("expected disconnected")
	}

	// Press ESC — must not hang.
	done := make(chan struct{})
	go func() {
		_, cmd := cm.Update(tea.KeyMsg{Type: tea.KeyEscape})
		if cmd == nil {
			t.Error("expected command from esc press")
		} else {
			msg := cmd()
			if _, ok := msg.(BackToListMsg); !ok {
				t.Errorf("expected BackToListMsg, got %T", msg)
			}
		}
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("ESC handler blocked — UI would be frozen")
	}
}

func TestChatModelCancel(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "conv-1")
	m.width = 80
	m.height = 24
	m.working = true

	// Press ctrl+c to cancel
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected command from ctrl+c")
	}

	// Execute the command
	msg := cmd()
	if _, ok := msg.(chatActionMsg); !ok {
		t.Fatalf("expected chatActionMsg, got %T", msg)
	}

	// Wait for async cancel
	time.Sleep(10 * time.Millisecond)
	if !client.cancelled {
		t.Error("expected cancel to be called")
	}
}

func TestChatModelNewConvoInit(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "")
	m.newConvo = true
	m.width = 80
	m.height = 24

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil init command")
	}

	// Init returns a tea.Batch; execute and find the modelsMsg
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", msg)
	}

	var found bool
	for _, c := range batch {
		if c == nil {
			continue
		}
		result := c()
		if mm, ok := result.(modelsMsg); ok {
			found = true
			if mm.err != nil {
				t.Fatalf("unexpected error: %v", mm.err)
			}
			if len(mm.models) == 0 {
				t.Fatal("expected models")
			}
		}
	}
	if !found {
		t.Fatal("expected modelsMsg in batch commands")
	}
}

func TestChatModelNewConvoModelCycle(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "")
	m.newConvo = true
	m.width = 80
	m.height = 24

	// Simulate receiving models (only ready ones should be kept)
	updated, _ := m.Update(modelsMsg{models: []ModelInfo{
		{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", Ready: true},
		{ID: "claude-haiku-4", DisplayName: "Claude Haiku 4", Ready: true},
		{ID: "broken-model", DisplayName: "Broken", Ready: false},
	}})
	cm := updated.(ChatModel)

	if len(cm.models) != 2 {
		t.Fatalf("expected 2 ready models, got %d", len(cm.models))
	}
	if cm.model != "claude-sonnet-4" {
		t.Errorf("expected default model claude-sonnet-4, got %q", cm.model)
	}

	// Tab should cycle to next model
	updated, _ = cm.Update(tea.KeyMsg{Type: tea.KeyTab})
	cm = updated.(ChatModel)
	if cm.model != "claude-haiku-4" {
		t.Errorf("expected claude-haiku-4 after tab, got %q", cm.model)
	}

	// Tab again should wrap around
	updated, _ = cm.Update(tea.KeyMsg{Type: tea.KeyTab})
	cm = updated.(ChatModel)
	if cm.model != "claude-sonnet-4" {
		t.Errorf("expected claude-sonnet-4 after wrap, got %q", cm.model)
	}
}

func TestChatModelNewConvoCreateOnEnter(t *testing.T) {
	client := &mockChatClient{newConvoID: "created-conv-1"}
	m := NewChatModel(client, "")
	m.newConvo = true
	m.model = "claude-sonnet-4"
	m.cwd = "/home/user/project"
	m.width = 80
	m.height = 24

	// Type a message
	for _, r := range "hello world" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	// Press Enter to create conversation
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm := updated.(ChatModel)
	_ = cm

	if cmd == nil {
		t.Fatal("expected command from enter")
	}

	msg := cmd()
	ncm, ok := msg.(newConvoCreatedMsg)
	if !ok {
		t.Fatalf("expected newConvoCreatedMsg, got %T", msg)
	}
	if ncm.err != nil {
		t.Fatalf("unexpected error: %v", ncm.err)
	}
	if ncm.conversationID != "created-conv-1" {
		t.Errorf("got id %q", ncm.conversationID)
	}

	// Verify client received correct params
	if client.newConvoMsg != "hello world" {
		t.Errorf("got message %q", client.newConvoMsg)
	}
	if client.newConvoModel != "claude-sonnet-4" {
		t.Errorf("got model %q", client.newConvoModel)
	}
	if client.newConvoCwd != "/home/user/project" {
		t.Errorf("got cwd %q", client.newConvoCwd)
	}
}

func TestChatModelNewConvoCreatedTransition(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "")
	m.newConvo = true
	m.width = 80
	m.height = 24

	// Receive newConvoCreatedMsg
	updated, cmd := m.Update(newConvoCreatedMsg{conversationID: "created-1"})
	cm := updated.(ChatModel)

	if cm.newConvo {
		t.Error("expected newConvo=false after creation")
	}
	if cm.conversationID != "created-1" {
		t.Errorf("got conversationID %q", cm.conversationID)
	}
	if cmd == nil {
		t.Fatal("expected command to fetch history")
	}
}

func TestChatModelNewConvoView(t *testing.T) {
	m := NewChatModel(&mockChatClient{}, "")
	m.newConvo = true
	m.model = "claude-sonnet-4"
	m.cwd = "/home/user/project"
	m.width = 80
	m.height = 24

	view := m.View()
	if !strings.Contains(view, "New conversation") {
		t.Errorf("expected 'New conversation' in view, got %q", view)
	}
	if !strings.Contains(view, "claude-sonnet-4") {
		t.Errorf("expected model name in view, got %q", view)
	}
	if !strings.Contains(view, "Tab") {
		t.Errorf("expected Tab hint in view, got %q", view)
	}
}

func TestChatModelCwdFromHistory(t *testing.T) {
	client := &mockChatClient{}
	m := NewChatModel(client, "conv-1")
	m.width = 80
	m.height = 24

	// Receive history with cwd
	updated, _ := m.Update(chatHistoryMsg{
		response: StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1", Cwd: "/home/user/project"},
			Messages:     []APIMessage{{MessageID: "msg-1", SequenceID: 1, Type: "user"}},
		},
	})
	cm := updated.(ChatModel)
	if cm.cwd != "/home/user/project" {
		t.Errorf("expected cwd from history, got %q", cm.cwd)
	}
}
