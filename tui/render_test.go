package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTextContent(t *testing.T) {
	content := LLMContent{Type: ContentTypeText, Text: "Hello world"}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "Hello world") {
		t.Errorf("expected 'Hello world' in output, got %q", result)
	}
}

func TestRenderThinkingContent(t *testing.T) {
	content := LLMContent{Type: ContentTypeThinking, Thinking: "Let me think about this carefully.\nSecond line of thought."}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "thinking") {
		t.Errorf("expected 'thinking' in output, got %q", result)
	}
}

func TestRenderRedactedThinking(t *testing.T) {
	content := LLMContent{Type: ContentTypeRedactedThinking}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "redacted") {
		t.Errorf("expected 'redacted' in output, got %q", result)
	}
}

func TestRenderToolUse(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "ls -la"})
	content := LLMContent{Type: ContentTypeToolUse, ToolName: "bash", ToolInput: input}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "bash") {
		t.Errorf("expected 'bash' in output, got %q", result)
	}
}

func TestRenderToolResult(t *testing.T) {
	content := LLMContent{
		Type:      ContentTypeToolResult,
		ToolUseID: "tool-1",
		ToolResult: []LLMContent{
			{Type: ContentTypeText, Text: "file1.go\nfile2.go"},
		},
	}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "file1.go") {
		t.Errorf("expected 'file1.go' in output, got %q", result)
	}
}

func TestRenderToolResultError(t *testing.T) {
	content := LLMContent{
		Type:      ContentTypeToolResult,
		ToolUseID: "tool-1",
		ToolError: true,
		ToolResult: []LLMContent{
			{Type: ContentTypeText, Text: "command not found"},
		},
	}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "command not found") {
		t.Errorf("expected error text in output, got %q", result)
	}
}

func TestRenderImageContent(t *testing.T) {
	content := LLMContent{Type: ContentTypeText, MediaType: "image/png", Data: "base64data"}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "Image") {
		t.Errorf("expected image placeholder in output, got %q", result)
	}
}

func TestRenderMessageUser(t *testing.T) {
	llmData := `{"Role":0,"Content":[{"Type":2,"Text":"What is Go?"}]}`
	msg := APIMessage{
		MessageID: "msg-1",
		Type:      "user",
		LlmData:   &llmData,
	}
	result := RenderMessage(msg, 80)
	if !strings.Contains(result, "What is Go?") {
		t.Errorf("expected user message in output, got %q", result)
	}
}

func TestRenderMessageAgent(t *testing.T) {
	llmData := `{"Role":1,"Content":[{"Type":2,"Text":"Go is a programming language."}]}`
	msg := APIMessage{
		MessageID: "msg-2",
		Type:      "agent",
		LlmData:   &llmData,
	}
	result := RenderMessage(msg, 80)
	if !strings.Contains(result, "Go is a programming language") {
		t.Errorf("expected agent message in output, got %q", result)
	}
}

func TestRenderMessageNoLlmData(t *testing.T) {
	msg := APIMessage{
		MessageID: "msg-3",
		Type:      "user",
		LlmData:   nil,
	}
	result := RenderMessage(msg, 80)
	// Should not panic, should produce some output
	if result == "" {
		t.Error("expected non-empty output for message without llm_data")
	}
}

func TestRenderToolUseTruncation(t *testing.T) {
	// Large input should be truncated
	longInput := make([]byte, 500)
	for i := range longInput {
		longInput[i] = 'x'
	}
	input, _ := json.Marshal(map[string]string{"content": string(longInput)})
	content := LLMContent{Type: ContentTypeToolUse, ToolName: "write_file", ToolInput: input}
	result := RenderContent(content, 80)
	if !strings.Contains(result, "write_file") {
		t.Errorf("expected tool name in output, got %q", result)
	}
	// Should be truncated — result should be shorter than the raw input
	if len(result) > 400 {
		t.Errorf("expected truncated output, got %d chars", len(result))
	}
}

func TestRenderMessageError(t *testing.T) {
	msg := APIMessage{
		MessageID: "msg-err",
		Type:      "error",
	}
	userData := `"Something went wrong"`
	msg.UserData = &userData
	result := RenderMessage(msg, 80)
	if result == "" {
		t.Error("expected non-empty output for error message")
	}
}
