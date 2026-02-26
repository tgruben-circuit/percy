package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	viewList view = iota
	viewChat
)

// Model is the root Bubble Tea model that delegates to list/chat views.
type Model struct {
	client    *Client
	serverURL string
	list      ListModel
	chat      ChatModel
	current   view
	width     int
	height    int
}

// Run starts the TUI, connecting to the given Percy server URL.
func Run(serverURL string) error {
	client := NewClient(serverURL)
	m := Model{
		client:    client,
		serverURL: serverURL,
		list:      NewListModel(client),
		current:   viewList,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return m.list.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Forward to both views
		var cmd1, cmd2 tea.Cmd
		var updated tea.Model
		updated, cmd1 = m.list.Update(msg)
		m.list = updated.(ListModel)
		updated, cmd2 = m.chat.Update(msg)
		m.chat = updated.(ChatModel)
		return m, tea.Batch(cmd1, cmd2)

	case SelectConversationMsg:
		m.chat = NewChatModel(m.client, msg.ConversationID)
		m.chat.SetServerURL(m.serverURL)
		m.chat.width = m.width
		m.chat.height = m.height
		m.current = viewChat
		return m, m.chat.Init()

	case NewConversationMsg:
		m.current = viewChat
		m.chat = NewChatModel(m.client, "")
		m.chat.newConvo = true
		m.chat.cwd = msg.DefaultCwd
		m.chat.model = msg.DefaultModel
		m.chat.SetServerURL(m.serverURL)
		m.chat.width = m.width
		m.chat.height = m.height
		return m, m.chat.Init()

	case BackToListMsg:
		m.current = viewList
		return m, m.list.Init()
	}

	switch m.current {
	case viewList:
		var cmd tea.Cmd
		var updated tea.Model
		updated, cmd = m.list.Update(msg)
		m.list = updated.(ListModel)
		return m, cmd
	case viewChat:
		var cmd tea.Cmd
		var updated tea.Model
		updated, cmd = m.chat.Update(msg)
		m.chat = updated.(ChatModel)
		return m, cmd
	}
	return m, nil
}

func (m Model) View() string {
	switch m.current {
	case viewChat:
		return m.chat.View()
	default:
		return m.list.View()
	}
}

// Ensure Model satisfies tea.Model.
var _ tea.Model = Model{}
var _ fmt.Stringer = view(0)

func (v view) String() string {
	switch v {
	case viewList:
		return "list"
	case viewChat:
		return "chat"
	default:
		return "unknown"
	}
}
