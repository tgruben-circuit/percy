# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Percy?

Percy is a mobile-friendly, multi-conversation, multi-modal, multi-model, single-user AI coding agent. Go backend, SQLite storage, React/TypeScript web UI, Bubble Tea terminal UI. Module path: `github.com/tgruben-circuit/percy`.

## Build & Development Commands

```bash
make build          # Build UI + templates + Go binary (to bin/percy)
make ui             # Build UI only (cd ui && pnpm install --frozen-lockfile && pnpm run build)
make templates      # Build template tarballs
make serve          # Run server locally (go run ./cmd/percy serve)
make serve-test     # Run with predictable model (no API key needed)
make test           # Run all tests (Go + E2E)
make test-go        # Go tests only (requires UI built first!)
make test-e2e       # Playwright E2E tests (headless)
```

**IMPORTANT**: Build the UI (`make ui`) before running Go tests — `ui/dist` must exist for the embed.

### Targeted testing

```bash
go test ./server              # Run tests for a single Go package
go test ./loop                # Run loop tests
go test ./claudetool          # Run tool tests
go test ./tui                 # Run TUI client tests
cd ui && pnpm run type-check  # TypeScript type checking
cd ui && pnpm run lint        # ESLint
cd ui && pnpm run test:e2e -- --grep "smoke"  # Run specific E2E tests
```

### Running a test instance

```bash
./bin/percy --model predictable --db test.db serve --port 8002
```

### Code generation

- **sqlc**: `sqlc generate` — schema in `db/schema/`, queries in `db/query/`, output in `db/generated/`
- **Go→TS types**: `cd ui && pnpm run generate-types`
- SQL migrations and frontend changes require rebuilding (`make build`)

## Architecture

```
cmd/percy/            Thin CLI entry point
server/               HTTP API server, ConversationManager, SSE broadcasting
loop/                 Core agentic loop — ProcessOneTurn() calls LLM, executes tools, records messages
claudetool/           Tool implementations: bash, patch, keyword, browse, changedir, output_iframe, subagent
llm/                  LLM abstraction layer with provider implementations:
  llm.go                Core interfaces (Service, Request, Response, Message, Tool)
  ant/                  Anthropic Claude
  oai/                  OpenAI
  gem/                  Google Gemini
models/               Model discovery, capabilities, and selection
db/                   SQLite via sqlc — conversations and messages tables
subpub/               Pub/sub for SSE real-time updates
ui/                   React/TypeScript frontend (esbuild, pnpm)
tui/                  Bubble Tea terminal UI — pure HTTP/SSE client (no server imports)
templates/            Project boilerplate templates (packaged as .tar.gz)
```

### Request flow

1. User sends message via web UI or TUI → POST `/api/conversation/<id>/chat`
2. Server queues message in ConversationManager (`server/convo.go`)
3. Loop (`loop/loop.go`) calls LLM with conversation history + system prompt
4. LLM responds with text and/or tool calls
5. Loop executes tools, records results as messages
6. All messages broadcast via SSE (`subpub/`) to UI subscribers

### Data model

- **Conversations** have **Messages** (types: user, agent, tool)
- Messages store `llm_data`, `user_data`, `usage_data` as JSON
- Subagent conversations use `user_initiated=false`

### Predictable model

A test fixture that returns pre-specified responses — useful for interactive testing and E2E tests without needing an LLM API key. Launch with `--model predictable`.

## Code Conventions (from AGENTS.md)

- **Brevity**: One way of doing things; refactor relentlessly.
- **Error handling**: Propagate errors, exit, or crash. No fallbacks.
- **No compatibility shims**: This is a new project — delete old code, don't keep it around.
- **No sleeps in tests**.
- **No alert()/confirm()/prompt()** in UI — use proper components.
- **React input automation**: Must use native setter pattern (see AGENTS.md #9) — `input.value = '...'` won't work.
- **Be careful with pkill**: If running under Percy, `pkill -f percy` will kill the parent process.
