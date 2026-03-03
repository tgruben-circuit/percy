# Co-Command Skills via Subagents

**Date:** 2026-03-03
**Status:** Approved

## Goal

Port the [claude-co-commands](https://github.com/SnakeO/claude-co-commands) skills to Percy, using Percy's existing subagent system instead of MCP/Codex. This gives Percy users a "second opinion" from a different model during brainstorming, planning, and validation.

## Approach

**Approach A (selected):** Add an optional `model` parameter to the existing `subagent` tool. Co-command skills instruct the agent to pass this parameter when spawning subagents. General-purpose — any skill can use different-model subagents.

Alternatives considered:
- **B: Separate `co_agent` tool** — rejected, duplicates subagent infrastructure, violates "one way of doing things"
- **C: Skill-declared `subagent-model` frontmatter** — rejected, requires implicit "active skill" state tracking that doesn't exist

## Design

### 1. Subagent Model Override

Add optional `model` parameter to the `subagent` tool input schema. When provided, `server/subagent.go` uses `llmManager.GetService(overrideModel)` instead of `s.defaultModel`. If not provided, behavior is unchanged (inherits parent model).

The `SubagentRunner` interface gains the model parameter:
```go
RunSubagent(ctx, conversationID, prompt string, wait bool, timeout time.Duration, model string) (string, error)
```

### 2. Three Co-Command Skills

Default subagent model: `gpt-5.3-codex`

All three follow the bias-prevention pattern: spawn background subagent, think independently, compare results.

**`co-brainstorm`** — Interactive idea exploration
1. Spawn subagent: `wait: false`, `model: gpt-5.3-codex`, prompt: brainstorm independently on topic
2. Parent brainstorms in parallel
3. Parent retrieves subagent ideas via follow-up message to same slug
4. Compare and synthesize best of both

**`co-plan`** — Parallel planning
1. Spawn subagent: `wait: false`, `model: gpt-5.3-codex`, prompt: create implementation plan
2. Parent creates own plan independently
3. Retrieve subagent plan
4. Compare, identify missed approaches or simpler alternatives

**`co-validate`** — Staff engineer review
1. Read plan file, spawn subagent: `wait: false`, `model: gpt-5.3-codex`, prompt: review plan critically
2. Parent does own independent review
3. Retrieve subagent review
4. Reconcile — accept valid concerns, override with explanation where disagreed

### 3. Configuration

The skill markdown is the configuration. Each SKILL.md instructs the agent to use `model: gpt-5.3-codex`. Users can override by:
- Placing a higher-priority skill in `~/.config/percy/` (overrides bundled)
- Telling the agent directly to use a different model

### 4. Error Handling

- **Model not available:** `GetService()` returns error. Skill instructs agent to tell user what's needed (e.g. OpenAI API key). No fallback to same-model.
- **Subagent timeout:** Existing 300s max timeout + progress summary mechanism. Parent can re-message the slug to continue.

## Files Changed

| File | Change |
|------|--------|
| `claudetool/subagent.go` | Add `model` to input schema, struct, pass through `RunSubagent()` |
| `server/subagent.go` | Accept model override, use in `GetService()` when provided |
| `bundled_skills/co-brainstorm/SKILL.md` | New — brainstorming workflow |
| `bundled_skills/co-plan/SKILL.md` | New — parallel planning workflow |
| `bundled_skills/co-validate/SKILL.md` | New — staff engineer review workflow |

No database changes, no API changes, no UI changes.
