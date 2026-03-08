package claudetool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
