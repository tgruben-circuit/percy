# Percy: a coding agent

Percy is a fork of [Shelley](https://github.com/boldsoftware/shelley) — an awesome foundation built by [Bold Software](https://github.com/boldsoftware) — with a bunch of new features on top.

Percy is a mobile-friendly, web-based, multi-conversation, multi-modal,
multi-model, single-user coding agent. It does not come with authorization or sandboxing:
bring your own.

*Mobile-friendly* because ideas can come any time.

*Web-based*, because terminal-based scroll back is punishment for shoplifting in some countries.

*Multi-modal* because screenshots, charts, and graphs are necessary, not to mention delightful.

*Multi-model* to benefit from all the innovation going on.

*Single-user* because it makes sense to bring the agent to the compute.

## What's new in Percy

### Memory Search

Percy remembers past conversations. After each conversation ends, messages are automatically chunked and indexed into a separate memory database. The agent can recall earlier decisions, code changes, and context using the `memory_search` tool. Supports hybrid search combining FTS5 keyword matching with optional vector embeddings (Ollama, or FTS5-only with zero dependencies).

### LSP Code Intelligence

Compiler-accurate code navigation powered by Language Server Protocol. The `code_intelligence` tool gives the agent five operations: **definition**, **references**, **hover**, **symbols**, and **diagnostics**. Works with Go (gopls), TypeScript, Python (pyright), Rust (rust-analyzer), and other LSP-enabled languages.

### Bundled Skills

14 workflow skills ship embedded in the binary, covering test-driven development, systematic debugging, brainstorming, plan writing and execution, code review, git worktrees, parallel agent dispatch, and more. Skills follow the [Agent Skills](https://agentskills.io) specification and can be overridden by user or project-level skills.

### Skills Discovery

Percy discovers skills from multiple sources: bundled skills, user config directories (`~/.config/percy/`, `~/.percy/`), shared agent skills (`~/.config/agents/skills/`), project `.skills/` directories, and SKILL.md files anywhere in the project tree. Use the `/skills` command to see what's available.

### Context Window Management

Proactive monitoring of LLM context usage with warnings at 80% capacity, automatic retry on response truncation (up to 2 retries), and increased max output tokens (16,384) for longer responses.

### Conversation Distillation

When a conversation gets long, Percy can distill it into an operational brief and continue in a fresh conversation. The distillation preserves files modified, decisions made, current state, and next steps — everything the agent needs to pick up where it left off.

### Notification Channels

Get notified when the agent finishes work. Supports Discord webhooks and email, with a test endpoint to verify connectivity. Channels are configurable via the API and persist in the database.

## Installation

### Pre-Built Binaries (macOS/Linux)

```bash
curl -Lo percy "https://github.com/tgruben-circuit/percy/releases/latest/download/percy_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" && chmod +x percy
```

The binaries are on the [releases page](https://github.com/tgruben-circuit/percy/releases/latest).

### Homebrew (macOS)

```bash
brew install --cask tgruben-circuit/tap/percy
```

### Build from Source

You'll need Go and Node.

```bash
git clone https://github.com/tgruben-circuit/percy.git
cd percy
make
```

## Architecture

The technical stack is Go for the backend, SQLite for storage, and Typescript
and React for the UI.

The data model is that Conversations have Messages, which might be from the
user, the model, the tools, or the harness. All of that is stored in the
database, and we use a SSE endpoint to keep the UI updated.

## Acknowledgments

Percy stands on the shoulders of [Shelley](https://github.com/boldsoftware/shelley) by [Bold Software](https://github.com/boldsoftware). Shelley provided the solid core — the agentic loop, multi-model LLM integration, tool execution, SSE-based UI, and the mobile-friendly web interface. We're grateful for that foundation.

Shelley was itself partially based on [Sketch](https://github.com/boldsoftware/sketch).

## Open source

Percy is Apache licensed. We require a CLA for contributions.

## Building Percy

Run `make`. Run `make serve` to start Percy locally.

### Dev Tricks

If you want to see how mobile looks, and you're on your home
network where you've got mDNS working fine, you can
run

```
socat TCP-LISTEN:9001,fork TCP:localhost:9000
```
