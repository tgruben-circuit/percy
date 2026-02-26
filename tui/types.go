// Package tui implements a Bubble Tea terminal UI client for Percy.
// It is a pure HTTP/SSE client — it does NOT import server internals.
// All types are independently defined to match the JSON wire format.
package tui

import (
	"encoding/json"
	"time"
)

// ContentType mirrors llm.ContentType integer values.
// These share a const block with MessageRole in the server, so they start at 2.
const (
	ContentTypeText             = 2
	ContentTypeThinking         = 3
	ContentTypeRedactedThinking = 4
	ContentTypeToolUse          = 5
	ContentTypeToolResult       = 6
)

// Conversation mirrors generated.Conversation JSON shape.
type Conversation struct {
	ConversationID       string    `json:"conversation_id"`
	Slug                 string    `json:"slug"`
	UserInitiated        bool      `json:"user_initiated"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	Cwd                  string    `json:"cwd"`
	Archived             bool      `json:"archived"`
	ParentConversationID string    `json:"parent_conversation_id"`
	Model                string    `json:"model"`
}

// ConversationWithState extends Conversation with a working flag.
type ConversationWithState struct {
	Conversation
	Working bool `json:"working"`
}

// APIMessage mirrors server.APIMessage.
type APIMessage struct {
	MessageID      string    `json:"message_id"`
	ConversationID string    `json:"conversation_id"`
	SequenceID     int64     `json:"sequence_id"`
	Type           string    `json:"type"`
	LlmData        *string   `json:"llm_data,omitempty"`
	UserData       *string   `json:"user_data,omitempty"`
	UsageData      *string   `json:"usage_data,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	DisplayData    *string   `json:"display_data,omitempty"`
	EndOfTurn      *bool     `json:"end_of_turn,omitempty"`
}

// ConversationState mirrors server.ConversationState.
type ConversationState struct {
	ConversationID string `json:"conversation_id"`
	Working        bool   `json:"working"`
	Model          string `json:"model,omitempty"`
}

// ConversationListUpdate mirrors server.ConversationListUpdate.
type ConversationListUpdate struct {
	Type           string        `json:"type"`
	Conversation   *Conversation `json:"conversation,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
}

// StreamResponse mirrors server.StreamResponse.
type StreamResponse struct {
	Messages               []APIMessage            `json:"messages"`
	Conversation           Conversation            `json:"conversation"`
	ConversationState      *ConversationState      `json:"conversation_state,omitempty"`
	ContextWindowSize      uint64                  `json:"context_window_size,omitempty"`
	ConversationListUpdate *ConversationListUpdate `json:"conversation_list_update,omitempty"`
	Heartbeat              bool                    `json:"heartbeat,omitempty"`
}

// LLMMessage mirrors llm.Message stored in APIMessage.LlmData.
type LLMMessage struct {
	Role    int          `json:"Role"`
	Content []LLMContent `json:"Content"`

	EndOfTurn bool `json:"EndOfTurn"`
}

// LLMContent mirrors llm.Content stored within LLMMessage.
type LLMContent struct {
	ID   string `json:"ID,omitempty"`
	Type int    `json:"Type"`
	Text string `json:"Text,omitempty"`

	// Thinking
	Thinking string `json:"Thinking,omitempty"`

	// Tool use
	ToolName  string          `json:"ToolName,omitempty"`
	ToolInput json.RawMessage `json:"ToolInput,omitempty"`

	// Tool result
	ToolUseID  string       `json:"ToolUseID,omitempty"`
	ToolError  bool         `json:"ToolError,omitempty"`
	ToolResult []LLMContent `json:"ToolResult,omitempty"`

	// Media
	MediaType string `json:"MediaType,omitempty"`
	Data      string `json:"Data,omitempty"`
}

// ChatRequest is the request body for POST /api/conversation/{id}/chat
// and POST /api/conversations/new.
type ChatRequest struct {
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
}

// ModelInfo mirrors the response from GET /api/models.
type ModelInfo struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name,omitempty"`
	Source           string `json:"source,omitempty"`
	Ready            bool   `json:"ready"`
	MaxContextTokens int    `json:"max_context_tokens,omitempty"`
}

// ParseLLMData parses the llm_data JSON string into an LLMMessage.
func ParseLLMData(data string) (LLMMessage, error) {
	var msg LLMMessage
	err := json.Unmarshal([]byte(data), &msg)
	return msg, err
}
