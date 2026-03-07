# Provider-Independent Programmatic Tool Calling with Mid-Conversation Model Switching (Design)

Date: 2026-03-07
Status: Approved

## Goals

1. Keep tool calling LLM-independent via Percy’s canonical internal protocol (`llm.Message`, `llm.Content`, `llm.Tool`).
2. Allow explicit mid-conversation model switching without losing context or tool-call continuity.
3. Ensure provider choice at conversation start does not lock the conversation forever.

## Non-Goals

1. Automatic model switching by agent.
2. Provider-specific feature negotiation in V1.
3. Compatibility shims for legacy behavior.

## Current State Summary

- Percy already uses a canonical internal request/response and tool-call representation in `llm/`.
- Provider adapters (`llm/ant`, `llm/oai`, `llm/gem`) translate to/from provider wire formats.
- `loop` executes tools from canonical `ContentTypeToolUse` and returns canonical `ContentTypeToolResult`.
- `ConversationManager` currently rejects model changes (`errConversationModelMismatch`).

## Proposed Architecture

Adopt explicit model switching at the conversation manager layer while preserving the canonical transcript format.

### New API

`POST /api/conversation/<id>/switch-model`

Request:

```json
{
  "model": "gpt-5.3-codex",
  "cancel_current_turn": false
}
```

Response:

- `200` on success
- `400` unknown/unavailable model
- `409` conversation is actively working and `cancel_current_turn` is false
- `500` internal failures

### Conversation Manager Changes

Add `SwitchModel(ctx, service, modelID, cancelCurrentTurn)` as the single explicit path.

Behavior:

1. Validate service/model.
2. If agent is active:
   - if `cancelCurrentTurn == false`: fail `409`
   - else cancel current loop and continue
3. Stop loop + cleanup toolset.
4. Rebuild loop from DB history/system prompt using target model service.
5. Persist `conversations.model = modelID`.
6. Broadcast conversation state update (`ConversationState.Model`).

### Loop/Tool Calling

No protocol change required:

- Keep tool definitions and results canonical.
- Continue adapter translation only at provider boundaries.
- Continue `insertMissingToolResults` repair logic before provider requests.

## Data Flow

1. UI sends switch request.
2. Server handler resolves target `llm.Service` from `llmManager`.
3. Server calls `ConversationManager.SwitchModel(...)`.
4. Manager performs guarded stop/rebuild/persist/broadcast.
5. Next user turn runs through the new provider with unchanged conversation context.

## Error Handling

- Unknown model: fail fast (`400`).
- Active loop without cancel flag: fail fast (`409`).
- Rebuild failure: fail (`500`), loop remains stopped; user must retry switch/send.
- DB persistence failure after rebuild: fail and stop rebuilt loop to avoid split-brain runtime-vs-DB model state.

No fallback behavior.

## Testing Plan

### Server/Manager

1. Switch model on idle conversation succeeds and persists.
2. Active conversation switch without cancel flag returns conflict.
3. Active conversation switch with cancel flag succeeds.
4. Next turn after switch uses new model.
5. SSE publishes updated conversation state model.

### Tool-Call Continuity

1. Conversation containing prior tool_use/tool_result history remains valid after switching providers.
2. Existing history repair logic (`insertMissingToolResults`) still protects provider contracts post-switch.

### UI

1. Model switch control sends request and handles success/error states.
2. While active: enforce explicit cancel-and-switch path.
3. Model indicator updates from SSE.

## Tradeoff Rationale

This uses the lowest-risk path:

- Minimal changes to stable loop/tool protocol.
- Explicit user intent for switching.
- Maintains one canonical transcript and adapter-based provider abstraction.

A service-proxy hot-swap approach is deferred due to higher concurrency complexity.
