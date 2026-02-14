---
name: opencode
description: Run OpenCode CLI for one-shot tasks and generate opencode.json project configuration. Use when delegating work to OpenCode or setting up a project for OpenCode usage.
allowed-tools: bash, patch
---

# OpenCode

## One-Shot Execution

Run OpenCode non-interactively with the `run` subcommand:

```bash
opencode run "your task here"
```

All permissions are auto-approved in run mode.

### Key Flags

| Flag | Description |
|------|-------------|
| `"prompt"` | Positional argument, the task to execute |
| `-m provider/model` | Select model (e.g., `anthropic/claude-sonnet-4-5`) |
| `--format default\|json` | Output format |
| `--file path` | Attach file(s) to the message |
| `--continue` | Resume most recent session |
| `--session id` | Continue a specific session |
| `--agent name` | Choose a specific agent |

### Examples

```bash
# Quick task
opencode run "Add input validation to the user registration endpoint"

# With model selection
opencode run -m anthropic/claude-sonnet-4-5 "Refactor the database layer"

# Attach files for context
opencode run --file schema.sql "Generate Go structs from this SQL schema"

# JSON output
opencode run --format json "List all exported functions in the api package"
```

### Long-Running Tasks

For tasks that may take a while, use tmux:

```bash
tmux new-session -d -s opencode-task 'opencode run "Build comprehensive test coverage"; echo "DONE"'
tmux attach -t opencode-task
```

## Project Configuration

### opencode.json

Create `opencode.json` at the project root:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5",
  "instructions": ["CONTRIBUTING.md", "docs/guidelines.md"],
  "tools": {
    "write": true,
    "bash": true,
    "edit": true
  },
  "permission": {
    "edit": "auto",
    "bash": "ask"
  },
  "compaction": {
    "auto": true,
    "prune": true,
    "reserved": 10000
  },
  "watcher": {
    "ignore": ["node_modules/**", "dist/**", ".git/**"]
  }
}
```

### AGENTS.md

OpenCode's primary context file is `AGENTS.md` at the project root. It falls back to `CLAUDE.md` if `AGENTS.md` doesn't exist. Include the same kind of content as CLAUDE.md:

- Build and test commands
- Architecture overview
- Code conventions
- Important paths

The `instructions` array in `opencode.json` can reference additional files:

```json
{
  "instructions": ["CONTRIBUTING.md", "docs/arch.md", ".cursor/rules/*.md"]
}
```

### File Precedence (lowest to highest)

1. Global: `~/.config/opencode/opencode.json`
2. Custom: `OPENCODE_CONFIG` env var
3. Project: `opencode.json` at project root
4. Inline: `OPENCODE_CONFIG_CONTENT` env var
