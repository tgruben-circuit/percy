package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestListModelInit(t *testing.T) {
	m := NewListModel(&mockClient{
		conversations: []ConversationWithState{
			{Conversation: Conversation{ConversationID: "c1", Slug: "first", UpdatedAt: time.Now()}, Working: false},
			{Conversation: Conversation{ConversationID: "c2", Slug: "second", UpdatedAt: time.Now()}, Working: true},
		},
	})

	// Init should return a command that fetches conversations
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil init command")
	}

	// Execute the command to get the message
	msg := cmd()
	fetchMsg, ok := msg.(conversationsMsg)
	if !ok {
		t.Fatalf("expected conversationsMsg, got %T", msg)
	}
	if len(fetchMsg.conversations) != 2 {
		t.Fatalf("got %d conversations", len(fetchMsg.conversations))
	}
}

func TestListModelUpdate(t *testing.T) {
	m := NewListModel(&mockClient{})

	// Simulate receiving conversations
	convos := []ConversationWithState{
		{Conversation: Conversation{ConversationID: "c1", Slug: "first"}, Working: false},
		{Conversation: Conversation{ConversationID: "c2", Slug: "second"}, Working: true},
	}

	updated, _ := m.Update(conversationsMsg{conversations: convos})
	lm := updated.(ListModel)
	if len(lm.conversations) != 2 {
		t.Fatalf("got %d conversations", len(lm.conversations))
	}
}

func TestListModelNavigation(t *testing.T) {
	m := NewListModel(&mockClient{})
	convos := []ConversationWithState{
		{Conversation: Conversation{ConversationID: "c1", Slug: "first"}},
		{Conversation: Conversation{ConversationID: "c2", Slug: "second"}},
		{Conversation: Conversation{ConversationID: "c3", Slug: "third"}},
	}
	updated, _ := m.Update(conversationsMsg{conversations: convos})
	m = updated.(ListModel)

	// Start at index 0
	if m.cursor != 0 {
		t.Errorf("expected cursor=0, got %d", m.cursor)
	}

	// Move down
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(ListModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", m.cursor)
	}

	// Move down again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(ListModel)
	if m.cursor != 2 {
		t.Errorf("expected cursor=2, got %d", m.cursor)
	}

	// Move down at end — should stay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(ListModel)
	if m.cursor != 2 {
		t.Errorf("expected cursor=2, got %d", m.cursor)
	}

	// Move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(ListModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", m.cursor)
	}
}

func TestListModelSelectConversation(t *testing.T) {
	m := NewListModel(&mockClient{})
	convos := []ConversationWithState{
		{Conversation: Conversation{ConversationID: "c1", Slug: "first"}},
	}
	updated, _ := m.Update(conversationsMsg{conversations: convos})
	m = updated.(ListModel)

	// Press enter to select
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated.(ListModel)

	if cmd == nil {
		t.Fatal("expected command from enter press")
	}

	msg := cmd()
	sel, ok := msg.(SelectConversationMsg)
	if !ok {
		t.Fatalf("expected SelectConversationMsg, got %T", msg)
	}
	if sel.ConversationID != "c1" {
		t.Errorf("got %q", sel.ConversationID)
	}
}

func TestListModelView(t *testing.T) {
	m := NewListModel(&mockClient{})
	convos := []ConversationWithState{
		{Conversation: Conversation{ConversationID: "c1", Slug: "first"}},
		{Conversation: Conversation{ConversationID: "c2", Slug: "second"}, Working: true},
	}
	updated, _ := m.Update(conversationsMsg{conversations: convos})
	m = updated.(ListModel)
	m.width = 80
	m.height = 24

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestListModelNewConversation(t *testing.T) {
	m := NewListModel(&mockClient{})
	convos := []ConversationWithState{
		{Conversation: Conversation{ConversationID: "c1", Slug: "first"}},
	}
	updated, _ := m.Update(conversationsMsg{conversations: convos})
	m = updated.(ListModel)

	// Press 'n' for new conversation
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected command from 'n' press")
	}
	msg := cmd()
	if _, ok := msg.(NewConversationMsg); !ok {
		t.Fatalf("expected NewConversationMsg, got %T", msg)
	}
}

// mockClient implements the subset of Client methods needed by the list view.
type mockClient struct {
	conversations []ConversationWithState
	deleteErr     error
	archiveErr    error
}

func (m *mockClient) ListConversations() ([]ConversationWithState, error) {
	return m.conversations, nil
}

func (m *mockClient) DeleteConversation(id string) error {
	return m.deleteErr
}

func (m *mockClient) ArchiveConversation(id string) error {
	return m.archiveErr
}

func (m *mockClient) NewConversation(message, model string) (string, error) {
	return "new-conv", nil
}
