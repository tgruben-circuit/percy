package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	Quit     key.Binding
	Back     key.Binding
	Select   key.Binding
	New      key.Binding
	Delete   key.Binding
	Archive  key.Binding
	Cancel   key.Binding
	Model    key.Binding
	Refresh  key.Binding
	Toggle   key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:     key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		New:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Archive:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
		Cancel:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel")),
		Model:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "model")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Toggle:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "toggle")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	}
}
