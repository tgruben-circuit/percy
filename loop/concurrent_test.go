package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgruben-circuit/percy/llm"
)

func TestExecuteTool(t *testing.T) {
	echoTool := &llm.Tool{
		Name:        "echo",
		Description: "echoes input",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {"msg": {"type": "string"}}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{
				LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "echoed"}},
			}
		},
	}

	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{echoTool},
	})

	content := llm.Content{
		ID:        "tool_1",
		Type:      llm.ContentTypeToolUse,
		ToolName:  "echo",
		ToolInput: json.RawMessage(`{"msg": "hi"}`),
	}

	result := l.executeTool(context.Background(), content, echoTool)

	if result.Type != llm.ContentTypeToolResult {
		t.Fatalf("expected tool result, got %s", result.Type)
	}
	if result.ToolUseID != "tool_1" {
		t.Fatalf("expected tool use ID 'tool_1', got %s", result.ToolUseID)
	}
	if result.ToolError {
		t.Fatal("expected no error")
	}
	if result.ToolUseStartTime == nil || result.ToolUseEndTime == nil {
		t.Fatal("expected start and end times to be set")
	}
	if result.ToolUseEndTime.Before(*result.ToolUseStartTime) {
		t.Fatal("end time should be after start time")
	}
}

func TestExecuteToolError(t *testing.T) {
	errorTool := &llm.Tool{
		Name:        "fail",
		Description: "always fails",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ErrorfToolOut("boom")
		},
	}

	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{errorTool},
	})

	content := llm.Content{
		ID:        "tool_2",
		Type:      llm.ContentTypeToolUse,
		ToolName:  "fail",
		ToolInput: json.RawMessage(`{}`),
	}

	result := l.executeTool(context.Background(), content, errorTool)

	if !result.ToolError {
		t.Fatal("expected ToolError to be true")
	}
	if len(result.ToolResult) != 1 || result.ToolResult[0].Text != "boom" {
		t.Fatalf("expected error text 'boom', got %v", result.ToolResult)
	}
}

func TestHandleToolCallsConcurrent(t *testing.T) {
	// Track concurrent execution with atomics
	var maxConcurrent atomic.Int32
	var current atomic.Int32

	makeTool := func(name string, concurrent bool, duration time.Duration) *llm.Tool {
		return &llm.Tool{
			Name:        name,
			Description: name,
			InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
			Concurrent:  concurrent,
			Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
				c := current.Add(1)
				for {
					old := maxConcurrent.Load()
					if c <= old || maxConcurrent.CompareAndSwap(old, c) {
						break
					}
				}
				timer := time.NewTimer(duration)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
				current.Add(-1)
				return llm.ToolOut{
					LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: name + " done"}},
				}
			},
		}
	}

	toolA := makeTool("concurrent-a", true, 50*time.Millisecond)
	toolB := makeTool("concurrent-b", true, 50*time.Millisecond)
	toolC := makeTool("sequential-c", false, 10*time.Millisecond)

	var recorded []llm.Message
	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{toolA, toolB, toolC},
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recorded = append(recorded, msg)
			return nil
		},
	})

	content := []llm.Content{
		{ID: "1", Type: llm.ContentTypeToolUse, ToolName: "concurrent-a", ToolInput: json.RawMessage(`{}`)},
		{ID: "2", Type: llm.ContentTypeToolUse, ToolName: "concurrent-b", ToolInput: json.RawMessage(`{}`)},
		{ID: "3", Type: llm.ContentTypeToolUse, ToolName: "sequential-c", ToolInput: json.RawMessage(`{}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.handleToolCalls(ctx, content)
	if err != nil {
		t.Fatalf("handleToolCalls failed: %v", err)
	}

	// Concurrent tools should have run in parallel (max concurrent >= 2)
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected concurrent tools to overlap, max concurrent was %d", maxConcurrent.Load())
	}

	// All 3 results should be recorded
	if len(recorded) < 1 {
		t.Fatal("expected at least 1 recorded message")
	}
	msg := recorded[0]
	if len(msg.Content) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(msg.Content))
	}

	// Results should be in original order
	for i, expectedID := range []string{"1", "2", "3"} {
		if msg.Content[i].ToolUseID != expectedID {
			t.Errorf("result %d: expected tool use ID %q, got %q", i, expectedID, msg.Content[i].ToolUseID)
		}
	}

	_ = runtime.GOMAXPROCS(0) // just to verify import is used
}

func TestHandleToolCallsResultOrdering(t *testing.T) {
	makeTool := func(name string, delay time.Duration) *llm.Tool {
		return &llm.Tool{
			Name:        name,
			Description: name,
			InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
			Concurrent:  true,
			Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
				return llm.ToolOut{
					LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: name}},
				}
			},
		}
	}

	slow := makeTool("slow", 80*time.Millisecond)
	medium := makeTool("medium", 40*time.Millisecond)
	fast := makeTool("fast", 10*time.Millisecond)

	var recorded []llm.Message
	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{slow, medium, fast},
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recorded = append(recorded, msg)
			return nil
		},
	})

	content := []llm.Content{
		{ID: "1", Type: llm.ContentTypeToolUse, ToolName: "slow", ToolInput: json.RawMessage(`{}`)},
		{ID: "2", Type: llm.ContentTypeToolUse, ToolName: "medium", ToolInput: json.RawMessage(`{}`)},
		{ID: "3", Type: llm.ContentTypeToolUse, ToolName: "fast", ToolInput: json.RawMessage(`{}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.handleToolCalls(ctx, content)
	if err != nil {
		t.Fatalf("handleToolCalls failed: %v", err)
	}

	if len(recorded) < 1 {
		t.Fatal("expected recorded message")
	}

	results := recorded[0].Content
	expectedOrder := []string{"1", "2", "3"}
	for i, id := range expectedOrder {
		if results[i].ToolUseID != id {
			t.Errorf("position %d: expected ID %q, got %q", i, id, results[i].ToolUseID)
		}
	}
}

func TestHandleToolCallsConcurrentErrorIndependence(t *testing.T) {
	successTool := &llm.Tool{
		Name:        "success",
		Description: "succeeds",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
		Concurrent:  true,
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{
				LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "ok"}},
			}
		},
	}

	failTool := &llm.Tool{
		Name:        "fail",
		Description: "fails",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
		Concurrent:  true,
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ErrorfToolOut("intentional failure")
		},
	}

	var recorded []llm.Message
	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{successTool, failTool},
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recorded = append(recorded, msg)
			return nil
		},
	})

	content := []llm.Content{
		{ID: "1", Type: llm.ContentTypeToolUse, ToolName: "success", ToolInput: json.RawMessage(`{}`)},
		{ID: "2", Type: llm.ContentTypeToolUse, ToolName: "fail", ToolInput: json.RawMessage(`{}`)},
		{ID: "3", Type: llm.ContentTypeToolUse, ToolName: "success", ToolInput: json.RawMessage(`{}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.handleToolCalls(ctx, content)
	if err != nil {
		t.Fatalf("handleToolCalls failed: %v", err)
	}

	if len(recorded) < 1 {
		t.Fatal("expected recorded message")
	}

	results := recorded[0].Content
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].ToolError {
		t.Error("result 0: expected success")
	}
	if !results[1].ToolError {
		t.Error("result 1: expected error")
	}
	if results[2].ToolError {
		t.Error("result 2: expected success (error independence)")
	}
}

func TestHandleToolCallsSemaphoreLimit(t *testing.T) {
	maxProcs := runtime.GOMAXPROCS(0)
	toolCount := maxProcs + 4

	var peak atomic.Int32
	var current atomic.Int32

	tools := make([]*llm.Tool, toolCount)
	for i := range tools {
		name := fmt.Sprintf("tool-%d", i)
		tools[i] = &llm.Tool{
			Name:        name,
			Description: name,
			InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
			Concurrent:  true,
			Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
				c := current.Add(1)
				for {
					old := peak.Load()
					if c <= old || peak.CompareAndSwap(old, c) {
						break
					}
				}
				timer := time.NewTimer(30 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
				current.Add(-1)
				return llm.ToolOut{
					LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "done"}},
				}
			},
		}
	}

	var recorded []llm.Message
	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   tools,
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recorded = append(recorded, msg)
			return nil
		},
	})

	var content []llm.Content
	for i := 0; i < toolCount; i++ {
		content = append(content, llm.Content{
			ID:        fmt.Sprintf("id-%d", i),
			Type:      llm.ContentTypeToolUse,
			ToolName:  fmt.Sprintf("tool-%d", i),
			ToolInput: json.RawMessage(`{}`),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := l.handleToolCalls(ctx, content)
	if err != nil {
		t.Fatalf("handleToolCalls failed: %v", err)
	}

	if int(peak.Load()) > maxProcs {
		t.Errorf("peak concurrency %d exceeded GOMAXPROCS %d", peak.Load(), maxProcs)
	}

	if len(recorded) < 1 || len(recorded[0].Content) != toolCount {
		t.Fatalf("expected %d results", toolCount)
	}
}

func TestHandleToolCallsSingleToolUnchanged(t *testing.T) {
	called := false
	tool := &llm.Tool{
		Name:        "single",
		Description: "single tool",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			called = true
			return llm.ToolOut{
				LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "result"}},
			}
		},
	}

	var recorded []llm.Message
	l := NewLoop(Config{
		LLM:     NewPredictableService(),
		History: []llm.Message{},
		Tools:   []*llm.Tool{tool},
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recorded = append(recorded, msg)
			return nil
		},
	})

	content := []llm.Content{
		{ID: "only", Type: llm.ContentTypeToolUse, ToolName: "single", ToolInput: json.RawMessage(`{}`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.handleToolCalls(ctx, content)
	if err != nil {
		t.Fatalf("handleToolCalls failed: %v", err)
	}

	if !called {
		t.Fatal("tool was not called")
	}

	if len(recorded) < 1 || len(recorded[0].Content) != 1 {
		t.Fatal("expected exactly 1 result")
	}

	if recorded[0].Content[0].ToolUseID != "only" {
		t.Error("wrong tool use ID")
	}
}
