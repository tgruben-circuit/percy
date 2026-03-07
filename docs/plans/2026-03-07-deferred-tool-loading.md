# Deferred Tool Loading Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce context window usage by only sending core tools to the LLM on every turn, and loading situational tools on-demand when the LLM requests them.

**Architecture:** Add a `Deferred` flag to `llm.Tool`. The `ToolSet` categorizes tools as core (always sent) or deferred (loaded on demand). A new `request_tools` meta-tool lets the LLM discover and activate deferred tools by category. The loop filters deferred tools out of requests until they're activated. This is entirely server-side — works with any LLM provider.

**Tech Stack:** Go (loop, claudetool, llm packages). No new dependencies.

---

## Design

### Tool Categories

| Category | Tools | Rationale |
|----------|-------|-----------|
| **Core** (always loaded) | `bash`, `patch`, `read_file`, `change_dir`, `keyword_search`, `subagent`, `todo_write`, `skill_load`, `memory_search` | Used nearly every turn or very small schema |
| **Browser** (deferred) | `browser_navigate`, `browser_resize`, `browser_eval`, `browser_take_screenshot`, `read_image`, `browser_recent_console_logs`, `browser_clear_console_logs` | 7 tools, large combined schema, only used for web tasks |
| **LSP** (deferred) | `code_intelligence` | Specialized, not always needed |
| **Cluster** (deferred) | `dispatch_tasks` | Only relevant in cluster mode |
| **Output** (deferred) | `output_iframe` | Only for visualization tasks |

### How It Works

1. `llm.Tool` gets a `Deferred bool` field and a `Category string` field
2. `ToolSet` stores all tools but exposes `ActiveTools()` (core only initially) and `AllTools()` (everything)
3. A `request_tools` meta-tool is always in the core set. When the LLM calls it with a category name, the ToolSet activates those tools
4. `loop.go` calls `ActiveTools()` instead of `Tools()` when building requests, but uses `AllTools()` for tool execution (so activated tools can be called immediately)
5. Once activated, tools stay active for the rest of the conversation

### The `request_tools` Tool

```
Name: "request_tools"
Description: "Load additional tools by category. Available categories are listed below.
  Call this before using specialized tools like browser automation or code intelligence.
  Returns the names of newly activated tools."
Schema: { "category": string (required) }
```

The description dynamically includes available (not-yet-activated) categories with their tool names.

---

## Tasks

### Task 1: Add `Deferred` and `Category` fields to `llm.Tool`

**Files:**
- Modify: `llm/llm.go` (Tool struct, ~line 114)

**Step 1: Add fields to Tool struct**

In `llm/llm.go`, add two fields to the `Tool` struct:

```go
type Tool struct {
	Name string
	Type        string
	Description string
	InputSchema json.RawMessage
	EndsTurn bool
	Cache bool

	// Deferred indicates this tool should not be sent to the LLM until explicitly activated.
	// Deferred tools are loaded on-demand via the request_tools meta-tool.
	Deferred bool
	// Category groups deferred tools for activation. Tools with the same category
	// are activated together. Empty category means the tool is always active (core).
	Category string

	Run func(ctx context.Context, input json.RawMessage) ToolOut `json:"-"`
}
```

**Step 2: Run existing tests**

Run: `cd /Users/toddgruben/Projects/shelley && go build ./...`
Expected: compiles cleanly, no behavior change (zero values preserve existing behavior)

**Step 3: Commit**

```bash
git add llm/llm.go
git commit -m "feat: add Deferred and Category fields to llm.Tool"
```

---

### Task 2: Create the `request_tools` meta-tool

**Files:**
- Create: `claudetool/request_tools.go`
- Create: `claudetool/request_tools_test.go`

**Step 1: Write the failing test**

Create `claudetool/request_tools_test.go`:

```go
package claudetool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tgruben-circuit/percy/llm"
)

func TestRequestToolsTool_ActivateCategory(t *testing.T) {
	browserTool := &llm.Tool{Name: "browser_navigate", Deferred: true, Category: "browser"}
	lspTool := &llm.Tool{Name: "code_intelligence", Deferred: true, Category: "lsp"}

	rt := &RequestToolsTool{
		deferredTools: []*llm.Tool{browserTool, lspTool},
		activated:     make(map[string]bool),
	}

	// Activate browser category
	input, _ := json.Marshal(map[string]string{"category": "browser"})
	out := rt.Run(context.Background(), input)
	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error)
	}

	if !rt.IsCategoryActive("browser") {
		t.Error("browser category should be active")
	}
	if rt.IsCategoryActive("lsp") {
		t.Error("lsp category should not be active")
	}
}

func TestRequestToolsTool_ActiveTools(t *testing.T) {
	coreTool := &llm.Tool{Name: "bash"}
	browserTool := &llm.Tool{Name: "browser_navigate", Deferred: true, Category: "browser"}
	lspTool := &llm.Tool{Name: "code_intelligence", Deferred: true, Category: "lsp"}

	rt := &RequestToolsTool{
		deferredTools: []*llm.Tool{browserTool, lspTool},
		activated:     make(map[string]bool),
	}

	allTools := []*llm.Tool{coreTool, browserTool, lspTool, rt.Tool()}

	// Before activation: only core + request_tools
	active := rt.FilterActiveTools(allTools)
	if len(active) != 2 { // bash + request_tools
		t.Errorf("expected 2 active tools, got %d", len(active))
	}

	// Activate browser
	rt.activated["browser"] = true
	active = rt.FilterActiveTools(allTools)
	if len(active) != 3 { // bash + browser_navigate + request_tools
		t.Errorf("expected 3 active tools, got %d", len(active))
	}
}

func TestRequestToolsTool_UnknownCategory(t *testing.T) {
	rt := &RequestToolsTool{
		deferredTools: []*llm.Tool{},
		activated:     make(map[string]bool),
	}

	input, _ := json.Marshal(map[string]string{"category": "nonexistent"})
	out := rt.Run(context.Background(), input)
	if out.Error == nil {
		t.Error("expected error for unknown category")
	}
}

func TestRequestToolsTool_DescriptionUpdates(t *testing.T) {
	browserTool := &llm.Tool{Name: "browser_navigate", Deferred: true, Category: "browser"}
	lspTool := &llm.Tool{Name: "code_intelligence", Deferred: true, Category: "lsp"}

	rt := &RequestToolsTool{
		deferredTools: []*llm.Tool{browserTool, lspTool},
		activated:     make(map[string]bool),
	}

	tool := rt.Tool()
	// Description should mention both categories
	if tool.Description == "" {
		t.Error("expected non-empty description")
	}

	// After activating all categories, request_tools should be excluded from active tools
	rt.activated["browser"] = true
	rt.activated["lsp"] = true
	active := rt.FilterActiveTools([]*llm.Tool{browserTool, lspTool, tool})
	// All deferred now active, request_tools should be excluded
	for _, t2 := range active {
		if t2.Name == requestToolsName {
			t.Error("request_tools should be excluded when all categories are activated")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestRequestTools -v`
Expected: FAIL — types don't exist yet

**Step 3: Write the implementation**

Create `claudetool/request_tools.go`:

```go
package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tgruben-circuit/percy/llm"
)

const requestToolsName = "request_tools"

// RequestToolsTool is a meta-tool that activates deferred tool categories on demand.
type RequestToolsTool struct {
	mu            sync.RWMutex
	deferredTools []*llm.Tool
	activated     map[string]bool // category -> activated
}

// NewRequestToolsTool creates a new RequestToolsTool for the given deferred tools.
func NewRequestToolsTool(deferred []*llm.Tool) *RequestToolsTool {
	return &RequestToolsTool{
		deferredTools: deferred,
		activated:     make(map[string]bool),
	}
}

type requestToolsInput struct {
	Category string `json:"category"`
}

// Tool returns the llm.Tool definition. The description dynamically lists available categories.
func (rt *RequestToolsTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        requestToolsName,
		Description: rt.description(),
		InputSchema: llm.MustSchema(requestToolsInputSchema),
		Run:         rt.Run,
	}
}

func (rt *RequestToolsTool) description() string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	categories := rt.availableCategories()
	if len(categories) == 0 {
		return "Load additional tools by category. No deferred tool categories are currently available."
	}

	var lines []string
	for _, cat := range categories {
		var names []string
		for _, t := range rt.deferredTools {
			if t.Category == cat {
				names = append(names, t.Name)
			}
		}
		lines = append(lines, fmt.Sprintf("  - %s: %s", cat, strings.Join(names, ", ")))
	}

	return fmt.Sprintf(`Load additional tools by category. Call this before using specialized tools.
Available categories:
%s

Returns the names of newly activated tools.`, strings.Join(lines, "\n"))
}

const requestToolsInputSchema = `{
  "type": "object",
  "required": ["category"],
  "properties": {
    "category": {
      "type": "string",
      "description": "The tool category to activate"
    }
  }
}`

// Run activates a deferred tool category.
func (rt *RequestToolsTool) Run(ctx context.Context, input json.RawMessage) llm.ToolOut {
	var req requestToolsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse request_tools input: %w", err)
	}

	if req.Category == "" {
		return llm.ErrorfToolOut("category is required")
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Check category exists
	var toolNames []string
	for _, t := range rt.deferredTools {
		if t.Category == req.Category {
			toolNames = append(toolNames, t.Name)
		}
	}
	if len(toolNames) == 0 {
		avail := rt.availableCategories()
		return llm.ErrorfToolOut("unknown category %q. Available: %s", req.Category, strings.Join(avail, ", "))
	}

	if rt.activated[req.Category] {
		return llm.ToolOut{
			LLMContent: llm.TextContent(fmt.Sprintf("Category %q is already active. Tools: %s", req.Category, strings.Join(toolNames, ", "))),
		}
	}

	rt.activated[req.Category] = true
	return llm.ToolOut{
		LLMContent: llm.TextContent(fmt.Sprintf("Activated %d tools in category %q: %s. These tools are now available.", len(toolNames), req.Category, strings.Join(toolNames, ", "))),
	}
}

// IsCategoryActive reports whether a category has been activated.
func (rt *RequestToolsTool) IsCategoryActive(category string) bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.activated[category]
}

// AllActivated reports whether all deferred categories have been activated.
func (rt *RequestToolsTool) AllActivated() bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	for _, cat := range rt.availableCategories() {
		if !rt.activated[cat] {
			return false
		}
	}
	return true
}

// FilterActiveTools returns only the tools that should be sent to the LLM:
// non-deferred tools, plus deferred tools whose category has been activated.
// If all categories are activated, the request_tools tool itself is excluded.
func (rt *RequestToolsTool) FilterActiveTools(tools []*llm.Tool) []*llm.Tool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	allActive := true
	for _, cat := range rt.availableCategories() {
		if !rt.activated[cat] {
			allActive = false
			break
		}
	}

	var active []*llm.Tool
	for _, t := range tools {
		if t.Name == requestToolsName {
			if !allActive {
				active = append(active, t)
			}
			continue
		}
		if !t.Deferred || rt.activated[t.Category] {
			active = append(active, t)
		}
	}
	return active
}

// HasDeferredTools reports whether there are any deferred tools.
func (rt *RequestToolsTool) HasDeferredTools() bool {
	return len(rt.deferredTools) > 0
}

// availableCategories returns sorted unique categories from deferred tools.
// Must be called with rt.mu held (read or write).
func (rt *RequestToolsTool) availableCategories() []string {
	seen := make(map[string]bool)
	var cats []string
	for _, t := range rt.deferredTools {
		if t.Category != "" && !seen[t.Category] {
			seen[t.Category] = true
			cats = append(cats, t.Category)
		}
	}
	sort.Strings(cats)
	return cats
}
```

**Step 4: Run tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestRequestTools -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add claudetool/request_tools.go claudetool/request_tools_test.go
git commit -m "feat: add request_tools meta-tool for deferred tool activation"
```

---

### Task 3: Wire deferred tools into `ToolSet`

**Files:**
- Modify: `claudetool/toolset.go`
- Modify: `claudetool/toolset_test.go`

**Step 1: Write failing tests**

Add to `claudetool/toolset_test.go`:

```go
func TestToolSet_DeferredTools(t *testing.T) {
	cfg := ToolSetConfig{
		LLMProvider:            &mockLLMProvider{},
		ModelID:                "test-model",
		WorkingDir:             "/test",
		EnableBrowser:          true,
		EnableCodeIntelligence: true,
	}

	ts := NewToolSet(context.Background(), cfg)

	// ActiveTools should be fewer than AllTools
	active := ts.ActiveTools()
	all := ts.AllTools()

	if len(active) >= len(all) {
		t.Errorf("expected active tools (%d) < all tools (%d)", len(active), len(all))
	}

	// ActiveTools should include request_tools
	found := false
	for _, tool := range active {
		if tool.Name == requestToolsName {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected request_tools in active tools")
	}
}

func TestToolSet_NoDeferredWithoutOptionalTools(t *testing.T) {
	cfg := ToolSetConfig{
		LLMProvider: &mockLLMProvider{},
		ModelID:     "test-model",
		WorkingDir:  "/test",
		// No browser, no LSP, no cluster
	}

	ts := NewToolSet(context.Background(), cfg)

	// No deferred tools, so ActiveTools == AllTools (no request_tools added)
	active := ts.ActiveTools()
	all := ts.AllTools()
	if len(active) != len(all) {
		t.Errorf("expected active (%d) == all (%d) when no deferred tools", len(active), len(all))
	}

	// request_tools should NOT be present
	for _, tool := range active {
		if tool.Name == requestToolsName {
			t.Error("request_tools should not be present when no deferred tools exist")
		}
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -run TestToolSet_Deferred -v`
Expected: FAIL — `ActiveTools` and `AllTools` don't exist

**Step 3: Modify `toolset.go`**

Changes:
1. Add `requestTools *RequestToolsTool` to `ToolSet`
2. Mark browser, LSP, cluster, and output_iframe tools as `Deferred` with `Category`
3. Add `ActiveTools()` method that filters via `RequestToolsTool.FilterActiveTools`
4. Add `AllTools()` method (returns all tools for execution lookup)
5. Rename existing `Tools()` to `AllTools()` and add `ActiveTools()`
6. Only add the `request_tools` meta-tool if there are deferred tools

In `NewToolSet`, after creating all tools:

```go
// Mark browser tools as deferred
if cfg.EnableBrowser {
	browserTools, browserCleanup := browse.RegisterBrowserTools(ctx, true, maxImageDimension)
	for _, bt := range browserTools {
		bt.Deferred = true
		bt.Category = "browser"
	}
	tools = append(tools, browserTools...)
	cleanups = append(cleanups, browserCleanup)
}

// Mark LSP tools as deferred
if cfg.EnableCodeIntelligence {
	lspTools, lspCleanup := lsp.RegisterLSPTools(wd.Get)
	for _, lt := range lspTools {
		lt.Deferred = true
		lt.Category = "lsp"
	}
	tools = append(tools, lspTools...)
	cleanups = append(cleanups, lspCleanup)
}

// Mark output_iframe as deferred
outputIframeTool.Tool().Deferred = true  // actually, set on the tool returned
outputIframeTool.Tool().Category = "output"
```

Actually — cleaner approach. Set `Deferred`/`Category` on the `*llm.Tool` after constructing them:

```go
// Collect deferred tools
var deferredTools []*llm.Tool
for _, t := range tools {
	if t.Deferred {
		deferredTools = append(deferredTools, t)
	}
}

// Only add request_tools if there are deferred tools
var reqTools *RequestToolsTool
if len(deferredTools) > 0 {
	reqTools = NewRequestToolsTool(deferredTools)
	tools = append(tools, reqTools.Tool())
}

return &ToolSet{
	tools:        tools,
	cleanup:      cleanup,
	wd:           wd,
	requestTools: reqTools,
}
```

And the new methods:

```go
// AllTools returns all tools (for tool execution lookup).
func (ts *ToolSet) AllTools() []*llm.Tool {
	return ts.tools
}

// ActiveTools returns only the tools that should be sent to the LLM.
// Deferred tools are excluded until their category is activated via request_tools.
func (ts *ToolSet) ActiveTools() []*llm.Tool {
	if ts.requestTools == nil {
		return ts.tools
	}
	return ts.requestTools.FilterActiveTools(ts.tools)
}
```

**Step 4: Update existing callers of `Tools()`**

The old `Tools()` method needs to stay as an alias for `AllTools()` temporarily, but we should just rename it. The callers:
- `server/convo.go:485` — uses `toolSet.Tools()` for `loop.Config.Tools`

This needs to change: the loop needs both `AllTools` (for execution) and `ActiveTools` (for sending to LLM). This is handled in Task 4.

For now, keep `Tools()` returning `AllTools()` so nothing breaks.

**Step 5: Run tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./claudetool/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add claudetool/toolset.go claudetool/toolset_test.go
git commit -m "feat: wire deferred tool loading into ToolSet"
```

---

### Task 4: Update the loop to use active vs all tools

**Files:**
- Modify: `loop/loop.go`
- Modify: `loop/loop_test.go`

The loop currently has a single `tools []*llm.Tool` field. We need:
- `tools []*llm.Tool` — all tools (for execution/lookup in `handleToolCalls`)
- `activeToolsFn func() []*llm.Tool` — returns current active tools (for LLM requests)

**Step 1: Write failing test**

Add to `loop/loop_test.go`:

```go
func TestLoopDeferredTools(t *testing.T) {
	// Create a deferred tool
	deferredTool := &llm.Tool{
		Name:        "deferred_test",
		Description: "A deferred test tool",
		InputSchema: llm.EmptySchema(),
		Deferred:    true,
		Category:    "test",
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{LLMContent: llm.TextContent("deferred tool result")}
		},
	}

	coreTool := &llm.Tool{
		Name:        "core_test",
		Description: "A core test tool",
		InputSchema: llm.EmptySchema(),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			return llm.ToolOut{LLMContent: llm.TextContent("core tool result")}
		},
	}

	allTools := []*llm.Tool{coreTool, deferredTool}

	// The activeToolsFn should only return non-deferred tools initially
	// (In real usage, ToolSet.ActiveTools() handles this)
	activeFn := func() []*llm.Tool {
		var active []*llm.Tool
		for _, t := range allTools {
			if !t.Deferred {
				active = append(active, t)
			}
		}
		return active
	}

	var recordedMessages []llm.Message

	l := NewLoop(Config{
		LLM: &mockLLM{
			response: &llm.Response{
				Content:    []llm.Content{{Type: llm.ContentTypeText, Text: "hello"}},
				StopReason: llm.StopReasonEndTurn,
			},
		},
		History: nil,
		Tools:   allTools,
		ActiveToolsFn: activeFn,
		RecordMessage: func(ctx context.Context, msg llm.Message, usage llm.Usage) error {
			recordedMessages = append(recordedMessages, msg)
			return nil
		},
		System: nil,
	})

	l.QueueUserMessage(llm.UserStringMessage("hi"))

	// The mock LLM should have received only core tools, not deferred ones.
	// We verify this by checking what the mock captured.
	// (This requires updating the mock to capture the request — see implementation.)
}
```

Actually, simpler approach — just verify through the mock that the LLM request had the right number of tools.

**Step 2: Modify `loop.Config` and `loop.Loop`**

In `loop/loop.go`:

```go
type Config struct {
	LLM              llm.Service
	History          []llm.Message
	Tools            []*llm.Tool           // All tools (for execution)
	ActiveToolsFn    func() []*llm.Tool    // Returns tools to send to LLM (nil = use Tools)
	RecordMessage    MessageRecordFunc
	Logger           *slog.Logger
	System           []llm.SystemContent
	WorkingDir       string
	OnGitStateChange GitStateChangeFunc
	GetWorkingDir    func() string
}
```

In `NewLoop`:
```go
activeToolsFn := config.ActiveToolsFn
if activeToolsFn == nil {
	// Default: all tools are always active
	allTools := config.Tools
	activeToolsFn = func() []*llm.Tool { return allTools }
}
return &Loop{
	...
	tools:         config.Tools,
	activeToolsFn: activeToolsFn,
	...
}
```

In `processLLMRequest`, change:
```go
// Before:
tools := l.tools

// After:
tools := l.activeToolsFn()
```

In `handleToolCalls`, keep using `l.tools` (all tools) for execution lookup:
```go
// This already uses l.tools for finding tools by name — no change needed.
for _, t := range l.tools {
	if t.Name == c.ToolName {
```

**Step 3: Run tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./loop/ -v -count=1`
Expected: PASS (nil ActiveToolsFn defaults to all tools)

**Step 4: Commit**

```bash
git add loop/loop.go loop/loop_test.go
git commit -m "feat: loop supports ActiveToolsFn for deferred tool filtering"
```

---

### Task 5: Wire it all together in `server/convo.go`

**Files:**
- Modify: `server/convo.go`

**Step 1: Update `ensureLoop`**

In `server/convo.go`, change the loop creation (~line 480-485):

```go
// Before:
loopInstance := loop.NewLoop(loop.Config{
	LLM:           service,
	History:       history,
	Tools:         toolSet.Tools(),
	...
})

// After:
loopInstance := loop.NewLoop(loop.Config{
	LLM:           service,
	History:       history,
	Tools:         toolSet.AllTools(),
	ActiveToolsFn: toolSet.ActiveTools,
	...
})
```

**Step 2: Build and verify**

Run: `cd /Users/toddgruben/Projects/shelley && go build ./...`
Expected: compiles

Run: `cd /Users/toddgruben/Projects/shelley && go test ./server/ -v -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add server/convo.go
git commit -m "feat: wire deferred tool loading into conversation loop"
```

---

### Task 6: Integration test with predictable model

**Files:**
- Modify: `loop/loop_test.go` (or create `loop/deferred_test.go`)

**Step 1: Write an integration test**

Create `loop/deferred_test.go` that verifies:
1. A loop with deferred tools only sends core tools in the initial LLM request
2. After `request_tools` is called, subsequent requests include the activated tools
3. Deferred tools can still be executed (via `handleToolCalls` which uses `AllTools`)

This test uses a capturing mock LLM that records the tools it receives in each request.

**Step 2: Run all tests**

Run: `cd /Users/toddgruben/Projects/shelley && make ui && go test ./...`
Expected: all PASS

**Step 3: Commit**

```bash
git add loop/deferred_test.go
git commit -m "test: integration test for deferred tool loading"
```

---

### Task 7: Remove old `Tools()` method, clean up

**Files:**
- Modify: `claudetool/toolset.go` — remove `Tools()` (replaced by `AllTools()` and `ActiveTools()`)
- Modify: any remaining callers of `Tools()`
- Modify: `claudetool/toolset_test.go` — update tests

**Step 1: Find all callers**

```bash
grep -rn '\.Tools()' --include='*.go' /Users/toddgruben/Projects/shelley/
```

Update each to use `AllTools()` or `ActiveTools()` as appropriate.

**Step 2: Run full test suite**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add -A
git commit -m "refactor: replace Tools() with AllTools()/ActiveTools()"
```

---

## Token Budget Impact

Estimated savings when browser + LSP are enabled but not yet activated:
- 7 browser tool definitions (~3-4K tokens)
- 1 LSP tool definition (~500 tokens)
- 1 output_iframe definition (~400 tokens)
- **Net savings: ~4-5K tokens per turn** (minus ~200 tokens for `request_tools` definition)

The savings scale linearly with conversation length since tools are re-sent every turn.

## Future Extensions

- MCP servers: tools from MCP servers are natural candidates for `Deferred: true`
- Auto-activation: if the LLM calls a tool that's deferred, auto-activate its category and retry (instead of returning "tool not found")
- Per-model categories: different models might have different core/deferred splits
