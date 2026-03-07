package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tgruben-circuit/percy/claudetool"
	"github.com/tgruben-circuit/percy/llm"
	"github.com/tgruben-circuit/percy/loop"
	"github.com/tgruben-circuit/percy/models"
)

type switchTestLLMManager struct {
	services map[string]llm.Service
}

func (m *switchTestLLMManager) GetService(modelID string) (llm.Service, error) {
	svc, ok := m.services[modelID]
	if !ok {
		return nil, fmt.Errorf("model not found: %s", modelID)
	}
	return svc, nil
}

func (m *switchTestLLMManager) GetAvailableModels() []string {
	modelsList := make([]string, 0, len(m.services))
	for id := range m.services {
		modelsList = append(modelsList, id)
	}
	return modelsList
}

func (m *switchTestLLMManager) HasModel(modelID string) bool {
	_, ok := m.services[modelID]
	return ok
}

func (m *switchTestLLMManager) GetModelInfo(modelID string) *models.ModelInfo {
	return nil
}

func (m *switchTestLLMManager) RefreshCustomModels() error {
	return nil
}

func TestSwitchModelEndpointHappyPath(t *testing.T) {
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

	reqBody := bytes.NewBufferString(`{"model":"predictable-2"}`)
	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/switch-model", reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.conversationMux().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %q", resp.Status)
	}
	if resp.Model != "predictable-2" {
		t.Fatalf("expected model predictable-2, got %q", resp.Model)
	}
}

func TestSwitchModelEndpointRequiresModel(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	llmManager := &switchTestLLMManager{services: map[string]llm.Service{
		"predictable": loop.NewPredictableService(),
	}}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := NewServer(database, llmManager, claudetool.ToolSetConfig{EnableBrowser: false}, logger, true, "", "predictable", "", nil)

	conv, err := database.CreateConversation(context.Background(), nil, true, nil, nil)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/switch-model", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.conversationMux().ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchModelEndpointRejectsUnsupportedModel(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	llmManager := &switchTestLLMManager{services: map[string]llm.Service{
		"predictable": loop.NewPredictableService(),
	}}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := NewServer(database, llmManager, claudetool.ToolSetConfig{EnableBrowser: false}, logger, true, "", "predictable", "", nil)

	conv, err := database.CreateConversation(context.Background(), nil, true, nil, nil)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/switch-model", bytes.NewBufferString(`{"model":"missing"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.conversationMux().ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchModelEndpointReturnsConflictWhenActiveWithoutCancel(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	llmManager := &switchTestLLMManager{services: map[string]llm.Service{
		"predictable":   loop.NewPredictableService(),
		"predictable-2": loop.NewPredictableService(),
	}}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := NewServer(database, llmManager, claudetool.ToolSetConfig{EnableBrowser: false}, logger, true, "", "predictable", "", nil)

	conv, err := database.CreateConversation(context.Background(), nil, true, nil, nil)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	chatReq := httptest.NewRequest("POST", "/"+conv.ConversationID+"/chat", bytes.NewBufferString(`{"message":"bash: tail -f /dev/null","model":"predictable"}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatW := httptest.NewRecorder()
	server.conversationMux().ServeHTTP(chatW, chatReq)
	if chatW.Code != 202 {
		t.Fatalf("expected chat status 202, got %d: %s", chatW.Code, chatW.Body.String())
	}

	req := httptest.NewRequest("POST", "/"+conv.ConversationID+"/switch-model", bytes.NewBufferString(`{"model":"predictable-2"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.conversationMux().ServeHTTP(w, req)

	if w.Code != 409 {
		t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
	}

	cancelReq := httptest.NewRequest("POST", "/"+conv.ConversationID+"/cancel", nil)
	cancelW := httptest.NewRecorder()
	server.conversationMux().ServeHTTP(cancelW, cancelReq)
	if cancelW.Code != 200 {
		t.Fatalf("expected cancel status 200, got %d: %s", cancelW.Code, cancelW.Body.String())
	}
}
