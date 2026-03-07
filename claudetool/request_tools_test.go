package claudetool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tgruben-circuit/percy/llm"
)

func testDeferredTools() []*llm.Tool {
	return []*llm.Tool{
		{Name: "browser_navigate", Deferred: true, Category: "browser"},
		{Name: "browser_click", Deferred: true, Category: "browser"},
		{Name: "lsp_symbols", Deferred: true, Category: "lsp"},
	}
}

func TestRequestToolsTool_ActivateCategory(t *testing.T) {
	rt := NewRequestToolsTool(testDeferredTools())

	if rt.IsCategoryActive("browser") {
		t.Fatal("browser should not be active initially")
	}

	out := rt.Run(context.Background(), mustJSON(t, requestToolsInput{Category: "browser"}))
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}

	if !rt.IsCategoryActive("browser") {
		t.Fatal("browser should be active after activation")
	}
	if rt.IsCategoryActive("lsp") {
		t.Fatal("lsp should not be active")
	}
	if rt.AllActivated() {
		t.Fatal("not all categories should be activated")
	}
}

func TestRequestToolsTool_ActiveTools(t *testing.T) {
	deferred := testDeferredTools()
	rt := NewRequestToolsTool(deferred)

	coreTool := &llm.Tool{Name: "bash"}
	requestTool := rt.Tool()

	all := append([]*llm.Tool{coreTool, requestTool}, deferred...)

	// Before activation: only core + request_tools.
	active := rt.FilterActiveTools(all)
	if len(active) != 2 {
		t.Fatalf("expected 2 active tools, got %d: %v", len(active), toolNames(active))
	}

	// Activate browser.
	rt.Run(context.Background(), mustJSON(t, requestToolsInput{Category: "browser"}))
	active = rt.FilterActiveTools(all)
	// core + request_tools + 2 browser tools = 4
	if len(active) != 4 {
		t.Fatalf("expected 4 active tools, got %d: %v", len(active), toolNames(active))
	}

	// Activate lsp — all activated, request_tools excluded.
	rt.Run(context.Background(), mustJSON(t, requestToolsInput{Category: "lsp"}))
	active = rt.FilterActiveTools(all)
	// core + 2 browser + 1 lsp = 4 (request_tools excluded)
	if len(active) != 4 {
		t.Fatalf("expected 4 active tools (no request_tools), got %d: %v", len(active), toolNames(active))
	}
	for _, tool := range active {
		if tool.Name == requestToolsName {
			t.Fatal("request_tools should be excluded when all categories are activated")
		}
	}
}

func TestRequestToolsTool_UnknownCategory(t *testing.T) {
	rt := NewRequestToolsTool(testDeferredTools())

	out := rt.Run(context.Background(), mustJSON(t, requestToolsInput{Category: "nonexistent"}))
	if out.Error == nil {
		t.Fatal("expected error for unknown category")
	}
	if !strings.Contains(out.Error.Error(), "unknown category") {
		t.Fatalf("expected 'unknown category' error, got: %v", out.Error)
	}
}

func TestRequestToolsTool_DescriptionUpdates(t *testing.T) {
	rt := NewRequestToolsTool(testDeferredTools())

	desc := rt.Tool().Description
	if !strings.Contains(desc, "browser") || !strings.Contains(desc, "lsp") {
		t.Fatalf("description should mention both categories, got: %s", desc)
	}

	// Activate browser — description should no longer mention it.
	rt.Run(context.Background(), mustJSON(t, requestToolsInput{Category: "browser"}))
	desc = rt.Tool().Description
	if strings.Contains(desc, "browser") {
		t.Fatalf("description should not mention activated category 'browser', got: %s", desc)
	}
	if !strings.Contains(desc, "lsp") {
		t.Fatalf("description should still mention 'lsp', got: %s", desc)
	}
}

func TestRequestToolsTool_AlreadyActivated(t *testing.T) {
	rt := NewRequestToolsTool(testDeferredTools())

	input := mustJSON(t, requestToolsInput{Category: "browser"})
	rt.Run(context.Background(), input)

	out := rt.Run(context.Background(), input)
	if out.Error != nil {
		t.Fatalf("re-activation should not error, got: %v", out.Error)
	}
	text := out.LLMContent[0].Text
	if !strings.Contains(text, "already active") {
		t.Fatalf("expected 'already active' message, got: %s", text)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func toolNames(tools []*llm.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
