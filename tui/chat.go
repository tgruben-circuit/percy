package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ChatClient is the interface the chat view needs from the HTTP client.
type ChatClient interface {
	SendMessage(conversationID, message string) error
	CancelConversation(id string) error
	GetConversation(id string) (StreamResponse, error)
	NewConversation(message, model, cwd string) (string, error)
	ListModels() ([]ModelInfo, error)
}

// Messages produced by the chat view.
type (
	chatHistoryMsg struct {
		response StreamResponse
		err      error
	}
	sseEventMsg       struct{ event StreamEvent }
	chatActionMsg     struct{ err error }
	BackToListMsg     struct{}
	modelsMsg         struct {
		models []ModelInfo
		err    error
	}
	newConvoCreatedMsg struct {
		conversationID string
		err            error
	}
)

// ChatModel is the Bubble Tea model for the chat view.
type ChatModel struct {
	client           ChatClient
	conversationID   string
	serverURL        string
	messages         []APIMessage
	messageIndex     map[string]int // messageID -> index in messages
	working          bool
	model            string
	contextWindowSize uint64
	width, height    int
	viewport         viewport.Model
	input            textinput.Model
	keys             KeyMap
	connected        bool
	sseEvents        chan StreamEvent
	sseDone          chan struct{}
	sseStream        *SSEStream
	err              error
	newConvo         bool
	cwd              string
	models           []ModelInfo
	modelIndex       int
}

// NewChatModel creates a new chat view for a conversation.
func NewChatModel(client ChatClient, conversationID string) ChatModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 80

	vp := viewport.New(80, 20)

	return ChatModel{
		client:         client,
		conversationID: conversationID,
		messageIndex:   make(map[string]int),
		input:          ti,
		viewport:       vp,
		keys:           DefaultKeyMap(),
		sseEvents:      make(chan StreamEvent, 64),
		sseDone:        make(chan struct{}),
	}
}

func (m ChatModel) Init() tea.Cmd {
	if m.newConvo {
		return tea.Batch(
			m.fetchModels,
			textinput.Blink,
		)
	}
	return tea.Batch(
		m.fetchHistory,
		textinput.Blink,
	)
}

func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case modelsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Filter to ready models only
		var ready []ModelInfo
		for _, mi := range msg.models {
			if mi.Ready {
				ready = append(ready, mi)
			}
		}
		m.models = ready
		if len(ready) > 0 {
			m.model = ready[0].ID
			m.modelIndex = 0
		}
		return m, nil

	case newConvoCreatedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.conversationID = msg.conversationID
		m.newConvo = false
		return m, m.fetchHistory

	case chatHistoryMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.mergeMessages(msg.response.Messages)
		if msg.response.ConversationState != nil {
			m.working = msg.response.ConversationState.Working
			m.model = msg.response.ConversationState.Model
		}
		if msg.response.Conversation.Cwd != "" {
			m.cwd = msg.response.Conversation.Cwd
		}
		m.contextWindowSize = msg.response.ContextWindowSize
		m.connected = true
		m.updateViewport()
		m.closeSSE()
		m.sseDone = make(chan struct{})
		m.sseStream = NewSSEStream(
			fmt.Sprintf("%s/api/conversation/%s/stream", m.serverURL, m.conversationID),
			m.sseEvents,
		)
		go m.sseStream.Connect()
		return m, m.waitForSSE

	case sseEventMsg:
		if msg.event.Err != nil {
			m.connected = false
			m.err = msg.event.Err
			return m, nil
		}
		resp := msg.event.Response
		if !resp.Heartbeat {
			m.mergeMessages(resp.Messages)
		}
		if resp.ConversationState != nil {
			m.working = resp.ConversationState.Working
			m.model = resp.ConversationState.Model
		}
		if resp.ContextWindowSize > 0 {
			m.contextWindowSize = resp.ContextWindowSize
		}
		m.updateViewport()
		return m, m.waitForSSE

	case chatActionMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
		headerHeight := 1   // title
		inputHeight := 3    // input + borders
		statusHeight := 1   // status bar
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight - inputHeight - statusHeight
		m.updateViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.viewport, cmd = m.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.closeSSE()
		return *m, func() tea.Msg { return BackToListMsg{} }

	case key.Matches(msg, m.keys.Cancel):
		if m.working {
			client := m.client
			id := m.conversationID
			return *m, func() tea.Msg { return chatActionMsg{err: client.CancelConversation(id)} }
		}
		m.closeSSE()
		return *m, tea.Quit

	case msg.Type == tea.KeyTab && m.newConvo && len(m.models) > 0:
		m.modelIndex = (m.modelIndex + 1) % len(m.models)
		m.model = m.models[m.modelIndex].ID
		return *m, nil

	case msg.Type == tea.KeyEnter:
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return *m, nil
		}
		m.input.Reset()
		if m.newConvo {
			client := m.client
			model := m.model
			cwd := m.cwd
			return *m, func() tea.Msg {
				id, err := client.NewConversation(text, model, cwd)
				return newConvoCreatedMsg{conversationID: id, err: err}
			}
		}
		client := m.client
		id := m.conversationID
		return *m, func() tea.Msg { return chatActionMsg{err: client.SendMessage(id, text)} }
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return *m, cmd
}

func (m ChatModel) View() string {
	if m.err != nil && len(m.messages) == 0 && !m.newConvo {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	if m.newConvo {
		title := agentStyle.Render("New conversation")
		modelLine := ""
		if m.model != "" {
			modelLine = toolStyle.Render("Model: "+m.model) + "  [Tab to cycle]"
		}
		cwdLine := ""
		if m.cwd != "" {
			cwdLine = toolStyle.Render("Dir: " + m.cwd)
		}
		var parts []string
		parts = append(parts, title)
		if modelLine != "" {
			parts = append(parts, modelLine)
		}
		if cwdLine != "" {
			parts = append(parts, cwdLine)
		}
		parts = append(parts, "", m.input.View())
		return strings.Join(parts, "\n")
	}

	title := agentStyle.Render("Percy")
	if m.model != "" {
		title += " " + toolStyle.Render("("+m.model+")")
	}

	status := StatusBar{
		Connected:         m.connected,
		Working:           m.working,
		Model:             m.model,
		ContextWindowSize: m.contextWindowSize,
		Width:             m.width,
		Cwd:               m.cwd,
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		title,
		m.viewport.View(),
		m.input.View(),
		status.View(),
	)
}

func (m *ChatModel) mergeMessages(msgs []APIMessage) {
	for _, msg := range msgs {
		if idx, ok := m.messageIndex[msg.MessageID]; ok {
			m.messages[idx] = msg
		} else {
			m.messageIndex[msg.MessageID] = len(m.messages)
			m.messages = append(m.messages, msg)
		}
	}
}

func (m *ChatModel) updateViewport() {
	var parts []string
	for _, msg := range m.messages {
		rendered := RenderMessage(msg, m.width)
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}
	content := strings.Join(parts, "\n")
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m ChatModel) fetchHistory() tea.Msg {
	resp, err := m.client.GetConversation(m.conversationID)
	return chatHistoryMsg{response: resp, err: err}
}

func (m ChatModel) waitForSSE() tea.Msg {
	select {
	case event := <-m.sseEvents:
		return sseEventMsg{event: event}
	case <-m.sseDone:
		return sseEventMsg{event: StreamEvent{Err: errStreamClosed}}
	}
}

var errStreamClosed = fmt.Errorf("stream closed")

func (m *ChatModel) closeSSE() {
	select {
	case <-m.sseDone:
	default:
		close(m.sseDone)
	}
	if m.sseStream != nil {
		m.sseStream.Close()
		m.sseStream = nil
	}
}

func (m ChatModel) fetchModels() tea.Msg {
	models, err := m.client.ListModels()
	return modelsMsg{models: models, err: err}
}

// SetServerURL stores the server base URL for SSE stream construction.
func (m *ChatModel) SetServerURL(url string) {
	m.serverURL = url
}
