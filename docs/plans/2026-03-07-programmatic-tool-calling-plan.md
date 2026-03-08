# Programmatic Tool Calling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `scripted_tools` tool that lets the LLM write Python scripts calling Percy's tools programmatically, with only the final printed output entering the conversation context.

**Architecture:** A new `ScriptedToolsTool` in `claudetool/` generates a Python harness with async stubs for each tool, communicates via JSON-lines IPC over stdin/stdout, and captures stderr (where `print()` is redirected) as the sole tool result. Uses `uv run python` to execute.

**Tech Stack:** Go (`claudetool` package), Python 3 (via `uv`). No new Go dependencies.

---

## Task 1: Python harness generator

**Files:**
- Create: `claudetool/scripted_tools.go`
- Create: `claudetool/scripted_tools_test.go`

**Step 1: Write the test for harness generation**

Create `claudetool/scripted_tools_test.go`:

```go
package claudetool

import (
	"strings"
	"testing"

	"github.com/tgruben-circuit/percy/llm"
)

func TestGenerateHarness(t *testing.T) {
	tools := []*llm.Tool{
		{Name: "read_file"},
		{Name: "bash"},
		{Name: "patch"},
	}

	script := "content = await read_file({\"path\": \"foo.go\"})\nprint(content)"
	harness := generateHarness(tools, script)

	// Should have imports
	if !strings.Contains(harness, "import sys, json, asyncio") {
		t.Error("missing imports")
	}

	// Should have stub for each tool
	for _, name := range []string{"read_file", "bash", "patch"} {
		if !strings.Contains(harness, "async def "+name+"(") {
			t.Errorf("missing stub for %s", name)
		}
	}

	// Should redirect stdout to stderr
	if !strings.Contains(harness, "sys.stdout = sys.stderr") {
		t.Error("missing stdout redirect")
	}

	// Should contain the user script
	if !strings.Contains(harness, script) {
		t.Error("missing user script")
	}

	// Should wrap in async main
	if !strings.Contains(harness, "asyncio.run(_main())") {
		t.Error("missing asyncio.run")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestGenerateHarness -v`
Expected: FAIL — `generateHarness` not defined

**Step 3: Implement `generateHarness`**

In `claudetool/scripted_tools.go`:

```go
package claudetool

import (
	"fmt"
	"strings"

	"github.com/tgruben-circuit/percy/llm"
)

const scriptedToolsName = "scripted_tools"

// excludedFromScripting lists tools that cannot be called from scripted_tools.
var excludedFromScripting = map[string]bool{
	scriptedToolsName: true, // no recursion
	subagentName:      true, // too complex
	requestToolsName:  true, // meta-tool, not useful in scripts
}

// filterScriptableTools returns tools that can be exposed to Python scripts.
func filterScriptableTools(tools []*llm.Tool) []*llm.Tool {
	var result []*llm.Tool
	for _, t := range tools {
		if !excludedFromScripting[t.Name] {
			result = append(result, t)
		}
	}
	return result
}

// generateHarness creates the Python script that wraps the user's code with
// tool stubs and IPC plumbing.
func generateHarness(tools []*llm.Tool, userScript string) string {
	var b strings.Builder

	// Preamble: imports and IPC helper
	b.WriteString(`import sys, json, asyncio

_ipc_out = sys.stdout  # real stdout reserved for IPC

def _call_tool(name, input_dict):
    req = json.dumps({"tool": name, "input": input_dict})
    _ipc_out.write(req + "\n")
    _ipc_out.flush()
    line = sys.stdin.readline()
    if not line:
        raise RuntimeError("IPC channel closed")
    resp = json.loads(line)
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]

`)

	// Generate async stub for each tool
	for _, t := range tools {
		fmt.Fprintf(&b, "async def %s(input): return _call_tool(%q, input)\n", t.Name, t.Name)
	}

	// Redirect print() to stderr
	b.WriteString(`
# Redirect print() to stderr — only printed output becomes the tool result
sys.stdout = sys.stderr

async def _main():
`)

	// Indent user script under _main()
	for _, line := range strings.Split(userScript, "\n") {
		b.WriteString("    " + line + "\n")
	}

	b.WriteString("\nasyncio.run(_main())\n")

	return b.String()
}
```

**Step 4: Run test**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestGenerateHarness -v`
Expected: PASS

**Step 5: Commit**

```bash
git add claudetool/scripted_tools.go claudetool/scripted_tools_test.go
git commit -m "feat(scripted_tools): add Python harness generator"
```

---

## Task 2: IPC loop and script execution

**Files:**
- Modify: `claudetool/scripted_tools.go`
- Modify: `claudetool/scripted_tools_test.go`

**Step 1: Write test for basic script execution (no tool calls)**

```go
func TestScriptedTools_BasicPrint(t *testing.T) {
	st := &ScriptedToolsTool{Tools: nil}
	input, _ := json.Marshal(map[string]string{"script": "print('hello world')"})
	out := st.Run(context.Background(), input)
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected 'hello world' in output, got: %s", text)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestScriptedTools_BasicPrint -v`
Expected: FAIL — `ScriptedToolsTool` and `Run` not defined

**Step 3: Implement `ScriptedToolsTool` and `Run`**

Add to `claudetool/scripted_tools.go`:

```go
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tgruben-circuit/percy/llm"
)

const defaultScriptTimeout = 2 * time.Minute

// ScriptedToolsTool lets the LLM write Python scripts that call Percy's tools
// programmatically. Only print() output enters the conversation context.
type ScriptedToolsTool struct {
	Tools      []*llm.Tool
	WorkingDir *MutableWorkingDir
	Timeout    time.Duration // 0 means defaultScriptTimeout
}

type scriptedToolsInput struct {
	Script string `json:"script"`
}

type ipcRequest struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type ipcResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func (s *ScriptedToolsTool) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultScriptTimeout
}

func (s *ScriptedToolsTool) Run(ctx context.Context, m json.RawMessage) llm.ToolOut {
	var req scriptedToolsInput
	if err := json.Unmarshal(m, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse scripted_tools input: %v", err)
	}
	if req.Script == "" {
		return llm.ErrorfToolOut("script is required")
	}

	// Check uv is available
	if _, err := exec.LookPath("uv"); err != nil {
		return llm.ErrorfToolOut("uv not found in PATH")
	}

	// Filter tools and generate harness
	scriptable := filterScriptableTools(s.Tools)
	harness := generateHarness(scriptable, req.Script)

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "percy-scripted-*.py")
	if err != nil {
		return llm.ErrorfToolOut("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(harness); err != nil {
		tmpFile.Close()
		return llm.ErrorfToolOut("failed to write script: %v", err)
	}
	tmpFile.Close()

	// Run with timeout
	execCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	cmd := exec.CommandContext(execCtx, "uv", "run", "python", tmpFile.Name())
	if s.WorkingDir != nil {
		cmd.Dir = s.WorkingDir.Get()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	// Set up pipes
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return llm.ErrorfToolOut("failed to create stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return llm.ErrorfToolOut("failed to create stdout pipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return llm.ErrorfToolOut("failed to start python: %v", err)
	}

	// IPC loop: read tool call requests from stdout, fulfill them, write responses to stdin
	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		var ipcReq ipcRequest
		if err := json.Unmarshal(scanner.Bytes(), &ipcReq); err != nil {
			// Malformed IPC — kill and return
			cmd.Process.Kill()
			return llm.ErrorfToolOut("malformed IPC from script: %v\nstderr: %s", err, stderr.String())
		}

		// Find and run the tool
		resp := s.executeTool(ctx, ipcReq)

		respBytes, _ := json.Marshal(resp)
		respBytes = append(respBytes, '\n')
		if _, err := stdinPipe.Write(respBytes); err != nil {
			break // pipe closed, process exiting
		}
	}

	stdinPipe.Close()
	waitErr := cmd.Wait()

	output := stderr.String()

	if execCtx.Err() == context.DeadlineExceeded {
		return llm.ErrorfToolOut("script timed out after %s\noutput so far: %s", s.timeout(), output)
	}
	if waitErr != nil && output == "" {
		return llm.ErrorfToolOut("script failed: %v", waitErr)
	}

	if output == "" {
		output = "(script produced no output)"
	}

	return llm.ToolOut{LLMContent: llm.TextContent(strings.TrimSpace(output))}
}

// executeTool finds a tool by name and runs it, returning an IPC response.
func (s *ScriptedToolsTool) executeTool(ctx context.Context, req ipcRequest) ipcResponse {
	for _, t := range s.Tools {
		if t.Name == req.Tool {
			out := t.Run(ctx, req.Input)
			if out.Error != nil {
				return ipcResponse{Error: out.Error.Error()}
			}
			// Extract text from LLMContent
			var texts []string
			for _, c := range out.LLMContent {
				if c.Text != "" {
					texts = append(texts, c.Text)
				}
			}
			return ipcResponse{Result: strings.Join(texts, "\n")}
		}
	}
	return ipcResponse{Error: fmt.Sprintf("unknown tool: %s", req.Tool)}
}
```

**Step 4: Run test**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestScriptedTools_BasicPrint -v`
Expected: PASS

**Step 5: Commit**

```bash
git add claudetool/scripted_tools.go claudetool/scripted_tools_test.go
git commit -m "feat(scripted_tools): add IPC loop and script execution"
```

---

## Task 3: Tool call IPC tests

**Files:**
- Modify: `claudetool/scripted_tools_test.go`

**Step 1: Write test for single tool call**

```go
func TestScriptedTools_SingleToolCall(t *testing.T) {
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{LLMContent: llm.TextContent("file contents here")}
		},
	}

	st := &ScriptedToolsTool{Tools: []*llm.Tool{mockTool}}

	script := `result = await read_file({"path": "test.go"})
print(f"got: {result}")`
	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "got: file contents here") {
		t.Errorf("expected tool result in output, got: %s", text)
	}
}
```

**Step 2: Write test for multiple tool calls in a loop**

```go
func TestScriptedTools_MultipleToolCalls(t *testing.T) {
	callCount := 0
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			callCount++
			var req map[string]string
			json.Unmarshal(input, &req)
			return llm.ToolOut{LLMContent: llm.TextContent(fmt.Sprintf("content of %s", req["path"]))}
		},
	}

	st := &ScriptedToolsTool{Tools: []*llm.Tool{mockTool}}

	script := `paths = ["a.go", "b.go", "c.go"]
results = []
for p in paths:
    content = await read_file({"path": p})
    results.append(content)
print(f"Read {len(results)} files")`

	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}
	if callCount != 3 {
		t.Errorf("expected 3 tool calls, got %d", callCount)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "Read 3 files") {
		t.Errorf("expected 'Read 3 files' in output, got: %s", text)
	}
}
```

**Step 3: Write test for tool error handling**

```go
func TestScriptedTools_ToolError(t *testing.T) {
	mockTool := &llm.Tool{
		Name: "read_file",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ErrorfToolOut("file not found")
		},
	}

	st := &ScriptedToolsTool{Tools: []*llm.Tool{mockTool}}

	// Script does NOT catch the error, so it should propagate as a traceback
	script := `result = await read_file({"path": "missing.go"})
print(result)`

	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	// The script crashes, so we get the traceback
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "file not found") {
		t.Errorf("expected error message in output, got: %s", text)
	}
}
```

**Step 4: Write test for unknown tool**

```go
func TestScriptedTools_UnknownTool(t *testing.T) {
	st := &ScriptedToolsTool{Tools: nil}

	script := `result = await nonexistent_tool({"key": "value"})
print(result)`

	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	// Python will get a NameError since there's no stub for nonexistent_tool
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "NameError") && !strings.Contains(text, "not defined") {
		t.Errorf("expected NameError in output, got: %s", text)
	}
}
```

**Step 5: Write test for script syntax error**

```go
func TestScriptedTools_SyntaxError(t *testing.T) {
	st := &ScriptedToolsTool{Tools: nil}

	script := `def broken(
print("nope")`

	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "SyntaxError") {
		t.Errorf("expected SyntaxError in output, got: %s", text)
	}
}
```

**Step 6: Write test for timeout**

```go
func TestScriptedTools_Timeout(t *testing.T) {
	st := &ScriptedToolsTool{
		Tools:   nil,
		Timeout: 2 * time.Second,
	}

	script := `import time
time.sleep(30)`

	input, _ := json.Marshal(map[string]string{"script": script})
	out := st.Run(context.Background(), input)
	if out.Error == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(out.Error.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", out.Error)
	}
}
```

**Step 7: Run all tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestScriptedTools -v -count=1`
Expected: all PASS

**Step 8: Commit**

```bash
git add claudetool/scripted_tools_test.go
git commit -m "test(scripted_tools): add IPC and error handling tests"
```

---

## Task 4: Tool definition and description

**Files:**
- Modify: `claudetool/scripted_tools.go`
- Modify: `claudetool/scripted_tools_test.go`

**Step 1: Write test for Tool() method**

```go
func TestScriptedTools_ToolDefinition(t *testing.T) {
	mockTools := []*llm.Tool{
		{Name: "read_file"},
		{Name: "bash"},
		{Name: "scripted_tools"}, // should be excluded
		{Name: "subagent"},       // should be excluded
	}

	st := &ScriptedToolsTool{Tools: mockTools}
	tool := st.Tool()

	if tool.Name != "scripted_tools" {
		t.Errorf("expected name 'scripted_tools', got %s", tool.Name)
	}

	// Description should list available tools
	if !strings.Contains(tool.Description, "read_file") {
		t.Error("description should list read_file")
	}
	if !strings.Contains(tool.Description, "bash") {
		t.Error("description should list bash")
	}
	// Excluded tools should not appear
	if strings.Contains(tool.Description, "subagent") {
		t.Error("description should not list subagent")
	}
}
```

**Step 2: Implement Tool() method**

Add to `claudetool/scripted_tools.go`:

```go
const scriptedToolsInputSchema = `{
	"type": "object",
	"required": ["script"],
	"properties": {
		"script": {
			"type": "string",
			"description": "Python script to execute. Tool functions are pre-defined as async functions. Use print() for output — only printed text is returned to the conversation."
		}
	}
}`

func (s *ScriptedToolsTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        scriptedToolsName,
		Description: s.description(),
		InputSchema: llm.MustSchema(scriptedToolsInputSchema),
		Run:         s.Run,
	}
}

func (s *ScriptedToolsTool) description() string {
	scriptable := filterScriptableTools(s.Tools)
	var names []string
	for _, t := range scriptable {
		names = append(names, t.Name)
	}

	return fmt.Sprintf(`Execute a Python script that can call other tools programmatically.
Tool results stay in the script — only your print() output enters the conversation.
Use this when you need to call multiple tools and process/filter/aggregate results before reporting.

Available tools: %s

Each tool is an async function matching its normal input schema. Example:
  content = await read_file({"path": "foo.go"})
  result = await bash({"command": "go test ./..."})

Use print() for output — only printed text is returned.`, strings.Join(names, ", "))
}
```

**Step 3: Run test**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestScriptedTools_ToolDefinition -v`
Expected: PASS

**Step 4: Commit**

```bash
git add claudetool/scripted_tools.go claudetool/scripted_tools_test.go
git commit -m "feat(scripted_tools): add tool definition and dynamic description"
```

---

## Task 5: Wire into ToolSet

**Files:**
- Modify: `claudetool/toolset.go`
- Modify: `claudetool/toolset_test.go`

**Step 1: Write test**

Add to `claudetool/toolset_test.go`:

```go
func TestToolSet_HasScriptedTools(t *testing.T) {
	cfg := ToolSetConfig{
		WorkingDir: "/tmp",
		ModelID:    "test-model",
	}

	ts := NewToolSet(context.Background(), cfg)

	found := false
	for _, tool := range ts.AllTools() {
		if tool.Name == "scripted_tools" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected scripted_tools in tool set")
	}
}
```

**Step 2: Modify `NewToolSet` in `claudetool/toolset.go`**

After the existing tool registrations (around line 197, after memory search), add:

```go
// Register scripted_tools — passes all current tools for programmatic calling
scriptedTool := &ScriptedToolsTool{
	Tools:      tools, // will be filtered in Run() to exclude self/subagent
	WorkingDir: wd,
}
tools = append(tools, scriptedTool.Tool())
```

Note: `ScriptedToolsTool` receives the full tool list, and `filterScriptableTools` handles exclusion at execution time. This means tools added later (browser, LSP, cluster, request_tools) are NOT in the scripted tools list — which is correct since they're deferred and may not be active. If we want scripted_tools to see deferred tools after activation, we can pass `tools` by reference later. For now, core tools only.

**Step 3: Run test**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestToolSet_HasScriptedTools -v`
Expected: PASS

**Step 4: Run full claudetool tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add claudetool/toolset.go claudetool/toolset_test.go
git commit -m "feat(scripted_tools): wire into ToolSet"
```

---

## Task 6: Integration test — tool results stay out of context

**Files:**
- Create: `loop/scripted_tools_test.go`

**Step 1: Write integration test**

This test verifies the key property: when the LLM uses `scripted_tools`, intermediate tool results do NOT appear in recorded messages — only the final print output.

```go
package loop

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/tgruben-circuit/percy/llm"
)

func TestScriptedTools_IntermediateResultsNotInContext(t *testing.T) {
	// A mock LLM that returns a scripted_tools call on the first turn,
	// then ends on the second turn.
	callCount := 0
	var mu sync.Mutex

	mockLLM := &funcLLM{
		doFunc: func(ctx context.Context, req *llm.Request) (*llm.Response, error) {
			mu.Lock()
			callCount++
			count := callCount
			mu.Unlock()

			if count == 1 {
				// First call: LLM requests scripted_tools
				return &llm.Response{
					Content: []llm.Content{{
						Type:    llm.ContentTypeToolUse,
						ID:      "tool_1",
						ToolUse: &llm.ToolUseContent{Name: "scripted_tools", Input: json.RawMessage(`{"script": "print('summary result')"}`),
						},
					}},
					StopReason: llm.StopReasonToolUse,
				}, nil
			}
			// Second call: LLM ends turn
			return &llm.Response{
				Content:    []llm.Content{{Type: llm.ContentTypeText, Text: "done"}},
				StopReason: llm.StopReasonEndTurn,
			}, nil
		},
	}

	var recorded []llm.Message
	var recordMu sync.Mutex

	scriptedTool := &llm.Tool{
		Name:        "scripted_tools",
		InputSchema: llm.MustSchema(`{"type":"object","properties":{"script":{"type":"string"}},"required":["script"]}`),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			// Simulate: this is the real scripted_tools Run that
			// internally called 5 tools but returns only the summary.
			return llm.ToolOut{LLMContent: llm.TextContent("summary result")}
		},
	}

	l := NewLoop(Config{
		LLM:   mockLLM,
		Tools: []*llm.Tool{scriptedTool},
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recordMu.Lock()
			recorded = append(recorded, msg)
			recordMu.Unlock()
			return nil
		},
	})

	l.QueueUserMessage(llm.UserStringMessage("analyze files"))
	err := l.ProcessOneTurn(context.Background())
	if err != nil {
		t.Fatalf("ProcessOneTurn failed: %v", err)
	}

	// Check recorded messages: should have tool result with "summary result"
	// but NO individual read_file results (those stayed inside the script)
	foundSummary := false
	for _, msg := range recorded {
		for _, c := range msg.Content {
			if c.Text == "summary result" {
				foundSummary = true
			}
		}
	}
	if !foundSummary {
		t.Error("expected 'summary result' in recorded messages")
	}
}
```

Note: This test may need adjustment based on the exact mock patterns used in `loop/`. Check `loop/loop_test.go` and `loop/deferred_test.go` for the existing mock LLM pattern and reuse it.

**Step 2: Run test**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./loop/ -run TestScriptedTools -v -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add loop/scripted_tools_test.go
git commit -m "test(scripted_tools): integration test verifying results stay out of context"
```

---

## Task 7: Build and run full test suite

**Step 1: Build UI**

Run: `cd /Users/toddgruben/Projects/shelley && make ui`
Expected: builds successfully

**Step 2: Run Go tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ ./loop/ -v -count=1`
Expected: all PASS

**Step 3: Run full test suite**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./... -count=1`
Expected: all PASS

**Step 4: Commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address test suite issues for scripted_tools"
```

---

## Summary

| Task | What | Files |
|------|-------|-------|
| 1 | Python harness generator | `claudetool/scripted_tools.go`, `_test.go` |
| 2 | IPC loop and script execution | `claudetool/scripted_tools.go`, `_test.go` |
| 3 | Tool call IPC tests | `claudetool/scripted_tools_test.go` |
| 4 | Tool definition and description | `claudetool/scripted_tools.go`, `_test.go` |
| 5 | Wire into ToolSet | `claudetool/toolset.go`, `_test.go` |
| 6 | Integration test | `loop/scripted_tools_test.go` |
| 7 | Full test suite verification | — |
