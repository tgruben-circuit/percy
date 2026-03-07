package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tgruben-circuit/percy/claudetool"
	"github.com/tgruben-circuit/percy/db"
	"github.com/tgruben-circuit/percy/db/generated"
	"github.com/tgruben-circuit/percy/llm"
	"github.com/tgruben-circuit/percy/loop"
)

func TestToolCallContinuityAfterModelSwitch(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	llmManager := &switchTestLLMManager{services: map[string]llm.Service{
		"predictable":   loop.NewPredictableService(),
		"predictable-2": loop.NewPredictableService(),
	}}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := NewServer(database, llmManager, claudetool.ToolSetConfig{EnableBrowser: false}, logger, true, "", "predictable", "", nil)

	ctx := context.Background()
	initialModel := "predictable"
	conv, err := database.CreateConversation(ctx, nil, true, nil, &initialModel)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	assistantToolUse := llm.Message{
		Role: llm.MessageRoleAssistant,
		Content: []llm.Content{{
			Type:      llm.ContentTypeToolUse,
			ID:        "toolu_test_1",
			ToolName:  "bash",
			ToolInput: json.RawMessage(`{"command":"echo seeded","slow_ok":false}`),
		}},
	}
	if _, err := database.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ConversationID,
		Type:           db.MessageTypeAgent,
		LLMData:        assistantToolUse,
		UsageData:      llm.Usage{},
	}); err != nil {
		t.Fatalf("create tool_use message: %v", err)
	}

	userToolResult := llm.Message{
		Role: llm.MessageRoleUser,
		Content: []llm.Content{{
			Type:      llm.ContentTypeToolResult,
			ToolUseID: "toolu_test_1",
			ToolResult: []llm.Content{{
				Type: llm.ContentTypeText,
				Text: "seeded",
			}},
		}},
	}
	if _, err := database.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ConversationID,
		Type:           db.MessageTypeUser,
		LLMData:        userToolResult,
		UsageData:      llm.Usage{},
	}); err != nil {
		t.Fatalf("create tool_result message: %v", err)
	}

	switchReq := httptest.NewRequest("POST", "/"+conv.ConversationID+"/switch-model", strings.NewReader(`{"model":"predictable-2"}`))
	switchReq.Header.Set("Content-Type", "application/json")
	switchW := httptest.NewRecorder()
	server.conversationMux().ServeHTTP(switchW, switchReq)
	if switchW.Code != 200 {
		t.Fatalf("expected switch status 200, got %d: %s", switchW.Code, switchW.Body.String())
	}

	chatReq := httptest.NewRequest("POST", "/"+conv.ConversationID+"/chat", strings.NewReader(`{"message":"echo: continuity after switch","model":"predictable-2"}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatW := httptest.NewRecorder()
	server.conversationMux().ServeHTTP(chatW, chatReq)
	if chatW.Code != 202 {
		t.Fatalf("expected chat status 202, got %d: %s", chatW.Code, chatW.Body.String())
	}

	waitFor(t, 5*time.Second, func() bool {
		return !server.IsAgentWorking(conv.ConversationID)
	})

	var messages []generated.Message
	err = database.Queries(ctx, func(q *generated.Queries) error {
		var qerr error
		messages, qerr = q.ListMessages(ctx, conv.ConversationID)
		return qerr
	})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}

	foundToolResult := false
	foundPostSwitchResponse := false
	for _, msg := range messages {
		if msg.LlmData == nil {
			continue
		}
		var llmMsg llm.Message
		if err := json.Unmarshal([]byte(*msg.LlmData), &llmMsg); err != nil {
			continue
		}
		for _, c := range llmMsg.Content {
			if c.Type == llm.ContentTypeToolResult && c.ToolUseID == "toolu_test_1" {
				foundToolResult = true
			}
			if c.Type == llm.ContentTypeText && strings.Contains(c.Text, "continuity after switch") {
				foundPostSwitchResponse = true
			}
		}
	}

	if !foundToolResult {
		t.Fatal("expected seeded tool_result to remain in canonical transcript")
	}
	if !foundPostSwitchResponse {
		t.Fatal("expected post-switch response to be generated from continued transcript")
	}
}
