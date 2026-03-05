# MuninnDB Memory Augmentation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Augment Percy's memory system with an optional MuninnDB backend so that (a) memories are written to both local SQLite and a shared MuninnDB server, (b) search results merge from both sources, and (c) multiple Percy instances on a LAN share a cognitive memory layer.

**Architecture:** A new `memory/muninn` package wraps the MuninnDB Go SDK REST client. The existing `memory_search` tool and `indexConversation` write path gain an optional MuninnDB sink/source. Configuration is via environment variables (`PERCY_MUNINN_URL`, `PERCY_MUNINN_VAULT`, `PERCY_MUNINN_TOKEN`). The local SQLite memory remains primary — MuninnDB is additive. If MuninnDB is unreachable, Percy operates normally with local-only memory.

**Tech Stack:** Go, MuninnDB REST API (no SDK dependency — we vendor a thin HTTP client to avoid the BSL-licensed module), existing `memory` package.

---

## Design Decisions

1. **No `go get` of the MuninnDB SDK.** The SDK is BSL-licensed. We write our own thin REST client (~150 lines) hitting `/api/engrams`, `/api/activate`, and `/api/engrams/batch`. This keeps Percy's dependency tree clean.

2. **Dual-write, merged-read.** On indexing: write cells to local SQLite (existing) AND push engrams to MuninnDB. On search: query both, interleave results by score, deduplicate by content hash.

3. **Vault-per-instance or shared vault.** Default vault is `"percy"`. Configurable via `PERCY_MUNINN_VAULT`. All Percy instances on the LAN sharing the same vault get cross-agent memory.

4. **Fire-and-forget writes.** MuninnDB writes are async and non-blocking. If the server is down, we log a warning and continue. Local SQLite is the source of truth.

5. **Activation > Search.** When querying MuninnDB, we use the `/api/activate` endpoint (context-aware, temporally weighted) rather than plain search. This is the whole point — we get cognitive scoring that SQLite FTS5 can't provide.

---

## Prerequisites

- A running MuninnDB instance (e.g., `docker run -d --name muninndb -p 8475:8475 ghcr.io/scrypster/muninndb:latest`)
- Three environment variables:
  - `PERCY_MUNINN_URL` — e.g., `http://192.168.1.50:8475` (empty = disabled)
  - `PERCY_MUNINN_VAULT` — e.g., `percy` (default: `percy`)
  - `PERCY_MUNINN_TOKEN` — API token (default: empty, works for default vault)

---

## Task 1: MuninnDB REST Client

Build a thin HTTP client for the MuninnDB REST API. No external dependencies.

**Files:**
- Create: `memory/muninn/client.go`
- Create: `memory/muninn/client_test.go`

**Step 1: Write the failing test**

```go
// memory/muninn/client_test.go
package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteEngram(t *testing.T) {
	var got WriteRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/engrams" || r.Method != "POST" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(WriteResponse{ID: "test-id-123"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "percy", "")
	id, err := c.Write(context.Background(), "auth design", "JWT with refresh tokens", []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "test-id-123" {
		t.Fatalf("got id %q, want %q", id, "test-id-123")
	}
	if got.Concept != "auth design" {
		t.Fatalf("got concept %q, want %q", got.Concept, "auth design")
	}
}

func TestActivate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/activate" || r.Method != "POST" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(ActivateResponse{
			Activations: []Activation{
				{ID: "eng-1", Concept: "auth", Content: "JWT design", Score: 0.95},
			},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "percy", "")
	resp, err := c.Activate(context.Background(), []string{"login flow"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Activations) != 1 || resp.Activations[0].Concept != "auth" {
		t.Fatalf("unexpected activations: %+v", resp.Activations)
	}
}

func TestWriteBatch(t *testing.T) {
	var got BatchWriteRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/engrams/batch" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(BatchWriteResponse{
			IDs: []string{"id-1", "id-2"},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "percy", "")
	ids, err := c.WriteBatch(context.Background(), []WriteRequest{
		{Concept: "one", Content: "first"},
		{Concept: "two", Content: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d ids, want 2", len(ids))
	}
}

func TestClientReturnsErrorOnHTTPFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "percy", "")
	_, err := c.Write(context.Background(), "test", "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./memory/muninn/`
Expected: FAIL — package doesn't exist

**Step 3: Write the implementation**

```go
// memory/muninn/client.go
package muninn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin REST client for MuninnDB.
type Client struct {
	baseURL string
	vault   string
	token   string
	http    *http.Client
}

// NewClient creates a MuninnDB client.
func NewClient(baseURL, vault, token string) *Client {
	return &Client{
		baseURL: baseURL,
		vault:   vault,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type WriteRequest struct {
	Vault   string   `json:"vault,omitempty"`
	Concept string   `json:"concept"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

type WriteResponse struct {
	ID string `json:"id"`
}

type BatchWriteRequest struct {
	Engrams []WriteRequest `json:"engrams"`
}

type BatchWriteResponse struct {
	IDs []string `json:"ids"`
}

type ActivateRequest struct {
	Vault      string   `json:"vault"`
	Context    []string `json:"context"`
	MaxResults int      `json:"max_results"`
}

type ActivateResponse struct {
	Activations []Activation `json:"activations"`
}

type Activation struct {
	ID      string  `json:"id"`
	Concept string  `json:"concept"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Tags    []string `json:"tags,omitempty"`
}

// Write stores a single engram.
func (c *Client) Write(ctx context.Context, concept, content string, tags []string) (string, error) {
	req := WriteRequest{
		Vault:   c.vault,
		Concept: concept,
		Content: content,
		Tags:    tags,
	}
	var resp WriteResponse
	if err := c.post(ctx, "/api/engrams", req, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// WriteBatch stores up to 50 engrams in one call.
func (c *Client) WriteBatch(ctx context.Context, engrams []WriteRequest) ([]string, error) {
	for i := range engrams {
		if engrams[i].Vault == "" {
			engrams[i].Vault = c.vault
		}
	}
	var resp BatchWriteResponse
	if err := c.post(ctx, "/api/engrams/batch", BatchWriteRequest{Engrams: engrams}, &resp); err != nil {
		return nil, err
	}
	return resp.IDs, nil
}

// Activate retrieves context-aware, temporally-weighted memories.
func (c *Client) Activate(ctx context.Context, context_ []string, maxResults int) (*ActivateResponse, error) {
	req := ActivateRequest{
		Vault:      c.vault,
		Context:    context_,
		MaxResults: maxResults,
	}
	var resp ActivateResponse
	if err := c.post(ctx, "/api/activate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Healthy returns true if the MuninnDB server is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/stats", nil)
	if err != nil {
		return false
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) post(ctx context.Context, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("muninn: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("muninn: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("muninn: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("muninn: HTTP %d: %s", resp.StatusCode, body)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("muninn: decode: %w", err)
		}
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
```

**Step 4: Run tests**

Run: `cd /Users/toddgruben/Projects/shelley && go test ./memory/muninn/`
Expected: PASS (all 4 tests)

**Step 5: Commit**

```bash
git add memory/muninn/
git commit -m "feat: add MuninnDB REST client"
```

---

## Task 2: Dual-Write on Conversation Indexing

When a conversation is indexed, also push engrams to MuninnDB.

**Files:**
- Create: `memory/muninn/sink.go`
- Create: `memory/muninn/sink_test.go`
- Modify: `server/server.go` (~lines 270-280, 320-395)
- Modify: `cmd/percy/main.go` (~lines 107-172)

**Step 1: Write the failing test for the sink**

```go
// memory/muninn/sink_test.go
package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSinkPushCells(t *testing.T) {
	var mu sync.Mutex
	var received BatchWriteRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(BatchWriteResponse{IDs: []string{"a", "b"}})
	}))
	defer ts.Close()

	sink := NewSink(NewClient(ts.URL, "test-vault", ""))
	cells := []Cellengram{
		{Concept: "auth", Content: "JWT tokens", Tags: []string{"security"}},
		{Concept: "db", Content: "use SQLite", Tags: []string{"storage"}},
	}
	err := sink.Push(context.Background(), "conv-123", "my-chat", cells)
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received.Engrams) != 2 {
		t.Fatalf("got %d engrams, want 2", len(received.Engrams))
	}
	if received.Engrams[0].Concept != "auth" {
		t.Fatalf("got concept %q, want %q", received.Engrams[0].Concept, "auth")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./memory/muninn/ -run TestSinkPush`
Expected: FAIL — Sink type doesn't exist

**Step 3: Write the Sink implementation**

```go
// memory/muninn/sink.go
package muninn

import (
	"context"
	"fmt"
	"log/slog"
)

// CellEngram maps a Percy memory cell to a MuninnDB engram.
type CellEngram struct {
	Concept string
	Content string
	Tags    []string
}

// Sink pushes memory cells to MuninnDB as engrams.
type Sink struct {
	client *Client
}

// NewSink creates a new MuninnDB sink.
func NewSink(client *Client) *Sink {
	return &Sink{client: client}
}

// Push sends cells to MuninnDB as a batch. Adds source metadata as tags.
func (s *Sink) Push(ctx context.Context, conversationID, slug string, cells []CellEngram) error {
	if len(cells) == 0 {
		return nil
	}

	engrams := make([]WriteRequest, len(cells))
	for i, c := range cells {
		tags := append([]string{}, c.Tags...)
		tags = append(tags, "percy", "conv:"+conversationID)
		if slug != "" {
			tags = append(tags, "slug:"+slug)
		}
		engrams[i] = WriteRequest{
			Concept: c.Concept,
			Content: c.Content,
			Tags:    tags,
		}
	}

	// Batch in groups of 50 (MuninnDB limit)
	for i := 0; i < len(engrams); i += 50 {
		end := i + 50
		if end > len(engrams) {
			end = len(engrams)
		}
		_, err := s.client.WriteBatch(ctx, engrams[i:end])
		if err != nil {
			return fmt.Errorf("muninn sink: batch %d-%d: %w", i, end, err)
		}
	}
	return nil
}

// PushAsync sends cells in a goroutine, logging errors instead of returning them.
func (s *Sink) PushAsync(conversationID, slug string, cells []CellEngram) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Push(ctx, conversationID, slug, cells); err != nil {
			slog.Warn("MuninnDB push failed", "error", err, "conversation", conversationID)
		}
	}()
}
```

*Note: `PushAsync` uses `time` — add the import. The test above uses `Cellengram` which should be `CellEngram` — fix casing in test.*

**Step 4: Run test**

Run: `go test ./memory/muninn/`
Expected: PASS

**Step 5: Wire into `server/server.go`**

Add a `muninnSink` field to Server, set via a new `SetMuninnSink` method. In `indexConversation`, after the existing `memoryDB.IndexConversation` call, convert the indexed cells to `CellEngram`s and call `s.muninnSink.PushAsync()`.

Modify `server/server.go`:
- Add field: `muninnSink *muninn.Sink` (~line 222)
- Add method: `SetMuninnSink(sink *muninn.Sink)` (~line 280)
- In `indexConversation()` (~line 389, after successful index): convert messages to engrams and push

**Step 6: Wire into `cmd/percy/main.go`**

After the embedder setup block (~line 161), add MuninnDB client initialization:

```go
// MuninnDB augmentation (optional)
if muninnURL := os.Getenv("PERCY_MUNINN_URL"); muninnURL != "" {
	vault := os.Getenv("PERCY_MUNINN_VAULT")
	if vault == "" {
		vault = "percy"
	}
	token := os.Getenv("PERCY_MUNINN_TOKEN")
	muninnClient := muninn.NewClient(muninnURL, vault, token)
	if muninnClient.Healthy(context.Background()) {
		svr.SetMuninnSink(muninn.NewSink(muninnClient))
		logger.Info("MuninnDB connected", "url", muninnURL, "vault", vault)
	} else {
		logger.Warn("MuninnDB unreachable, running without augmentation", "url", muninnURL)
	}
}
```

**Step 7: Commit**

```bash
git add memory/muninn/ server/server.go cmd/percy/main.go
git commit -m "feat: dual-write memory cells to MuninnDB"
```

---

## Task 3: Merged Read — Augment memory_search with MuninnDB Activation

The `memory_search` tool queries both local SQLite and MuninnDB, merges results.

**Files:**
- Create: `memory/muninn/source.go`
- Create: `memory/muninn/source_test.go`
- Modify: `claudetool/memory/tool.go`
- Modify: `claudetool/toolset.go` (add MuninnDB source to config)
- Modify: `cmd/percy/main.go` (pass client to tool)

**Step 1: Write the failing test for the source**

```go
// memory/muninn/source_test.go
package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	memdb "github.com/tgruben-circuit/percy/memory"
)

func TestSourceSearch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ActivateResponse{
			Activations: []Activation{
				{ID: "eng-1", Concept: "auth design", Content: "JWT with refresh", Score: 0.92},
				{ID: "eng-2", Concept: "db choice", Content: "SQLite for simplicity", Score: 0.71},
			},
		})
	}))
	defer ts.Close()

	src := NewSource(NewClient(ts.URL, "percy", ""))
	results, err := src.Search(context.Background(), "authentication", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ResultType != "muninn" {
		t.Fatalf("got type %q, want %q", results[0].ResultType, "muninn")
	}
	if results[0].Content != "JWT with refresh" {
		t.Fatalf("got content %q", results[0].Content)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./memory/muninn/ -run TestSourceSearch`
Expected: FAIL

**Step 3: Implement Source**

```go
// memory/muninn/source.go
package muninn

import (
	"context"

	memdb "github.com/tgruben-circuit/percy/memory"
)

// Source queries MuninnDB and returns results as MemoryResults.
type Source struct {
	client *Client
}

func NewSource(client *Client) *Source {
	return &Source{client: client}
}

// Search activates MuninnDB with the query and converts to MemoryResults.
func (s *Source) Search(ctx context.Context, query string, limit int) ([]memdb.MemoryResult, error) {
	resp, err := s.client.Activate(ctx, []string{query}, limit)
	if err != nil {
		return nil, err
	}

	results := make([]memdb.MemoryResult, len(resp.Activations))
	for i, a := range resp.Activations {
		results[i] = memdb.MemoryResult{
			ResultType: "muninn",
			TopicName:  a.Concept,
			Content:    a.Content,
			Score:      a.Score,
		}
	}
	return results, nil
}
```

**Step 4: Run test**

Run: `go test ./memory/muninn/`
Expected: PASS

**Step 5: Update the memory search tool to accept an optional MuninnDB source**

Modify `claudetool/memory/tool.go`:
- Add a `muninnSource` field to `MemorySearchTool`
- Add `NewMemorySearchToolWithMuninn(db, embedder, source)` constructor
- In `Run()`, after the local search, also query MuninnDB and append results
- Update `formatMemoryResults` to handle `ResultType == "muninn"`

The merge strategy: local results first (they're the source of truth), then MuninnDB results that aren't duplicates. Deduplicate by checking if any MuninnDB result's content is a substring of a local result's content (or vice versa).

**Step 6: Update `claudetool/toolset.go`**

Add `MuninnSource *muninn.Source` to `ToolSetConfig` (~line 78).
Pass it through when constructing the memory search tool.

**Step 7: Update `cmd/percy/main.go`**

In the MuninnDB setup block (from Task 2), also set:
```go
toolSetConfig.MuninnSource = muninn.NewSource(muninnClient)
```

**Step 8: Commit**

```bash
git add memory/muninn/ claudetool/memory/ claudetool/toolset.go cmd/percy/main.go
git commit -m "feat: merge MuninnDB activations into memory_search results"
```

---

## Task 4: Format MuninnDB Results in the UI

The `formatMemoryResults` function needs a case for `"muninn"` result type, and the frontend may need to render it.

**Files:**
- Modify: `claudetool/memory/tool.go` (add format case)

**Step 1: Update formatMemoryResults**

Add a case in the switch:
```go
case "muninn":
	fmt.Fprintf(&b, "--- Muninn Memory: %q (score: %.2f) ---\n%s\n\n", r.TopicName, r.Score, r.Content)
```

**Step 2: Verify build**

Run: `cd /Users/toddgruben/Projects/shelley && go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add claudetool/memory/tool.go
git commit -m "feat: format MuninnDB results in memory search output"
```

---

## Task 5: Integration Test with Mock MuninnDB Server

End-to-end test: index a conversation → verify dual write → search → verify merged results.

**Files:**
- Create: `memory/muninn/integration_test.go`

**Step 1: Write integration test**

```go
// memory/muninn/integration_test.go
package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestFullCycle verifies write → activate round-trip against a mock server.
func TestFullCycle(t *testing.T) {
	var mu sync.Mutex
	stored := map[string]WriteRequest{}
	idCounter := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/api/engrams":
			var req WriteRequest
			json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("eng-%d", idCounter)
			idCounter++
			stored[id] = req
			json.NewEncoder(w).Encode(WriteResponse{ID: id})

		case "/api/activate":
			var activations []Activation
			for id, eng := range stored {
				activations = append(activations, Activation{
					ID:      id,
					Concept: eng.Concept,
					Content: eng.Content,
					Score:   0.8,
				})
			}
			json.NewEncoder(w).Encode(ActivateResponse{Activations: activations})

		case "/api/stats":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test", "")

	// Verify health
	if !client.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}

	// Write
	id, err := client.Write(context.Background(), "test concept", "test content", []string{"tag1"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}

	// Activate
	src := NewSource(client)
	results, err := src.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Content != "test content" {
		t.Fatalf("got content %q", results[0].Content)
	}
}
```

**Step 2: Run test**

Run: `go test ./memory/muninn/ -run TestFullCycle`
Expected: PASS

**Step 3: Commit**

```bash
git add memory/muninn/integration_test.go
git commit -m "test: add MuninnDB integration test with mock server"
```

---

## Task 6: Documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Step 1: Add MuninnDB section to README**

After the "Memory Search" section, add:

```markdown
### MuninnDB Augmentation (Optional)

Percy can optionally push memories to a [MuninnDB](https://github.com/scrypster/muninndb) server and include its context-aware activations in search results. This enables cross-agent memory sharing on a LAN — what one Percy instance learns becomes available to all others.

```bash
# Start MuninnDB (Docker)
docker run -d --name muninndb -p 8475:8475 ghcr.io/scrypster/muninndb:latest

# Configure Percy
export PERCY_MUNINN_URL=http://localhost:8475
export PERCY_MUNINN_VAULT=percy        # optional, default: percy
export PERCY_MUNINN_TOKEN=your-token   # optional, default: empty
```

Local SQLite memory remains primary. MuninnDB is additive — if unreachable, Percy operates normally.
```

**Step 2: Update CLAUDE.md architecture section**

Add to the architecture table:
```
memory/muninn/        Optional MuninnDB REST client for cross-agent memory augmentation
```

**Step 3: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "docs: document MuninnDB augmentation"
```

---

## Summary

| Task | What | Files | Est. |
|------|------|-------|------|
| 1 | MuninnDB REST client | `memory/muninn/client.go`, test | 20 min |
| 2 | Dual-write sink | `memory/muninn/sink.go`, server wiring | 25 min |
| 3 | Merged-read source | `memory/muninn/source.go`, tool wiring | 25 min |
| 4 | Format MuninnDB results | `claudetool/memory/tool.go` | 5 min |
| 5 | Integration test | `memory/muninn/integration_test.go` | 15 min |
| 6 | Documentation | README, CLAUDE.md | 10 min |

**Total: ~100 minutes of implementation.**

---

## What I Need From You

1. **MuninnDB running on your LAN.** Confirm the URL (e.g., `http://192.168.x.x:8475`).
2. **Vault name preference.** Default `percy` works, or you may want per-machine vaults.
3. **API token.** If you've configured auth on your MuninnDB instance.
4. **Confirmation on the "no SDK dependency" approach.** I'm writing a ~150-line REST client rather than `go get`-ing their BSL-licensed module. This keeps your dependency tree clean but means we track any API changes manually.
