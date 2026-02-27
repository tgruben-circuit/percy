package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tgruben-circuit/percy/db/generated"
	"github.com/tgruben-circuit/percy/llm"
)

// EditMessageRequest is the request body for editing a message.
type EditMessageRequest struct {
	SequenceID int64  `json:"sequence_id"`
	Message    string `json:"message"`
}

// handleEditMessage handles POST /api/conversation/{id}/edit
// Edits a user message at the given sequence and replays from that point.
func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request, conversationID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EditMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.SequenceID <= 0 || req.Message == "" {
		http.Error(w, "sequence_id and message are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get the conversation manager
	manager, err := s.getOrCreateConversationManager(ctx, conversationID)
	if err != nil {
		s.logger.Error("Failed to get conversation manager", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Cancel any running loop
	if err := manager.CancelConversation(ctx); err != nil {
		s.logger.Error("Failed to cancel conversation", "error", err)
	}

	// Reset the loop so ensureLoop reloads from DB
	manager.ResetLoop()

	// Delete messages from the edit point onwards
	if err := s.db.QueriesTx(ctx, func(q *generated.Queries) error {
		return q.DeleteMessagesFromSequence(ctx, generated.DeleteMessagesFromSequenceParams{
			ConversationID: conversationID,
			SequenceID:     req.SequenceID,
		})
	}); err != nil {
		s.logger.Error("Failed to delete messages", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Notify subscribers about the changes (clients should refetch)
	go s.notifySubscribers(ctx, conversationID)

	// Get the model for this conversation
	conv, err := s.db.GetConversationByID(ctx, conversationID)
	if err != nil {
		http.Error(w, "Conversation not found", http.StatusNotFound)
		return
	}

	modelID := s.defaultModel
	if conv.Model != nil {
		modelID = *conv.Model
	}

	service, err := s.llmManager.GetService(modelID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Model unavailable: %s", modelID), http.StatusBadRequest)
		return
	}

	// Send the edited message which will record it and start the loop
	userMessage := llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: req.Message}},
	}
	if _, err := manager.AcceptUserMessage(ctx, service, modelID, userMessage); err != nil {
		s.logger.Error("Failed to accept edited message", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleRegenerateMessage handles POST /api/conversation/{id}/regenerate
// Deletes the last agent response and re-triggers the LLM.
func (s *Server) handleRegenerateMessage(w http.ResponseWriter, r *http.Request, conversationID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Find the last user message
	messages, err := s.db.ListMessages(ctx, conversationID)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	// Find the last user message sequence ID and text
	var lastUserSeqID int64
	var lastUserText string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == "user" {
			lastUserSeqID = messages[i].SequenceID
			// Extract text from user_data
			if messages[i].UserData != nil {
				var userData map[string]interface{}
				if err := json.Unmarshal([]byte(*messages[i].UserData), &userData); err == nil {
					if text, ok := userData["text"].(string); ok {
						lastUserText = text
					}
				}
			}
			// Fallback to llm_data
			if lastUserText == "" && messages[i].LlmData != nil {
				var llmMsg llm.Message
				if err := json.Unmarshal([]byte(*messages[i].LlmData), &llmMsg); err == nil {
					for _, c := range llmMsg.Content {
						if c.Type == llm.ContentTypeText && c.Text != "" {
							lastUserText = c.Text
							break
						}
					}
				}
			}
			break
		}
	}

	if lastUserSeqID == 0 {
		http.Error(w, "No user message found to regenerate from", http.StatusBadRequest)
		return
	}

	// Get conversation manager and cancel/reset
	manager, err := s.getOrCreateConversationManager(ctx, conversationID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := manager.CancelConversation(ctx); err != nil {
		s.logger.Error("Failed to cancel for regenerate", "error", err)
	}
	manager.ResetLoop()

	// Delete the user message and everything after it
	if err := s.db.QueriesTx(ctx, func(q *generated.Queries) error {
		return q.DeleteMessagesFromSequence(ctx, generated.DeleteMessagesFromSequenceParams{
			ConversationID: conversationID,
			SequenceID:     lastUserSeqID,
		})
	}); err != nil {
		s.logger.Error("Failed to delete messages for regenerate", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Notify subscribers
	go s.notifySubscribers(ctx, conversationID)

	// Get model and re-send the same user message
	conv, err := s.db.GetConversationByID(ctx, conversationID)
	if err != nil {
		http.Error(w, "Conversation not found", http.StatusNotFound)
		return
	}

	modelID := s.defaultModel
	if conv.Model != nil {
		modelID = *conv.Model
	}

	service, err := s.llmManager.GetService(modelID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Model unavailable: %s", modelID), http.StatusBadRequest)
		return
	}

	userMessage := llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: lastUserText}},
	}
	if _, err := manager.AcceptUserMessage(ctx, service, modelID, userMessage); err != nil {
		s.logger.Error("Failed to re-send for regenerate", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
