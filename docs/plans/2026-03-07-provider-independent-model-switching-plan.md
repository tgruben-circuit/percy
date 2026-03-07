# Provider-Independent Model Switching Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow explicit mid-conversation model switching while keeping canonical provider-independent tool-calling behavior.

**Architecture:** Add a dedicated switch-model API that delegates to `ConversationManager.SwitchModel`, which performs guarded loop stop/rebuild against the same canonical transcript and persists the new model. Keep all tool-call schemas and loop semantics unchanged.

**Tech Stack:** Go (net/http, Percy server/loop/llm abstractions), React/TypeScript UI, SQLite via existing db layer, SSE conversation state updates.

---

### Task 1: Add server API contract for model switching

**Files:**
- Modify: `server/handlers.go`
- Test: `server/*_test.go` (new handler tests in existing style)

**Step 1: Write failing handler test for happy path**

- Add test for `POST /api/conversation/<id>/switch-model` with valid model.
- Assert `200` and response body includes switched model ID (or status success payload).

**Step 2: Run test to verify failure**

Run: `go test ./server -run SwitchModel -v`
Expected: FAIL because endpoint/handler is missing.

**Step 3: Implement request/response types + route + handler**

- Add `SwitchModelRequest` struct:
  - `Model string`
  - `CancelCurrentTurn bool`
- Wire route in server mux.
- Resolve model service via `llmManager.GetService`.
- Call manager switch method.
- Return clear status codes (`400/409/500/200`).

**Step 4: Re-run tests**

Run: `go test ./server -run SwitchModel -v`
Expected: PASS for happy-path test.

**Step 5: Commit**

```bash
git add server/handlers.go server/*_test.go
git commit -m "server: add switch-model conversation endpoint"
```

---

### Task 2: Implement conversation manager switch behavior

**Files:**
- Modify: `server/convo.go`
- Test: `server/conversation_state_test.go` and/or new focused manager tests

**Step 1: Write failing tests for manager behavior**

Add tests covering:
1. Switch while idle succeeds and updates model.
2. Switch while active with `cancel_current_turn=false` returns conflict.
3. Switch while active with cancel flag succeeds.

**Step 2: Run tests to verify failure**

Run: `go test ./server -run ConversationModelSwitch -v`
Expected: FAIL because switch method/semantics don’t exist.

**Step 3: Implement `SwitchModel` in `ConversationManager`**

- Add single explicit method with signature similar to:
  - `SwitchModel(ctx context.Context, service llm.Service, modelID string, cancelCurrentTurn bool) error`
- If active and cancel not requested: return dedicated conflict error.
- Stop loop/toolset.
- Recreate loop using existing DB history/system prompt through existing setup path.
- Persist conversation model immediately after successful rebuild.
- Broadcast updated state (`ConversationState.Model`).

**Step 4: Update mismatch logic**

- Remove/replace hard mismatch rejection in `ensureLoop` for explicit switch path.
- Preserve guardrails so normal chat path does not silently auto-switch.

**Step 5: Re-run tests**

Run: `go test ./server -run ConversationModelSwitch -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add server/convo.go server/*_test.go
git commit -m "server: support explicit conversation model switching"
```

---

### Task 3: Ensure canonical transcript continuity across provider switch

**Files:**
- Modify/Test: `server/*_test.go` (integration-style conversation test)
- Possibly verify no code changes needed in `loop/loop.go`

**Step 1: Write failing continuity test**

- Build conversation history containing assistant tool_use + user tool_result blocks.
- Switch model to another provider-backed service.
- Send next user message.
- Assert request processing continues and tool-call handling remains valid.

**Step 2: Run test to verify failure (if any)**

Run: `go test ./server -run ToolCallContinuityAfterModelSwitch -v`
Expected: FAIL if edge cases exist.

**Step 3: Apply minimal fix if needed**

- Prefer no protocol changes.
- If needed, tighten manager rebuild sequencing only.

**Step 4: Re-run tests**

Run: `go test ./server -run ToolCallContinuityAfterModelSwitch -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add server/*_test.go server/convo.go loop/loop.go
git commit -m "test: verify tool-call continuity across model switch"
```

---

### Task 4: Add UI model-switch action

**Files:**
- Modify: `ui/src/...` (conversation header/model picker components + API client)
- Check guidance before edit in `ui/src/components/AGENTS.md`
- Test: relevant UI tests (if present) or add focused ones

**Step 1: Write failing UI/API test**

- Test switch-model request payload and success handling.
- Test active-conversation guard path (show explicit cancel-and-switch action).

**Step 2: Run UI test/type-check to verify failure**

Run:
- `cd ui && pnpm run type-check`
- `cd ui && pnpm run lint`
Expected: failure before implementation.

**Step 3: Implement UI control + API call**

- Add switch action in model selector/header.
- When working:
  - default action blocked (or prompts explicit cancel-and-switch path via existing UI components; no alert/confirm/prompt).
- Trigger backend endpoint and handle errors clearly.

**Step 4: Re-run UI checks**

Run:
- `cd ui && pnpm run type-check`
- `cd ui && pnpm run lint`
Expected: PASS.

**Step 5: Commit**

```bash
git add ui/src
git commit -m "ui: add explicit model switch action for conversations"
```

---

### Task 5: End-to-end verification and regression checks

**Files:**
- Modify tests as needed

**Step 1: Build UI bundle required for Go tests**

Run: `make ui`
Expected: success, `ui/dist` generated.

**Step 2: Run targeted Go tests**

Run: `go test ./server`
Expected: PASS.

**Step 3: Run full UI checks**

Run:
- `cd ui && pnpm run type-check`
- `cd ui && pnpm run lint`
Expected: PASS.

**Step 4: Smoke test manually (optional but recommended)**

- Start test instance with predictable model and a second model available.
- Switch model mid-conversation via UI.
- Confirm next turn uses new model and state updates in UI.

**Step 5: Final commit (if any remaining changes)**

```bash
git add -A
git commit -m "test: add model switching coverage and finalize integration"
```
