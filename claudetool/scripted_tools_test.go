package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgruben-circuit/percy/llm"
)

func TestGenerateHarness(t *testing.T) {
	tools := []*llm.Tool{
		{
			Name: "read_file",
			Run:  func(ctx context.Context, input json.RawMessage) llm.ToolOut { return llm.ToolOut{} },
		},
		{
			Name: "bash",
			Run:  func(ctx context.Context, input json.RawMessage) llm.ToolOut { return llm.ToolOut{} },
		},
	}

	harness := generateHarness(tools, "x = 1\nprint(x)")

	// Check imports
	if !strings.Contains(harness, "import sys, json, asyncio") {
		t.Error("missing imports")
	}

	// Check IPC setup
	if !strings.Contains(harness, "_ipc_out = sys.stdout") {
		t.Error("missing _ipc_out")
	}
	if !strings.Contains(harness, "def _call_tool(name, input_dict)") {
		t.Error("missing _call_tool")
	}

	// Check tool stubs
	if !strings.Contains(harness, "async def read_file(**kwargs):") {
		t.Error("missing read_file stub")
	}
	if !strings.Contains(harness, "async def bash(**kwargs):") {
		t.Error("missing bash stub")
	}

	// Check stdout redirect
	if !strings.Contains(harness, "sys.stdout = sys.stderr") {
		t.Error("missing stdout redirect")
	}

	// Check user script wrapped in async def _main()
	if !strings.Contains(harness, "async def _main():") {
		t.Error("missing async def _main")
	}
	if !strings.Contains(harness, "    x = 1") {
		t.Error("user script not indented")
	}
	if !strings.Contains(harness, "    print(x)") {
		t.Error("user script second line not indented")
	}

	// Check asyncio.run
	if !strings.Contains(harness, "asyncio.run(_main())") {
		t.Error("missing asyncio.run")
	}
}

func TestFilterScriptableTools(t *testing.T) {
	tools := []*llm.Tool{
		{Name: "bash"},
		{Name: "scripted_tools"},
		{Name: "subagent"},
		{Name: "request_tools"},
		{Name: "read_file"},
	}

	filtered := filterScriptableTools(tools)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
	names := make(map[string]bool)
	for _, t := range filtered {
		names[t.Name] = true
	}
	if !names["bash"] || !names["read_file"] {
		t.Errorf("expected bash and read_file, got %v", names)
	}
}

func TestScriptedTools_BasicPrint(t *testing.T) {
	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      nil,
		WorkingDir: wd,
	}

	input, _ := json.Marshal(scriptedToolsInput{Script: "print('hello world')"})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	if len(out.LLMContent) == 0 {
		t.Fatal("expected content")
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", text)
	}
}

func TestScriptedTools_SingleToolCall(t *testing.T) {
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{LLMContent: llm.TextContent("file contents here")}
		},
	}

	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      []*llm.Tool{mockTool},
		WorkingDir: wd,
	}

	script := `result = await read_file(path="test.txt")
print(result)`
	input, _ := json.Marshal(scriptedToolsInput{Script: script})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	if len(out.LLMContent) == 0 {
		t.Fatal("expected content")
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "file contents here") {
		t.Errorf("expected 'file contents here' in output, got %q", text)
	}
}

func TestScriptedTools_SyntaxError(t *testing.T) {
	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      nil,
		WorkingDir: wd,
	}

	input, _ := json.Marshal(scriptedToolsInput{Script: "def foo(:\n  pass"})
	out := st.Run(context.Background(), json.RawMessage(input))
	// Syntax errors should be returned as content, not as error
	if out.Error != nil {
		t.Fatalf("expected no error (traceback as content), got: %v", out.Error)
	}
	if len(out.LLMContent) == 0 {
		t.Fatal("expected content with traceback")
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "SyntaxError") {
		t.Errorf("expected SyntaxError in output, got %q", text)
	}
}

func TestScriptedTools_MultipleToolCalls(t *testing.T) {
	var callCount atomic.Int32
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			callCount.Add(1)
			var kwargs struct {
				Path string `json:"path"`
			}
			json.Unmarshal(input, &kwargs)
			return llm.ToolOut{LLMContent: llm.TextContent(fmt.Sprintf("content of %s", kwargs.Path))}
		},
	}

	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      []*llm.Tool{mockTool},
		WorkingDir: wd,
	}

	script := `paths = ["a.go", "b.go", "c.go"]
results = []
for p in paths:
    content = await read_file(path=p)
    results.append(content)
print(f"Read {len(results)} files")`
	input, _ := json.Marshal(scriptedToolsInput{Script: script})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	if got := callCount.Load(); got != 3 {
		t.Errorf("expected 3 tool calls, got %d", got)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "Read 3 files") {
		t.Errorf("expected 'Read 3 files' in output, got %q", text)
	}
}

func TestScriptedTools_ToolError(t *testing.T) {
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ErrorfToolOut("file not found: test.go")
		},
	}

	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      []*llm.Tool{mockTool},
		WorkingDir: wd,
	}

	script := `result = await read_file(path="test.go")
print(result)`
	input, _ := json.Marshal(scriptedToolsInput{Script: script})
	out := st.Run(context.Background(), json.RawMessage(input))
	// Script crashes with RuntimeError; traceback returned as content
	if out.Error != nil {
		t.Fatalf("expected no error (traceback as content), got: %v", out.Error)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "file not found") {
		t.Errorf("expected 'file not found' in output, got %q", text)
	}
}

func TestScriptedTools_ToolErrorCaught(t *testing.T) {
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ErrorfToolOut("file not found: missing.go")
		},
	}

	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      []*llm.Tool{mockTool},
		WorkingDir: wd,
	}

	script := `try:
    result = await read_file(path="missing.go")
except RuntimeError as e:
    print(f"caught error: {e}")`
	input, _ := json.Marshal(scriptedToolsInput{Script: script})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "caught error: file not found") {
		t.Errorf("expected 'caught error: file not found' in output, got %q", text)
	}
}

func TestScriptedTools_Timeout(t *testing.T) {
	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      nil,
		WorkingDir: wd,
		Timeout:    2 * time.Second,
	}

	script := `import time
time.sleep(30)`
	input, _ := json.Marshal(scriptedToolsInput{Script: script})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(out.Error.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got %q", out.Error.Error())
	}
}

func TestScriptedTools_EmptyScript(t *testing.T) {
	wd := NewMutableWorkingDir(t.TempDir())
	st := &ScriptedToolsTool{
		Tools:      nil,
		WorkingDir: wd,
	}

	input, _ := json.Marshal(scriptedToolsInput{Script: ""})
	out := st.Run(context.Background(), json.RawMessage(input))
	if out.Error == nil {
		t.Fatal("expected error for empty script")
	}
	if !strings.Contains(out.Error.Error(), "empty") {
		t.Errorf("expected 'empty' in error, got %q", out.Error.Error())
	}
}
