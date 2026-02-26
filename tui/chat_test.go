package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type mockChatClient struct {
	sentMessage string
	cancelled   bool
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
		Conversation: Conversation{ConversationID: id},
		Messages: []APIMessage{
			{MessageID: "msg-1", SequenceID: 1, Type: "user"},
			{MessageID: "msg-2", SequenceID: 2, Type: "agent"},
		},
	}, nil
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
