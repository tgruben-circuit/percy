package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/tgruben-circuit/percy/db/generated"
	"github.com/tgruben-circuit/percy/llm"
)

func (s *Server) handleExportConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "markdown"
	}

	ctx := r.Context()
	conv, err := s.db.GetConversationByID(ctx, conversationID)
	if err != nil {
		http.Error(w, "Conversation not found", http.StatusNotFound)
		return
	}

	messages, err := s.db.ListMessages(ctx, conversationID)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	slug := conversationID
	if conv.Slug != nil {
		slug = *conv.Slug
	}

	switch format {
	case "markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, slug))
		exportMarkdown(w, conv, messages)
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, slug))
		exportJSON(w, conv, messages)
	default:
		http.Error(w, "Invalid format. Use 'markdown' or 'json'", http.StatusBadRequest)
	}
}

func exportMarkdown(w io.Writer, conv *generated.Conversation, messages []generated.Message) {
	slug := conv.ConversationID
	if conv.Slug != nil {
		slug = *conv.Slug
	}

	fmt.Fprintf(w, "# %s\n\n", slug)
	fmt.Fprintf(w, "**Date:** %s\n\n", conv.CreatedAt.Format("2006-01-02 15:04"))
	if conv.Model != nil {
		fmt.Fprintf(w, "**Model:** %s\n\n", *conv.Model)
	}
	fmt.Fprintf(w, "---\n\n")

	for _, msg := range messages {
		switch msg.Type {
		case "user":
			fmt.Fprintf(w, "## User\n\n")
			writeMessageContent(w, msg)
			fmt.Fprintf(w, "\n---\n\n")
		case "agent":
			fmt.Fprintf(w, "## Assistant\n\n")
			writeMessageContent(w, msg)
			fmt.Fprintf(w, "\n---\n\n")
		case "tool":
			writeMessageContent(w, msg)
		case "error":
			fmt.Fprintf(w, "> **Error:** ")
			writeMessageContent(w, msg)
			fmt.Fprintf(w, "\n\n")
		case "system", "gitinfo":
			// Skip system and gitinfo messages in export
		}
	}
}

func writeMessageContent(w io.Writer, msg generated.Message) {
	// Try to extract text from user_data first (for user messages)
	if msg.UserData != nil {
		var userData map[string]interface{}
		if err := json.Unmarshal([]byte(*msg.UserData), &userData); err == nil {
			if text, ok := userData["text"].(string); ok && text != "" {
				fmt.Fprintf(w, "%s\n", text)
				return
			}
		}
	}

	// Fall back to llm_data
	if msg.LlmData == nil {
		return
	}

	var llmMsg llm.Message
	if err := json.Unmarshal([]byte(*msg.LlmData), &llmMsg); err != nil {
		return
	}

	for _, content := range llmMsg.Content {
		switch content.Type {
		case llm.ContentTypeText:
			if content.Text != "" {
				fmt.Fprintf(w, "%s\n", content.Text)
			}
		case llm.ContentTypeToolUse:
			fmt.Fprintf(w, "\n**Tool: %s**\n", content.ToolName)
			if len(content.ToolInput) > 0 {
				// Pretty-print tool input
				var prettyInput interface{}
				if err := json.Unmarshal(content.ToolInput, &prettyInput); err == nil {
					if formatted, err := json.MarshalIndent(prettyInput, "", "  "); err == nil {
						fmt.Fprintf(w, "```json\n%s\n```\n", string(formatted))
					}
				}
			}
		case llm.ContentTypeToolResult:
			for _, res := range content.ToolResult {
				if res.Type == llm.ContentTypeText && res.Text != "" {
					// Truncate very long tool results
					text := res.Text
					if len(text) > 2000 {
						text = text[:2000] + "\n... (truncated)"
					}
					fmt.Fprintf(w, "```\n%s\n```\n", text)
				}
			}
		case llm.ContentTypeThinking:
			// Skip thinking blocks
		}
	}
}

func exportJSON(w io.Writer, conv *generated.Conversation, messages []generated.Message) {
	type exportMessage struct {
		Type      string      `json:"type"`
		Sequence  int64       `json:"sequence_id"`
		CreatedAt string      `json:"created_at"`
		LLMData   interface{} `json:"llm_data,omitempty"`
		UserData  interface{} `json:"user_data,omitempty"`
		UsageData interface{} `json:"usage_data,omitempty"`
	}

	type exportConversation struct {
		ID        string          `json:"conversation_id"`
		Slug      *string         `json:"slug,omitempty"`
		Model     *string         `json:"model,omitempty"`
		CreatedAt string          `json:"created_at"`
		UpdatedAt string          `json:"updated_at"`
		Messages  []exportMessage `json:"messages"`
	}

	export := exportConversation{
		ID:        conv.ConversationID,
		Slug:      conv.Slug,
		Model:     conv.Model,
		CreatedAt: conv.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: conv.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	for _, msg := range messages {
		em := exportMessage{
			Type:      msg.Type,
			Sequence:  msg.SequenceID,
			CreatedAt: msg.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		// Parse JSON strings into objects so they're not double-escaped
		if msg.LlmData != nil {
			var v interface{}
			if err := json.Unmarshal([]byte(*msg.LlmData), &v); err == nil {
				em.LLMData = v
			}
		}
		if msg.UserData != nil {
			var v interface{}
			if err := json.Unmarshal([]byte(*msg.UserData), &v); err == nil {
				em.UserData = v
			}
		}
		if msg.UsageData != nil {
			var v interface{}
			if err := json.Unmarshal([]byte(*msg.UsageData), &v); err == nil {
				em.UsageData = v
			}
		}
		export.Messages = append(export.Messages, em)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(export)
}
