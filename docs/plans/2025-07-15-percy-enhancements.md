# Percy Enhancements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add six major features to Percy: conversation forking, cost dashboard, message editing/regeneration, conversation export, file tree viewer, and Ollama support.

**Architecture:** Each feature is independent and can be built in any order. All follow the same pattern: DB migration (if needed) → Go API endpoint → React UI component. Features are additive — no existing behavior changes.

**Tech Stack:** Go (net/http, sqlc), SQLite, React/TypeScript, SSE streaming, esbuild

**Dependencies between features:** None. All six are independent.

---

## Feature 1: Conversation Forking / Branching

Fork a conversation at any message, creating a new conversation with history up to that point.

### Task 1.1: DB Query — Copy Messages Up To Sequence

**Files:**
- Modify: `db/query/messages.sql`
- Modify: `db/query/conversations.sql`
- Regenerate: `db/generated/` (via `sqlc generate`)

**Step 1: Add the fork-related queries**

Add to `db/query/messages.sql`:
```sql
-- name: ListMessagesUpToSequence :many
SELECT * FROM messages
WHERE conversation_id = ? AND sequence_id <= ?
ORDER BY sequence_id ASC;
```

**Step 2: Regenerate sqlc**

Run: `sqlc generate`
Expected: New functions in `db/generated/query.sql.go`

**Step 3: Commit**
```bash
git add db/query/ db/generated/
git commit -m "feat: add ListMessagesUpToSequence query for conversation forking"
```

### Task 1.2: DB Helper — ForkConversation

**Files:**
- Modify: `db/db.go`
- Create: `db/db_fork_test.go`

**Step 1: Write the failing test**

```go
func TestForkConversation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Create source conversation with 5 messages
	conv, err := db.CreateConversation(ctx, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, err := db.CreateMessage(ctx, CreateMessageParams{
			ConversationID: conv.ConversationID,
			Type:           MessageTypeUser,
			UserData:       map[string]string{"text": fmt.Sprintf("msg %d", i)},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Fork at sequence 3
	forked, err := db.ForkConversation(ctx, conv.ConversationID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify new conversation exists
	if forked.ConversationID == conv.ConversationID {
		t.Fatal("fork should create a new conversation")
	}

	// Verify it has exactly 3 messages
	msgs, err := db.ListMessages(ctx, forked.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Verify messages are re-sequenced 1..3
	for i, m := range msgs {
		if m.SequenceID != int64(i+1) {
			t.Fatalf("expected sequence_id %d, got %d", i+1, m.SequenceID)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./db -run TestForkConversation -v`
Expected: FAIL — `ForkConversation` not defined

**Step 3: Implement ForkConversation**

Add to `db/db.go`:
```go
// ForkConversation creates a new conversation by copying messages up to (and including)
// the given sequence ID from the source conversation.
func (db *DB) ForkConversation(ctx context.Context, sourceConversationID string, atSequenceID int64, modelID *string) (*generated.Conversation, error) {
	// Get source conversation for cwd
	source, err := db.GetConversationByID(ctx, sourceConversationID)
	if err != nil {
		return nil, fmt.Errorf("get source conversation: %w", err)
	}

	// Create new conversation
	conv, err := db.CreateConversation(ctx, nil, true, ptrToStringPtr(source.Cwd), modelID)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}

	// Copy messages up to the given sequence
	msgs, err := db.writer.ListMessagesUpToSequence(ctx, generated.ListMessagesUpToSequenceParams{
		ConversationID: sourceConversationID,
		SequenceID:     atSequenceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list source messages: %w", err)
	}

	for i, msg := range msgs {
		_, err := db.writer.CreateMessage(ctx, generated.CreateMessageParams{
			MessageID:           generateMessageID(),
			ConversationID:      conv.ConversationID,
			SequenceID:          int64(i + 1),
			Type:                msg.Type,
			LlmData:             msg.LlmData,
			UserData:            msg.UserData,
			UsageData:           msg.UsageData,
			DisplayData:         msg.DisplayData,
			ExcludedFromContext: msg.ExcludedFromContext,
		})
		if err != nil {
			return nil, fmt.Errorf("copy message %d: %w", i, err)
		}
	}

	return conv, nil
}
```

Note: You'll need to check the exact generated `CreateMessage` params and `generateMessageID` helper — adapt to match existing patterns in `db.go`. The key logic is: create conversation, list source messages up to sequence, insert copies with new IDs and re-sequenced.

**Step 4: Run test to verify it passes**

Run: `go test ./db -run TestForkConversation -v`
Expected: PASS

**Step 5: Commit**
```bash
git add db/
git commit -m "feat: add ForkConversation DB helper"
```

### Task 1.3: API Endpoint — POST /api/conversations/fork

**Files:**
- Modify: `server/handlers.go`
- Modify: `server/server.go` (route registration)
- Create: `server/fork_test.go`

**Step 1: Write the failing test**

Follow the pattern of `TestHandleRenameConversation` in `server/handlers_test.go`. Create a conversation with messages, POST to `/api/conversations/fork` with `{source_conversation_id, at_sequence_id, model}`, assert 201 + new conversation_id returned.

**Step 2: Run test to verify it fails**

Run: `go test ./server -run TestHandleForkConversation -v`
Expected: FAIL — 404 (route not registered)

**Step 3: Implement the handler**

Add to `server/handlers.go`:
```go
type ForkConversationRequest struct {
	SourceConversationID string `json:"source_conversation_id"`
	AtSequenceID         int64  `json:"at_sequence_id"`
	Model                string `json:"model,omitempty"`
}

func (s *Server) handleForkConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ForkConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.SourceConversationID == "" || req.AtSequenceID <= 0 {
		http.Error(w, "source_conversation_id and at_sequence_id are required", http.StatusBadRequest)
		return
	}

	// Determine model
	modelID := req.Model
	if modelID == "" {
		source, err := s.db.GetConversationByID(r.Context(), req.SourceConversationID)
		if err != nil {
			http.Error(w, "Source conversation not found", http.StatusNotFound)
			return
		}
		if source.Model != nil {
			modelID = *source.Model
		}
	}

	var modelPtr *string
	if modelID != "" {
		modelPtr = &modelID
	}

	conv, err := s.db.ForkConversation(r.Context(), req.SourceConversationID, req.AtSequenceID, modelPtr)
	if err != nil {
		s.logger.Error("Failed to fork conversation", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	go s.publishConversationListUpdate(ConversationListUpdate{
		Type:         "update",
		Conversation: conv,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"conversation_id": conv.ConversationID})
}
```

Register in `server/server.go`:
```go
mux.Handle("/api/conversations/fork", http.HandlerFunc(s.handleForkConversation))
```

**Step 4: Run test to verify it passes**

Run: `go test ./server -run TestHandleForkConversation -v`
Expected: PASS

**Step 5: Commit**
```bash
git add server/
git commit -m "feat: add POST /api/conversations/fork endpoint"
```

### Task 1.4: UI — Fork Button on Messages

**Files:**
- Modify: `ui/src/services/api.ts`
- Modify: `ui/src/components/MessageActionBar.tsx`
- Modify: `ui/src/components/ChatInterface.tsx`
- Modify: `ui/src/App.tsx`

**Step 1: Add API method**

In `api.ts`:
```typescript
async forkConversation(sourceConversationId: string, atSequenceId: number, model?: string): Promise<{ conversation_id: string }> {
  const response = await fetch(`${this.baseUrl}/conversations/fork`, {
    method: "POST",
    headers: this.postHeaders,
    body: JSON.stringify({ source_conversation_id: sourceConversationId, at_sequence_id: atSequenceId, model }),
  });
  if (!response.ok) throw new Error(`Failed to fork: ${response.statusText}`);
  return response.json();
}
```

**Step 2: Add fork button to MessageActionBar**

Add a fork/branch icon button next to the existing copy button. It should appear on user and agent messages. On click, call `onFork(sequenceId)` prop.

**Step 3: Wire up in ChatInterface**

Pass `onFork` callback to MessageActionBar that calls `api.forkConversation()` then navigates to the new conversation (call `onConversationUpdate` from App to switch).

**Step 4: Build and test manually**

Run: `make build && ./bin/percy --model predictable --db /tmp/percy-test.db serve -port 8002`
Navigate to http://localhost:8002, create a conversation, hover a message, click fork button.

**Step 5: Commit**
```bash
git add ui/src/
git commit -m "feat: add fork button to conversation messages"
```

---

## Feature 2: Cost Tracking Dashboard

Aggregate view of spending by day, model, and conversation.

### Task 2.1: DB Query — Aggregate Usage Data

**Files:**
- Create: `db/query/usage.sql`
- Regenerate: `db/generated/`

**Step 1: Add usage aggregation queries**

Create `db/query/usage.sql`:
```sql
-- name: GetUsageSummary :many
-- Aggregates usage data from agent messages across all conversations.
-- Returns one row per (date, model) pair.
SELECT
  date(m.created_at) as date,
  c.model,
  COUNT(*) as message_count,
  SUM(json_extract(m.usage_data, '$.input_tokens')) as total_input_tokens,
  SUM(json_extract(m.usage_data, '$.output_tokens')) as total_output_tokens,
  SUM(json_extract(m.usage_data, '$.cost_usd')) as total_cost_usd
FROM messages m
JOIN conversations c ON m.conversation_id = c.conversation_id
WHERE m.type = 'agent'
  AND m.usage_data IS NOT NULL
  AND m.created_at >= ?
GROUP BY date(m.created_at), c.model
ORDER BY date(m.created_at) DESC;

-- name: GetUsageByConversation :many
-- Aggregates usage per conversation for a date range.
SELECT
  c.conversation_id,
  c.slug,
  c.model,
  COUNT(*) as message_count,
  SUM(json_extract(m.usage_data, '$.input_tokens')) as total_input_tokens,
  SUM(json_extract(m.usage_data, '$.output_tokens')) as total_output_tokens,
  SUM(json_extract(m.usage_data, '$.cost_usd')) as total_cost_usd
FROM messages m
JOIN conversations c ON m.conversation_id = c.conversation_id
WHERE m.type = 'agent'
  AND m.usage_data IS NOT NULL
  AND m.created_at >= ?
GROUP BY c.conversation_id
ORDER BY total_cost_usd DESC;
```

**Step 2: Regenerate sqlc**

Run: `sqlc generate`

**Step 3: Commit**
```bash
git add db/
git commit -m "feat: add usage aggregation queries for cost dashboard"
```

### Task 2.2: API Endpoint — GET /api/usage

**Files:**
- Create: `server/usage_handlers.go`
- Create: `server/usage_handlers_test.go`
- Modify: `server/server.go` (route registration)

**Step 1: Write the test**

Test that creating a conversation with agent messages that have `usage_data` JSON returns correct aggregations from `GET /api/usage?since=2024-01-01`.

**Step 2: Implement the handler**

Return JSON:
```json
{
  "by_date": [{"date": "2025-07-15", "model": "claude-opus-4.6", "message_count": 12, "input_tokens": 50000, "output_tokens": 8000, "cost_usd": 0.42}],
  "by_conversation": [{"conversation_id": "c12345", "slug": "fix-auth", "model": "claude-opus-4.6", "message_count": 12, "cost_usd": 0.42}],
  "total_cost_usd": 3.14
}
```

Register: `mux.Handle("GET /api/usage", gzipHandler(http.HandlerFunc(s.handleUsage)))`

**Step 3: Run test, commit**
```bash
git add server/
git commit -m "feat: add GET /api/usage endpoint"
```

### Task 2.3: UI — Cost Dashboard Modal

**Files:**
- Create: `ui/src/components/CostDashboard.tsx`
- Modify: `ui/src/services/api.ts`
- Modify: `ui/src/App.tsx`
- Modify: `ui/src/components/ConversationDrawer.tsx`

**Step 1: Add API method**

```typescript
async getUsage(since: string): Promise<UsageSummary> {
  const response = await fetch(`${this.baseUrl}/usage?since=${since}`);
  if (!response.ok) throw new Error(`Failed to get usage: ${response.statusText}`);
  return response.json();
}
```

**Step 2: Create CostDashboard component**

Use the `<Modal>` wrapper component. Display:
- **Period selector**: 7d / 30d / 90d / All time (buttons at top)
- **Total spend**: Large number at top
- **By-day table**: Date | Model | Messages | Input Tokens | Output Tokens | Cost
- **By-conversation table**: Slug (clickable) | Model | Messages | Cost
- Use existing inline styles pattern (no CSS modules in this project)

**Step 3: Add trigger in ConversationDrawer**

Add a "💰 Usage" button in the drawer footer (next to existing gear/settings area). On click, open the CostDashboard modal.

**Step 4: Wire up in App.tsx**

Add `showCostDashboard` state, pass toggle to drawer, render `<CostDashboard>` when open.

**Step 5: Build and test manually**

Run: `make build && ./bin/percy --model predictable --db /tmp/percy-test.db serve -port 8002`

**Step 6: Commit**
```bash
git add ui/src/
git commit -m "feat: add cost dashboard UI"
```

---

## Feature 3: Message Editing & Regeneration

Edit a user message and replay from that point. Regenerate an agent response.

### Task 3.1: API Endpoint — POST /api/conversation/{id}/edit

This endpoint takes a `sequence_id` and new `message` text. It deletes all messages after that sequence, updates the message content, and triggers the agent loop.

**Files:**
- Modify: `db/query/messages.sql`
- Regenerate: `db/generated/`
- Modify: `db/db.go`

**Step 1: Add DB queries**

Add to `db/query/messages.sql`:
```sql
-- name: DeleteMessagesAfterSequence :exec
DELETE FROM messages
WHERE conversation_id = ? AND sequence_id > ?;

-- name: UpdateMessageLLMData :exec
UPDATE messages
SET llm_data = ?, user_data = ?
WHERE message_id = ?;
```

**Step 2: Regenerate sqlc and commit**

Run: `sqlc generate`
```bash
git add db/
git commit -m "feat: add delete-after-sequence and update-message queries"
```

### Task 3.2: Server Handler — Edit & Replay

**Files:**
- Create: `server/edit_handlers.go`
- Create: `server/edit_handlers_test.go`
- Modify: `server/server.go`

**Step 1: Write the test**

Create conversation with 6 messages. POST edit at sequence 2 with new text. Verify messages after sequence 2 are deleted. Verify message at sequence 2 is updated.

**Step 2: Implement the handler**

```go
type EditMessageRequest struct {
	SequenceID int64  `json:"sequence_id"`
	Message    string `json:"message"`
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Parse request
	// 2. Delete messages after sequence_id
	// 3. Update the message at sequence_id with new content
	// 4. Reset the conversation manager's loop (cancel existing, nil it out)
	// 5. Re-queue as a new user message to trigger the agent
	// 6. Return 202
}
```

The key insight: after editing, the ConversationManager's loop must be reset. Call `cm.CancelConversation()` (which nils the loop), then `cm.AcceptUserMessage()` will call `ensureLoop()` which reloads all messages from DB fresh. Since we deleted the messages after the edit point, the loop sees the edited history.

But we need a variant: the edited message is already in DB, so we should NOT record a new user message. Instead:
1. Delete messages after `sequence_id`
2. Update the message at `sequence_id` with new llm_data/user_data
3. Cancel any running loop
4. Create a new loop from DB state (ensureLoop handles this)
5. The last message in history is the edited user message, so the loop will call the LLM

Actually, the simplest approach that fits the existing architecture:
1. Delete messages at and after `sequence_id`
2. Cancel any running loop
3. Call `AcceptUserMessage()` with the new text — this records it as a new message and starts the loop

This avoids needing to update message content in place.

Register: `mux.HandleFunc("POST /{id}/edit", ...)` in `conversationMux()`

**Step 3: Run test, commit**
```bash
git add server/
git commit -m "feat: add POST /conversation/{id}/edit endpoint"
```

### Task 3.3: API Endpoint — POST /api/conversation/{id}/regenerate

Regenerate the last agent response. Deletes the last agent message (and any tool messages after it), then re-triggers the loop.

**Files:**
- Modify: `server/edit_handlers.go`
- Modify: `server/edit_handlers_test.go`

**Step 1: Write the test**

Create conversation, send message, get agent response. POST regenerate. Verify old agent message is gone, new one appears.

**Step 2: Implement**

```go
func (s *Server) handleRegenerateMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Get the last user message's sequence_id
	// 2. Delete all messages after that sequence_id (agent + tool responses)
	// 3. Cancel any running loop
	// 4. Re-trigger the loop (ensureLoop will reload from DB, see the user message, call LLM)
}
```

Register: `mux.HandleFunc("POST /{id}/regenerate", ...)` in `conversationMux()`

**Step 3: Run test, commit**
```bash
git add server/
git commit -m "feat: add POST /conversation/{id}/regenerate endpoint"
```

### Task 3.4: UI — Edit and Regenerate Buttons

**Files:**
- Modify: `ui/src/services/api.ts`
- Modify: `ui/src/components/MessageActionBar.tsx`
- Modify: `ui/src/components/ChatInterface.tsx`
- Modify: `ui/src/components/Message.tsx`

**Step 1: Add API methods**

```typescript
async editMessage(conversationId: string, sequenceId: number, message: string): Promise<void> { ... }
async regenerateMessage(conversationId: string): Promise<void> { ... }
```

**Step 2: Add edit button on user messages**

In `MessageActionBar`, show a pencil icon on user messages. On click, switch the message into edit mode — replace the text display with a textarea pre-filled with the message text. Save button calls `editMessage()`. Cancel reverts.

**Step 3: Add regenerate button on agent messages**

In `MessageActionBar`, show a refresh icon on the last agent message. On click, call `regenerateMessage()`.

**Step 4: Handle SSE reconnect**

After edit/regenerate, the existing SSE stream should pick up the new messages automatically (they get published via subpub). The client may need to handle message deletions — the simplest approach is to refetch all messages after an edit/regenerate action.

**Step 5: Build and test manually, commit**
```bash
git add ui/src/
git commit -m "feat: add edit and regenerate buttons to messages"
```

---

## Feature 4: Export Conversations

Export a conversation as Markdown or JSON.

### Task 4.1: API Endpoint — GET /api/conversation/{id}/export

**Files:**
- Create: `server/export_handlers.go`
- Create: `server/export_handlers_test.go`
- Modify: `server/handlers.go` (route registration in `conversationMux()`)

**Step 1: Write the test**

Create conversation with user + agent messages. GET `/api/conversation/{id}/export?format=markdown`. Assert response is valid Markdown with correct Content-Type and Content-Disposition headers.

**Step 2: Implement the handler**

```go
func (s *Server) handleExportConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	format := r.URL.Query().Get("format") // "markdown" or "json"
	if format == "" {
		format = "markdown"
	}

	ctx := r.Context()
	conv, err := s.db.GetConversationByID(ctx, conversationID)
	// ... error handling

	messages, err := s.db.ListMessages(ctx, conversationID)
	// ... error handling

	slug := conversationID
	if conv.Slug != nil {
		slug = *conv.Slug
	}

	switch format {
	case "markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, slug))
		exportMarkdown(w, conv, messages)
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, slug))
		exportJSON(w, conv, messages)
	default:
		http.Error(w, "Invalid format. Use 'markdown' or 'json'", http.StatusBadRequest)
	}
}
```

The `exportMarkdown` function should:
- Write `# {slug}` header with date
- For each message: `## User` / `## Assistant` header
- Text content as-is
- Tool uses as fenced code blocks with tool name
- Tool results as fenced code blocks
- Skip thinking blocks
- Include `---` separators between turns

The `exportJSON` function should:
- Write the conversation metadata + all messages as a JSON object
- Include the parsed llm_data, user_data, usage_data as nested objects (not stringified JSON)

Register: `mux.HandleFunc("GET /{id}/export", ...)` in `conversationMux()`

**Step 3: Run test, commit**
```bash
git add server/
git commit -m "feat: add GET /conversation/{id}/export endpoint"
```

### Task 4.2: UI — Export Button

**Files:**
- Modify: `ui/src/components/ChatInterface.tsx`

**Step 1: Add export to overflow menu**

The ChatInterface header has an overflow menu (the `...` button). Add "Export as Markdown" and "Export as JSON" options. On click, open `window.open("/api/conversation/{id}/export?format=markdown")` — the browser will download the file due to Content-Disposition header.

**Step 2: Build and test manually, commit**
```bash
git add ui/src/
git commit -m "feat: add export buttons to conversation overflow menu"
```

---

## Feature 5: File Tree / Context Viewer

Show files the agent has touched in the current conversation.

### Task 5.1: Extract Touched Files from Messages (Server-Side)

**Files:**
- Create: `server/filetree.go`
- Create: `server/filetree_test.go`
- Modify: `server/handlers.go` (route registration in `conversationMux()`)

Rather than maintaining state, we extract touched files from message content on demand. Scan tool_use messages for `patch` (has `path` in input), `bash` (look for common file operations), `read_file` (has `path`), `change_dir` (has `path`).

**Step 1: Write the test**

Create messages with various tool_use content. Call `extractTouchedFiles(messages)`. Verify it returns the correct file paths with operation types.

**Step 2: Implement**

```go
type TouchedFile struct {
	Path       string `json:"path"`
	Operation  string `json:"operation"` // "read", "write", "patch", "create", "navigate"
	FirstSeen  int64  `json:"first_seen"`  // sequence_id
	LastSeen   int64  `json:"last_seen"`   // sequence_id
}

func extractTouchedFiles(messages []generated.Message) []TouchedFile {
	fileMap := make(map[string]*TouchedFile)
	for _, msg := range messages {
		if msg.LlmData == nil { continue }
		var llmMsg llm.Message
		if err := json.Unmarshal([]byte(*msg.LlmData), &llmMsg); err != nil { continue }
		for _, content := range llmMsg.Content {
			if content.Type != llm.ContentTypeToolUse { continue }
			var input map[string]interface{}
			json.Unmarshal(content.ToolInput, &input)
			switch content.ToolName {
			case "patch":
				if path, ok := input["path"].(string); ok {
					// add/update path with operation "patch"
				}
			case "read_file":
				if path, ok := input["path"].(string); ok {
					// add/update path with operation "read"
				}
			case "change_dir":
				if path, ok := input["path"].(string); ok {
					// add/update path with operation "navigate"
				}
			}
		}
	}
	// Convert map to sorted slice
}

func (s *Server) handleTouchedFiles(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	messages, err := s.db.ListMessages(r.Context(), conversationID)
	// ... error handling
	files := extractTouchedFiles(messages)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}
```

Register: `mux.HandleFunc("GET /{id}/files", ...)` in `conversationMux()`

**Step 3: Run test, commit**
```bash
git add server/
git commit -m "feat: add GET /conversation/{id}/files endpoint"
```

### Task 5.2: UI — File Tree Panel

**Files:**
- Create: `ui/src/components/FileTreePanel.tsx`
- Modify: `ui/src/services/api.ts`
- Modify: `ui/src/components/ChatInterface.tsx`

**Step 1: Add API method**

```typescript
async getTouchedFiles(conversationId: string): Promise<TouchedFile[]> {
  const response = await fetch(`${this.baseUrl}/conversation/${conversationId}/files`);
  if (!response.ok) throw new Error(`Failed to get files: ${response.statusText}`);
  return response.json();
}
```

**Step 2: Create FileTreePanel component**

A collapsible panel (similar to how TerminalPanel works) that shows:
- Grouped by directory, displayed as a tree
- Each file shows an icon based on operation (📝 patch, 👁 read, 📁 navigate)
- Shows count: "12 files touched"
- Collapsible header: "Files" with toggle
- Refreshes when new tool messages arrive (poll on message count change)

**Step 3: Add to ChatInterface**

Add a files icon button in the header/status bar area. On click, toggle the FileTreePanel.

**Step 4: Build and test manually, commit**
```bash
git add ui/src/
git commit -m "feat: add file tree panel showing touched files"
```

---

## Feature 6: Ollama / Local Model Support

First-class Ollama provider using the OpenAI-compatible API.

### Task 6.1: Add Ollama Provider Type

**Files:**
- Modify: `models/models.go`
- Modify: `server/custom_models.go`

**Step 1: Add provider constant**

In `models/models.go`:
```go
ProviderOllama Provider = "ollama"
```

**Step 2: Add to custom model creation**

In `server/custom_models.go`, add `"ollama"` to the valid provider types list.

In `models/models.go`, add `"ollama"` case in `createServiceFromModel()`:
```go
case "ollama":
	apiKey := model.ApiKey
	if apiKey == "" {
		apiKey = "ollama" // Ollama doesn't require a real key
	}
	endpoint := model.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/v1"
	}
	return &oai.Service{
		Model:    oai.Model{ModelName: model.ModelName, URL: endpoint},
		APIKey:   apiKey,
		ModelURL: endpoint,
		HTTPC:    m.httpc,
	}
```

**Step 3: Commit**
```bash
git add models/ server/
git commit -m "feat: add ollama as first-class provider type"
```

### Task 6.2: Auto-Discovery of Ollama Models

**Files:**
- Create: `models/ollama.go`
- Create: `models/ollama_test.go`
- Modify: `models/models.go`

**Step 1: Write the test**

```go
func TestDiscoverOllamaModels(t *testing.T) {
	// Use httptest.NewServer to mock Ollama's /api/tags endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3.1:latest", "size": 4000000000},
					{"name": "codellama:13b", "size": 7000000000},
				},
			})
		}
	}))
	defer server.Close()

	models, err := DiscoverOllamaModels(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "ollama/llama3.1:latest" {
		t.Fatalf("unexpected ID: %s", models[0].ID)
	}
}
```

**Step 2: Implement DiscoverOllamaModels**

```go
// DiscoverOllamaModels queries an Ollama instance for available models.
// baseURL should be like "http://localhost:11434".
func DiscoverOllamaModels(baseURL string) ([]Model, error) {
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []Model
	for _, m := range result.Models {
		modelName := m.Name
		models = append(models, Model{
			ID:          "ollama/" + modelName,
			Provider:    ProviderOllama,
			Description: fmt.Sprintf("Ollama: %s", modelName),
			Factory: func(config *Config, httpc *http.Client) (llm.Service, error) {
				endpoint := baseURL + "/v1"
				return &oai.Service{
					Model:    oai.Model{ModelName: modelName, URL: endpoint},
					APIKey:   "ollama",
					ModelURL: endpoint,
					HTTPC:    httpc,
				}, nil
			},
		})
	}
	return models, nil
}
```

**Step 3: Integrate into Manager initialization**

In `NewManager()`, after loading built-in and custom models, attempt Ollama discovery with a short timeout (1 second). On failure, log a debug message and continue. Discovered models are added with a lower priority than custom models.

Add `OllamaURL` field to `Config`:
```go
OllamaURL string // Default: "http://localhost:11434", empty to disable
```

**Step 4: Run test, commit**
```bash
git add models/
git commit -m "feat: auto-discover Ollama models at startup"
```

### Task 6.3: CLI Flag and Env Var

**Files:**
- Modify: `cmd/percy/main.go`

**Step 1: Add flag**

Add `--ollama-url` flag (env: `OLLAMA_URL`, default: `http://localhost:11434`). Pass to `models.Config.OllamaURL`.

To disable Ollama discovery, set `--ollama-url=""`.

**Step 2: Commit**
```bash
git add cmd/
git commit -m "feat: add --ollama-url flag for Ollama discovery"
```

### Task 6.4: UI — Ollama in Models Modal

**Files:**
- Modify: `ui/src/components/ModelsModal.tsx`

**Step 1: Update ModelsModal**

The existing ModelsModal already handles custom models with `provider_type` selection. Add `"ollama"` to the provider type dropdown. When selected:
- Pre-fill endpoint with `http://localhost:11434/v1`
- Pre-fill API key with `ollama`
- Show a note: "Ollama models are also auto-discovered if Ollama is running locally"

The auto-discovered models already appear in the model picker (they come from `/api/models`). The ModelsModal change is just for manually adding Ollama models on remote hosts.

**Step 2: Build and test manually, commit**
```bash
git add ui/src/
git commit -m "feat: add Ollama provider option to models modal"
```

---

## Implementation Order Recommendation

These features are independent, but a pragmatic order based on complexity and value:

1. **Feature 6: Ollama** (smallest, most self-contained, immediate value for offline use)
2. **Feature 4: Export** (small, no DB changes, high utility)
3. **Feature 2: Cost Dashboard** (medium, useful for monitoring spend)
4. **Feature 5: File Tree** (medium, no DB changes, nice UX)
5. **Feature 1: Forking** (medium, touches DB + server + UI)
6. **Feature 3: Edit/Regenerate** (most complex, touches the conversation loop lifecycle)

---

## Testing Strategy

- **Go tests**: `go test ./db -v`, `go test ./server -v`, `go test ./models -v` after each backend task
- **UI type-check**: `cd ui && pnpm run type-check` after each UI task
- **Manual testing**: `make build && ./bin/percy --model predictable --db /tmp/percy-test.db serve -port 8002` for each feature
- **E2E consideration**: Add Playwright tests for fork, edit, regenerate, and export flows in `ui/e2e/` if time permits (not blocking)

## Pre-flight Checks

Before starting any task:
1. `make ui` — ensure UI builds
2. `go test ./server ./db ./models` — ensure existing tests pass
3. `cd ui && pnpm run type-check` — ensure no type errors
