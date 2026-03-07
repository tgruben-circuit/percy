package loop

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgruben-circuit/percy/llm"
)

// capturingMockLLM is a mock LLM that records each request and returns
// pre-configured responses in sequence.
type capturingMockLLM struct {
	mu        sync.Mutex
	requests  []*llm.Request
	responses []*llm.Response
	callIndex int
}

func (m *capturingMockLLM) Do(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	idx := m.callIndex
	m.callIndex++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return m.responses[len(m.responses)-1], nil
}

func (m *capturingMockLLM) TokenContextWindow() int { return 200000 }
func (m *capturingMockLLM) MaxImageDimension() int  { return 0 }

func (m *capturingMockLLM) getRequests() []*llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*llm.Request, len(m.requests))
	copy(out, m.requests)
	return out
}

func TestLoopDeferredToolFiltering(t *testing.T) {
	// Track whether each tool was executed
	var coreToolCalled atomic.Int32
	var deferredToolCalled atomic.Int32

	coreTool := &llm.Tool{
		Name:        "core_tool",
		Description: "A core tool always available",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {"msg": {"type": "string"}}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			coreToolCalled.Add(1)
			return llm.ToolOut{
				LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "core tool executed"}},
			}
		},
	}

	deferredTool := &llm.Tool{
		Name:        "deferred_tool",
		Description: "A deferred tool, initially hidden from the LLM",
		InputSchema: llm.MustSchema(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			deferredToolCalled.Add(1)
			return llm.ToolOut{
				LLMContent: []llm.Content{{Type: llm.ContentTypeText, Text: "deferred tool executed"}},
			}
		},
	}

	allTools := []*llm.Tool{coreTool, deferredTool}

	// activated controls whether the deferred tool is visible to the LLM.
	// Initially false — only coreTool is returned by ActiveToolsFn.
	var activated atomic.Bool

	activeToolsFn := func() []*llm.Tool {
		if activated.Load() {
			return allTools
		}
		return []*llm.Tool{coreTool}
	}

	now := time.Now()

	// Prepare mock responses:
	//
	// Call 1: LLM receives only core_tool. It responds with a tool_use for
	//         the deferred_tool (simulating an activation flow like request_tools).
	//
	// Call 2: After the deferred tool executes and activation flag is flipped,
	//         the LLM receives both tools. It responds with end_turn.
	mock := &capturingMockLLM{
		responses: []*llm.Response{
			{
				Role: llm.MessageRoleAssistant,
				Content: []llm.Content{
					{Type: llm.ContentTypeText, Text: "Calling the deferred tool"},
					{
						Type:      llm.ContentTypeToolUse,
						ID:        "tu_deferred_1",
						ToolName:  "deferred_tool",
						ToolInput: json.RawMessage(`{"data": "test"}`),
					},
				},
				StopReason: llm.StopReasonToolUse,
				Usage:      llm.Usage{InputTokens: 100, OutputTokens: 50},
				StartTime:  &now,
				EndTime:    &now,
			},
			{
				Role: llm.MessageRoleAssistant,
				Content: []llm.Content{
					{Type: llm.ContentTypeText, Text: "All done"},
				},
				StopReason: llm.StopReasonEndTurn,
				Usage:      llm.Usage{InputTokens: 200, OutputTokens: 30},
				StartTime:  &now,
				EndTime:    &now,
			},
		},
	}

	// Use a custom RecordMessage that flips the activation flag when the
	// deferred tool result is recorded. This simulates the real flow where
	// executing a tool like "request_tools" causes ActiveToolsFn to include
	// more tools on subsequent LLM calls.
	recordMessage := func(ctx context.Context, message llm.Message, usage llm.Usage) error {
		// When we see a tool result being recorded, activate the deferred tool
		// for subsequent LLM requests.
		for _, c := range message.Content {
			if c.Type == llm.ContentTypeToolResult {
				activated.Store(true)
			}
		}
		return nil
	}

	l := NewLoop(Config{
		LLM:           mock,
		History:       []llm.Message{},
		Tools:         allTools,
		ActiveToolsFn: activeToolsFn,
		RecordMessage: recordMessage,
	})

	// Queue a user message to kick off the loop
	l.QueueUserMessage(llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "Please activate the deferred tool"}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.ProcessOneTurn(ctx)
	if err != nil {
		t.Fatalf("ProcessOneTurn failed: %v", err)
	}

	// --- Assertions ---

	requests := mock.getRequests()
	if len(requests) < 2 {
		t.Fatalf("expected at least 2 LLM requests, got %d", len(requests))
	}

	// 1. First LLM request should only contain the core tool.
	req1Tools := requests[0].Tools
	req1ToolNames := toolNames(req1Tools)
	if len(req1Tools) != 1 {
		t.Errorf("call 1: expected 1 tool, got %d: %v", len(req1Tools), req1ToolNames)
	}
	if req1ToolNames[0] != "core_tool" {
		t.Errorf("call 1: expected tool 'core_tool', got %q", req1ToolNames[0])
	}

	// 2. The deferred tool should have been executed even though it wasn't
	//    in the active tools list (it's looked up from allTools).
	if deferredToolCalled.Load() != 1 {
		t.Errorf("expected deferred_tool to be called once, got %d", deferredToolCalled.Load())
	}

	// 3. Second LLM request (after tool execution) should contain both tools
	//    because activated is now true.
	req2Tools := requests[1].Tools
	req2ToolNames := toolNames(req2Tools)
	if len(req2Tools) != 2 {
		t.Errorf("call 2: expected 2 tools, got %d: %v", len(req2Tools), req2ToolNames)
	}
	hasCore := false
	hasDeferred := false
	for _, name := range req2ToolNames {
		switch name {
		case "core_tool":
			hasCore = true
		case "deferred_tool":
			hasDeferred = true
		}
	}
	if !hasCore || !hasDeferred {
		t.Errorf("call 2: expected both core_tool and deferred_tool, got %v", req2ToolNames)
	}

	// 4. core_tool should NOT have been called (the LLM never issued a tool_use for it).
	if coreToolCalled.Load() != 0 {
		t.Errorf("expected core_tool not to be called, got %d", coreToolCalled.Load())
	}
}

// toolNames extracts tool names from a slice of tools.
func toolNames(tools []*llm.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
