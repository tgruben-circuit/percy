package tui

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConversationJSONRoundTrip(t *testing.T) {
	raw := `{
		"conversation_id": "conv-123",
		"slug": "test-convo",
		"user_initiated": true,
		"created_at": "2025-01-01T00:00:00Z",
		"updated_at": "2025-01-01T01:00:00Z",
		"cwd": "/home/user",
		"archived": false,
		"parent_conversation_id": null,
		"model": "claude-sonnet-4-20250514"
	}`

	var c Conversation
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}

	if c.ConversationID != "conv-123" {
		t.Errorf("got conversation_id %q", c.ConversationID)
	}
	if c.Slug != "test-convo" {
		t.Errorf("got slug %q", c.Slug)
	}
	if !c.UserInitiated {
		t.Error("expected user_initiated=true")
	}
	if c.Cwd != "/home/user" {
		t.Errorf("got cwd %q", c.Cwd)
	}
	if c.Model != "claude-sonnet-4-20250514" {
		t.Errorf("got model %q", c.Model)
	}
	if c.Archived {
		t.Error("expected archived=false")
	}

	// Round-trip
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var c2 Conversation
	if err := json.Unmarshal(data, &c2); err != nil {
		t.Fatal(err)
	}
	if c2.ConversationID != c.ConversationID {
		t.Error("round-trip mismatch")
	}
}

func TestConversationNullFields(t *testing.T) {
	raw := `{
		"conversation_id": "conv-456",
		"slug": null,
		"user_initiated": false,
		"created_at": "2025-01-01T00:00:00Z",
		"updated_at": "2025-01-01T00:00:00Z",
		"cwd": null,
		"archived": true,
		"parent_conversation_id": "parent-1",
		"model": null
	}`

	var c Conversation
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.Slug != "" {
		t.Errorf("expected empty slug, got %q", c.Slug)
	}
	if c.ParentConversationID != "parent-1" {
		t.Errorf("got parent %q", c.ParentConversationID)
	}
}

func TestConversationWithStateJSON(t *testing.T) {
	raw := `{
		"conversation_id": "conv-789",
		"slug": "test",
		"user_initiated": true,
		"created_at": "2025-01-01T00:00:00Z",
		"updated_at": "2025-01-01T00:00:00Z",
		"archived": false,
		"working": true
	}`

	var cs ConversationWithState
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		t.Fatal(err)
	}
	if cs.ConversationID != "conv-789" {
		t.Errorf("got %q", cs.ConversationID)
	}
	if !cs.Working {
		t.Error("expected working=true")
	}
}

func TestAPIMessageJSON(t *testing.T) {
	raw := `{
		"message_id": "msg-1",
		"conversation_id": "conv-1",
		"sequence_id": 42,
		"type": "agent",
		"llm_data": "{\"Role\":1,\"Content\":[{\"Type\":2,\"Text\":\"hello\"}]}",
		"user_data": null,
		"usage_data": "{\"input_tokens\":10}",
		"created_at": "2025-01-01T00:00:00Z",
		"display_data": null,
		"end_of_turn": true
	}`

	var msg APIMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MessageID != "msg-1" {
		t.Errorf("got message_id %q", msg.MessageID)
	}
	if msg.SequenceID != 42 {
		t.Errorf("got sequence_id %d", msg.SequenceID)
	}
	if msg.Type != "agent" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.LlmData == nil {
		t.Fatal("expected non-nil llm_data")
	}
	if msg.EndOfTurn == nil || !*msg.EndOfTurn {
		t.Error("expected end_of_turn=true")
	}
}

func TestLLMMessageJSON(t *testing.T) {
	raw := `{
		"Role": 1,
		"Content": [
			{"Type": 2, "Text": "Hello world"},
			{"Type": 3, "Thinking": "Let me think..."},
			{"Type": 5, "ToolName": "bash", "ToolInput": {"command": "ls"}}
		],
		"EndOfTurn": true
	}`

	var msg LLMMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Role != 1 {
		t.Errorf("got role %d", msg.Role)
	}
	if len(msg.Content) != 3 {
		t.Fatalf("got %d contents", len(msg.Content))
	}

	// Text content
	if msg.Content[0].Type != ContentTypeText {
		t.Errorf("expected type %d, got %d", ContentTypeText, msg.Content[0].Type)
	}
	if msg.Content[0].Text != "Hello world" {
		t.Errorf("got text %q", msg.Content[0].Text)
	}

	// Thinking content
	if msg.Content[1].Type != ContentTypeThinking {
		t.Errorf("expected type %d, got %d", ContentTypeThinking, msg.Content[1].Type)
	}
	if msg.Content[1].Thinking != "Let me think..." {
		t.Errorf("got thinking %q", msg.Content[1].Thinking)
	}

	// Tool use content
	if msg.Content[2].Type != ContentTypeToolUse {
		t.Errorf("expected type %d, got %d", ContentTypeToolUse, msg.Content[2].Type)
	}
	if msg.Content[2].ToolName != "bash" {
		t.Errorf("got tool name %q", msg.Content[2].ToolName)
	}
}

func TestContentTypeConstants(t *testing.T) {
	// These must match the server's iota values from llm/llm.go
	if ContentTypeText != 2 {
		t.Errorf("ContentTypeText=%d, want 2", ContentTypeText)
	}
	if ContentTypeThinking != 3 {
		t.Errorf("ContentTypeThinking=%d, want 3", ContentTypeThinking)
	}
	if ContentTypeRedactedThinking != 4 {
		t.Errorf("ContentTypeRedactedThinking=%d, want 4", ContentTypeRedactedThinking)
	}
	if ContentTypeToolUse != 5 {
		t.Errorf("ContentTypeToolUse=%d, want 5", ContentTypeToolUse)
	}
	if ContentTypeToolResult != 6 {
		t.Errorf("ContentTypeToolResult=%d, want 6", ContentTypeToolResult)
	}
}

func TestStreamResponseJSON(t *testing.T) {
	raw := `{
		"messages": [
			{
				"message_id": "msg-1",
				"conversation_id": "conv-1",
				"sequence_id": 1,
				"type": "user",
				"created_at": "2025-01-01T00:00:00Z"
			}
		],
		"conversation": {
			"conversation_id": "conv-1",
			"user_initiated": true,
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		},
		"conversation_state": {
			"conversation_id": "conv-1",
			"working": true,
			"model": "claude-sonnet-4-20250514"
		},
		"context_window_size": 50000
	}`

	var sr StreamResponse
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		t.Fatal(err)
	}
	if len(sr.Messages) != 1 {
		t.Fatalf("got %d messages", len(sr.Messages))
	}
	if sr.Conversation.ConversationID != "conv-1" {
		t.Errorf("got conversation_id %q", sr.Conversation.ConversationID)
	}
	if sr.ConversationState == nil {
		t.Fatal("expected non-nil conversation_state")
	}
	if !sr.ConversationState.Working {
		t.Error("expected working=true")
	}
	if sr.ContextWindowSize != 50000 {
		t.Errorf("got context_window_size %d", sr.ContextWindowSize)
	}
}

func TestStreamResponseHeartbeat(t *testing.T) {
	raw := `{
		"messages": [],
		"conversation": {
			"conversation_id": "conv-1",
			"user_initiated": true,
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		},
		"heartbeat": true
	}`

	var sr StreamResponse
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		t.Fatal(err)
	}
	if !sr.Heartbeat {
		t.Error("expected heartbeat=true")
	}
}

func TestStreamResponseConversationListUpdate(t *testing.T) {
	raw := `{
		"messages": [],
		"conversation": {
			"conversation_id": "conv-1",
			"user_initiated": true,
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		},
		"conversation_list_update": {
			"type": "update",
			"conversation": {
				"conversation_id": "conv-2",
				"slug": "updated-slug",
				"user_initiated": true,
				"created_at": "2025-01-01T00:00:00Z",
				"updated_at": "2025-01-01T01:00:00Z"
			}
		}
	}`

	var sr StreamResponse
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.ConversationListUpdate == nil {
		t.Fatal("expected non-nil conversation_list_update")
	}
	if sr.ConversationListUpdate.Type != "update" {
		t.Errorf("got type %q", sr.ConversationListUpdate.Type)
	}
	if sr.ConversationListUpdate.Conversation == nil {
		t.Fatal("expected non-nil conversation in update")
	}
	if sr.ConversationListUpdate.Conversation.Slug != "updated-slug" {
		t.Errorf("got slug %q", sr.ConversationListUpdate.Conversation.Slug)
	}
}

func TestChatRequestJSON(t *testing.T) {
	req := ChatRequest{
		Message: "Hello",
		Model:   "claude-sonnet-4-20250514",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var req2 ChatRequest
	if err := json.Unmarshal(data, &req2); err != nil {
		t.Fatal(err)
	}
	if req2.Message != "Hello" {
		t.Errorf("got message %q", req2.Message)
	}
	if req2.Model != "claude-sonnet-4-20250514" {
		t.Errorf("got model %q", req2.Model)
	}
}

func TestModelInfoJSON(t *testing.T) {
	raw := `{
		"id": "claude-sonnet-4-20250514",
		"display_name": "Claude Sonnet 4",
		"source": "anthropic",
		"ready": true,
		"max_context_tokens": 200000
	}`

	var m ModelInfo
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "claude-sonnet-4-20250514" {
		t.Errorf("got id %q", m.ID)
	}
	if m.DisplayName != "Claude Sonnet 4" {
		t.Errorf("got display_name %q", m.DisplayName)
	}
	if !m.Ready {
		t.Error("expected ready=true")
	}
	if m.MaxContextTokens != 200000 {
		t.Errorf("got max_context_tokens %d", m.MaxContextTokens)
	}
}

func TestToolResultContent(t *testing.T) {
	raw := `{
		"Role": 0,
		"Content": [
			{
				"Type": 6,
				"ToolUseID": "tool-123",
				"ToolError": true,
				"ToolResult": [
					{"Type": 2, "Text": "command not found"}
				]
			}
		]
	}`

	var msg LLMMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("got %d contents", len(msg.Content))
	}
	c := msg.Content[0]
	if c.Type != ContentTypeToolResult {
		t.Errorf("got type %d", c.Type)
	}
	if c.ToolUseID != "tool-123" {
		t.Errorf("got tool_use_id %q", c.ToolUseID)
	}
	if !c.ToolError {
		t.Error("expected tool_error=true")
	}
	if len(c.ToolResult) != 1 {
		t.Fatalf("got %d tool results", len(c.ToolResult))
	}
	if c.ToolResult[0].Text != "command not found" {
		t.Errorf("got text %q", c.ToolResult[0].Text)
	}
}

func TestConversationTimeParsing(t *testing.T) {
	raw := `{
		"conversation_id": "conv-time",
		"user_initiated": true,
		"created_at": "2025-06-15T14:30:00.123456Z",
		"updated_at": "2025-06-15T15:45:00Z"
	}`

	var c Conversation
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
	expected := time.Date(2025, 6, 15, 14, 30, 0, 123456000, time.UTC)
	if !c.CreatedAt.Equal(expected) {
		t.Errorf("got created_at %v, want %v", c.CreatedAt, expected)
	}
}
