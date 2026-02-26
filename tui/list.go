package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ListClient is the interface the list view needs from the HTTP client.
type ListClient interface {
	ListConversations() ([]ConversationWithState, error)
	DeleteConversation(id string) error
	ArchiveConversation(id string) error
}

// Messages produced by the list view.
type (
	conversationsMsg struct {
		conversations []ConversationWithState
		err           error
	}
	SelectConversationMsg struct{ ConversationID string }
	NewConversationMsg    struct{}
	listActionDoneMsg     struct{ err error }
)

// ListModel is the Bubble Tea model for the conversation list view.
type ListModel struct {
	client        ListClient
	conversations []ConversationWithState
	cursor        int
	width, height int
	keys          KeyMap
	loading       bool
	err           error
}

// NewListModel creates a new list view model.
func NewListModel(client ListClient) ListModel {
	return ListModel{
		client:  client,
		keys:    DefaultKeyMap(),
		loading: true,
	}
}

func (m ListModel) Init() tea.Cmd {
	return m.fetchConversations
}

func (m ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case conversationsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.conversations = msg.conversations
		m.err = nil
		if m.cursor >= len(m.conversations) {
			m.cursor = max(0, len(m.conversations)-1)
		}
		return m, nil

	case listActionDoneMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, m.fetchConversations

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m ListModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.conversations)-1 {
			m.cursor++
		}
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, m.keys.Select):
		if len(m.conversations) > 0 {
			id := m.conversations[m.cursor].ConversationID
			return m, func() tea.Msg { return SelectConversationMsg{ConversationID: id} }
		}
	case key.Matches(msg, m.keys.New):
		return m, func() tea.Msg { return NewConversationMsg{} }
	case key.Matches(msg, m.keys.Delete):
		if len(m.conversations) > 0 {
			id := m.conversations[m.cursor].ConversationID
			client := m.client
			return m, func() tea.Msg { return listActionDoneMsg{err: client.DeleteConversation(id)} }
		}
	case key.Matches(msg, m.keys.Archive):
		if len(m.conversations) > 0 {
			id := m.conversations[m.cursor].ConversationID
			client := m.client
			return m, func() tea.Msg { return listActionDoneMsg{err: client.ArchiveConversation(id)} }
		}
	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		return m, m.fetchConversations
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

func (m ListModel) View() string {
	if m.loading && len(m.conversations) == 0 {
		return "Loading conversations..."
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	if len(m.conversations) == 0 {
		return "No conversations. Press 'n' to create one."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("Conversations"))
	b.WriteString("\n\n")

	for i, c := range m.conversations {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		name := c.Slug
		if name == "" {
			name = c.ConversationID[:min(8, len(c.ConversationID))]
		}

		var indicator string
		if c.Working {
			indicator = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("●")
		}

		age := formatAge(c.UpdatedAt)

		line := fmt.Sprintf("%s%s%s %s", cursor, name, indicator, dimStyle.Render(age))
		if i == m.cursor {
			line = selectedStyle.Render(cursor+name) + indicator + " " + dimStyle.Render(age)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("j/k: navigate | enter: open | n: new | d: delete | a: archive | r: refresh | q: quit"))

	return b.String()
}

func (m ListModel) fetchConversations() tea.Msg {
	convos, err := m.client.ListConversations()
	return conversationsMsg{conversations: convos, err: err}
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
