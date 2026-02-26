package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	userStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // blue
	agentStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))  // green
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))   // red
	thinkStyle   = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("8")) // dim
	toolBold     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))  // magenta
	imagePlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
)

// RenderMessage renders a full APIMessage for the TUI.
func RenderMessage(msg APIMessage, width int) string {
	var header string
	switch msg.Type {
	case "user":
		header = userStyle.Render("You")
	case "agent":
		header = agentStyle.Render("Percy")
	case "tool":
		header = toolStyle.Render("Tool")
	case "error":
		header = errorStyle.Render("Error")
		if msg.UserData != nil {
			return header + "\n" + errorStyle.Render(*msg.UserData) + "\n"
		}
		return header + "\n"
	default:
		header = msg.Type
	}

	if msg.LlmData == nil {
		return header + "\n"
	}

	llmMsg, err := ParseLLMData(*msg.LlmData)
	if err != nil {
		return header + "\n" + fmt.Sprintf("(parse error: %v)\n", err)
	}

	var parts []string
	for _, c := range llmMsg.Content {
		rendered := RenderContent(c, width)
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}

	if len(parts) == 0 {
		return header + "\n"
	}

	return header + "\n" + strings.Join(parts, "\n")
}

// RenderContent renders a single LLMContent block for the TUI.
func RenderContent(c LLMContent, width int) string {
	// Image detection (text content with media data)
	if c.MediaType != "" && c.Data != "" {
		return imagePlStyle.Render(fmt.Sprintf("[Image: %s]", c.MediaType))
	}

	switch c.Type {
	case ContentTypeText:
		return renderMarkdown(c.Text, width)

	case ContentTypeThinking:
		firstLine := c.Thinking
		if idx := strings.Index(firstLine, "\n"); idx >= 0 {
			firstLine = firstLine[:idx]
		}
		if len(firstLine) > 80 {
			firstLine = firstLine[:77] + "..."
		}
		return thinkStyle.Render(fmt.Sprintf("[thinking] %s", firstLine))

	case ContentTypeRedactedThinking:
		return thinkStyle.Render("[redacted thinking]")

	case ContentTypeToolUse:
		name := toolBold.Render(c.ToolName)
		input := truncateJSON(c.ToolInput, 200)
		return fmt.Sprintf("%s %s", name, toolStyle.Render(input))

	case ContentTypeToolResult:
		return renderToolResult(c)

	default:
		return fmt.Sprintf("[content type %d]", c.Type)
	}
}

func renderToolResult(c LLMContent) string {
	var texts []string
	for _, rc := range c.ToolResult {
		if rc.Text != "" {
			texts = append(texts, rc.Text)
		}
	}
	output := strings.Join(texts, "\n")
	if len(output) > 500 {
		output = output[:497] + "..."
	}

	if c.ToolError {
		return errorStyle.Render(output)
	}
	return toolStyle.Render(output)
}

func renderMarkdown(text string, width int) string {
	if width < 20 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}

func truncateJSON(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	s := string(raw)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
