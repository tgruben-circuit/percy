package loop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tgruben-circuit/percy/claudetool"
	"github.com/tgruben-circuit/percy/llm"
)

func TestScriptedTools_ResultsStayOutOfContext(t *testing.T) {
	// Mock inner tool: returns a known string that should NOT leak into recorded messages.
	const intermediatePayload = "file content XYZ"
	mockReadFile := &llm.Tool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: llm.MustSchema(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{LLMContent: llm.TextContent(intermediatePayload)}
		},
	}

	// Build the ScriptedToolsTool with the mock inner tool.
	wd := claudetool.NewMutableWorkingDir(t.TempDir())
	st := &claudetool.ScriptedToolsTool{
		Tools:      []*llm.Tool{mockReadFile},
		WorkingDir: wd,
		Timeout:    30 * time.Second,
	}
	scriptedTool := st.Tool()

	// The script calls read_file 3 times and prints only a summary.
	script := `results = []
for i in range(3):
    r = await read_file(path="test.go")
    results.append(r)
print(f"Processed {len(results)} files")`

	scriptJSON, _ := json.Marshal(script)

	now := time.Now()

	// Mock LLM:
	//   Response 1: call scripted_tools with the script above
	//   Response 2: text "done", end turn
	mock := &capturingMockLLM{
		responses: []*llm.Response{
			{
				Role: llm.MessageRoleAssistant,
				Content: []llm.Content{
					{
						Type:      llm.ContentTypeToolUse,
						ID:        "tu_scripted_1",
						ToolName:  "scripted_tools",
						ToolInput: json.RawMessage(`{"script":` + string(scriptJSON) + `}`),
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
					{Type: llm.ContentTypeText, Text: "done"},
				},
				StopReason: llm.StopReasonEndTurn,
				Usage:      llm.Usage{InputTokens: 200, OutputTokens: 30},
				StartTime:  &now,
				EndTime:    &now,
			},
		},
	}

	// Collect all recorded messages.
	var mu sync.Mutex
	var recorded []llm.Message
	recordMessage := func(ctx context.Context, message llm.Message, usage llm.Usage) error {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, message)
		return nil
	}

	l := NewLoop(Config{
		LLM:           mock,
		History:       []llm.Message{},
		Tools:         []*llm.Tool{scriptedTool},
		RecordMessage: recordMessage,
	})

	l.QueueUserMessage(llm.Message{
		Role:    llm.MessageRoleUser,
		Content: []llm.Content{{Type: llm.ContentTypeText, Text: "run the script"}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := l.ProcessOneTurn(ctx); err != nil {
		t.Fatalf("ProcessOneTurn failed: %v", err)
	}

	// --- Assertions ---

	// Collect all text from recorded messages into one string for searching.
	var allText strings.Builder
	for _, msg := range recorded {
		for _, c := range msg.Content {
			allText.WriteString(c.Text)
			allText.WriteString(" ")
			// Also check inside tool results.
			for _, r := range c.ToolResult {
				allText.WriteString(r.Text)
				allText.WriteString(" ")
			}
		}
	}
	combined := allText.String()

	// The summary (print output) SHOULD appear in recorded messages.
	if !strings.Contains(combined, "Processed 3 files") {
		t.Errorf("expected 'Processed 3 files' in recorded messages, got:\n%s", combined)
	}

	// The intermediate tool result SHOULD NOT appear in recorded messages.
	if strings.Contains(combined, intermediatePayload) {
		t.Errorf("intermediate tool result %q should NOT appear in recorded messages, but found in:\n%s", intermediatePayload, combined)
	}
}
