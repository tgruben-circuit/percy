package server

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/tgruben-circuit/percy/db/generated"
	"github.com/tgruben-circuit/percy/slug"
)

// ForkConversationRequest is the request body for forking a conversation.
type ForkConversationRequest struct {
	SourceConversationID string `json:"source_conversation_id"`
	AtSequenceID         int64  `json:"at_sequence_id"`
	Model                string `json:"model,omitempty"`
}

func (s *Server) handleForkConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ForkConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.SourceConversationID == "" || req.AtSequenceID <= 0 {
		http.Error(w, "source_conversation_id and at_sequence_id (> 0) are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get source conversation
	source, err := s.db.GetConversationByID(ctx, req.SourceConversationID)
	if err != nil {
		http.Error(w, "Source conversation not found", http.StatusNotFound)
		return
	}

	// Determine model
	modelID := req.Model
	if modelID == "" && source.Model != nil {
		modelID = *source.Model
	}
	var modelPtr *string
	if modelID != "" {
		modelPtr = &modelID
	}

	// Create new conversation
	conv, err := s.db.CreateConversation(ctx, nil, true, source.Cwd, modelPtr)
	if err != nil {
		s.logger.Error("Failed to create forked conversation", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get messages up to the specified sequence
	var msgs []generated.Message
	if err := s.db.Queries(ctx, func(q *generated.Queries) error {
		var err error
		msgs, err = q.ListMessagesUpToSequence(ctx, generated.ListMessagesUpToSequenceParams{
			ConversationID: req.SourceConversationID,
			SequenceID:     req.AtSequenceID,
		})
		return err
	}); err != nil {
		s.logger.Error("Failed to get source messages", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Copy messages to new conversation
	if err := s.db.QueriesTx(ctx, func(q *generated.Queries) error {
		for i, msg := range msgs {
			_, err := q.CreateMessage(ctx, generated.CreateMessageParams{
				MessageID:           uuid.New().String(),
				ConversationID:      conv.ConversationID,
				SequenceID:          int64(i + 1),
				Type:                msg.Type,
				LlmData:             msg.LlmData,
				UserData:            msg.UserData,
				UsageData:           msg.UsageData,
				DisplayData:         msg.DisplayData,
				ExcludedFromContext: msg.ExcludedFromContext,
			})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		s.logger.Error("Failed to copy messages", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	go s.publishConversationListUpdate(ConversationListUpdate{
		Type:         "update",
		Conversation: conv,
	})

	// Generate slug from the first user message
	if len(msgs) > 0 {
		var firstUserText string
		for _, msg := range msgs {
			if msg.Type == "user" && msg.UserData != nil {
				var userData map[string]interface{}
				if err := json.Unmarshal([]byte(*msg.UserData), &userData); err == nil {
					if text, ok := userData["text"].(string); ok {
						firstUserText = text
						break
					}
				}
			}
		}
		if firstUserText != "" {
			go func() {
				_, err := slug.GenerateSlug(ctx, s.llmManager, s.db, s.logger, conv.ConversationID, firstUserText, modelID)
				if err != nil {
					s.logger.Warn("Failed to generate slug for fork", "error", err)
				}
			}()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"conversation_id": conv.ConversationID}) //nolint:errchkjson
}
