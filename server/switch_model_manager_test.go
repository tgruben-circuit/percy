package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/tgruben-circuit/percy/claudetool"
	"github.com/tgruben-circuit/percy/llm"
	"github.com/tgruben-circuit/percy/loop"
)

func TestConversationModelSwitchIdleUpdatesModel(t *testing.T) {
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

	manager, err := server.getOrCreateConversationManager(ctx, conv.ConversationID)
	if err != nil {
		t.Fatalf("get manager: %v", err)
	}

	serviceA, _ := llmManager.GetService("predictable")
	serviceB, _ := llmManager.GetService("predictable-2")
	if err := manager.ensureLoop(serviceA, "predictable"); err != nil {
		t.Fatalf("ensure loop: %v", err)
	}

	if err := manager.SwitchModel(ctx, serviceB, "predictable-2", false); err != nil {
		t.Fatalf("switch model: %v", err)
	}

	if got := manager.GetModel(); got != "predictable-2" {
		t.Fatalf("expected manager model predictable-2, got %q", got)
	}

	updated, err := database.GetConversationByID(ctx, conv.ConversationID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if updated.Model == nil {
		t.Fatal("expected db model predictable-2, got nil")
	}
	if *updated.Model != "predictable-2" {
		t.Fatalf("expected db model predictable-2, got %q", *updated.Model)
	}
}

func TestConversationModelSwitchActiveWithoutCancelReturnsConflict(t *testing.T) {
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

	manager, err := server.getOrCreateConversationManager(ctx, conv.ConversationID)
	if err != nil {
		t.Fatalf("get manager: %v", err)
	}

	serviceA, _ := llmManager.GetService("predictable")
	serviceB, _ := llmManager.GetService("predictable-2")
	_, err = manager.AcceptUserMessage(ctx, serviceA, "predictable", llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "bash: tail -f /dev/null"}},
	})
	if err != nil {
		t.Fatalf("accept message: %v", err)
	}

	err = manager.SwitchModel(ctx, serviceB, "predictable-2", false)
	if !errors.Is(err, errSwitchModelConflict) {
		t.Fatalf("expected errSwitchModelConflict, got %v", err)
	}

	if cancelErr := manager.CancelConversation(ctx); cancelErr != nil {
		t.Fatalf("cancel conversation: %v", cancelErr)
	}
}

func TestConversationModelSwitchActiveWithCancelSucceeds(t *testing.T) {
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

	manager, err := server.getOrCreateConversationManager(ctx, conv.ConversationID)
	if err != nil {
		t.Fatalf("get manager: %v", err)
	}

	serviceA, _ := llmManager.GetService("predictable")
	serviceB, _ := llmManager.GetService("predictable-2")
	_, err = manager.AcceptUserMessage(ctx, serviceA, "predictable", llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "bash: tail -f /dev/null"}},
	})
	if err != nil {
		t.Fatalf("accept message: %v", err)
	}

	if err := manager.SwitchModel(ctx, serviceB, "predictable-2", true); err != nil {
		t.Fatalf("switch model: %v", err)
	}

	if got := manager.GetModel(); got != "predictable-2" {
		t.Fatalf("expected manager model predictable-2, got %q", got)
	}
	if manager.IsAgentWorking() {
		t.Fatal("expected agent to be idle after cancel-and-switch")
	}

	updated, err := database.GetConversationByID(ctx, conv.ConversationID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if updated.Model == nil {
		t.Fatal("expected db model predictable-2, got nil")
	}
	if *updated.Model != "predictable-2" {
		t.Fatalf("expected db model predictable-2, got %q", *updated.Model)
	}
}
