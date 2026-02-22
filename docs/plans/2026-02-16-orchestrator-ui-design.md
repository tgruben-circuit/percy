# Orchestrator UI Design: Full Server + Cluster Coordination

Note: Historical planning document (Feb 2026). References may be outdated; see `architecture_nat.md` and `README.md` for current state.

**Date**: 2026-02-16
**Status**: Approved
**Approach**: Merge orchestrate into serve with --cluster flag (Approach A)

## Problem

The cluster coordination layer exists but there's no way to interact with it. The `orchestrate` subcommand doesn't start a web server, workers can't execute tasks, and there's no UI for monitoring. We need the orchestrator to be a full Percy instance you can chat with, plus worker task execution and a cluster dashboard.

## Key Decisions

- **Remove `orchestrate` subcommand** -- `percy serve --cluster :PORT` is the orchestrator
- **Role detection is automatic** -- `:PORT` = orchestrator (embedded NATS), `nats://...` = worker
- **Dispatch via LLM tool** -- orchestrator's LLM gets a `dispatch_tasks` tool when workers are connected
- **Worker execution reuses conversation machinery** -- no new execution engine
- **Git worktrees for isolation** -- each task gets its own worktree, no shared directory conflicts
- **Completion via polling** -- worker polls `IsAgentWorking()` like the subagent pattern
- **Event-driven dependency resolution** -- NATS subscription, not polling
- **Side panel dashboard** -- cluster status in UI, conversation stays clean
- **Codex review after each implementation phase**

## CLI

```bash
# Solo mode (unchanged)
percy serve

# Orchestrator: embeds NATS, full server + coordinator
percy serve --cluster :4222 --agent-name orchestrator --capabilities planning

# Worker: connects to orchestrator, full server + task executor
percy serve --cluster nats://orch-host:4222 --agent-name backend --capabilities go,sql
```

Role detection:
- `--cluster :PORT` = orchestrator (starts embedded NATS)
- `--cluster nats://...` = worker (connects to existing NATS)
- No `--cluster` = solo mode, unchanged

## Orchestrator's Dispatch Tool

When workers are connected, the orchestrator's LLM gets a `dispatch_tasks` tool:

```json
{
  "name": "dispatch_tasks",
  "input": {
    "tasks": [
      {"id": "t1", "title": "Add JWT library", "description": "...", "specialization": ["go"]},
      {"id": "t2", "title": "Update login endpoint", "description": "...", "depends_on": ["t1"]},
      {"id": "t3", "title": "Update auth context", "description": "...", "specialization": ["ts"], "depends_on": ["t1"]}
    ]
  }
}
```

- Only registered when `clusterNode != nil` AND workers are connected
- Builds a `TaskPlan` and calls `orchestrator.SubmitPlan()`
- Returns summary: "Dispatched 3 tasks. t1 submitted immediately, t2 and t3 waiting on t1."

System prompt addition in cluster mode:
> "You are the orchestrator of a cluster of Percy agents. When the user gives you a large task, break it into subtasks and use the dispatch_tasks tool to send them to available workers. Each worker has its own LLM and tools. Describe each subtask clearly -- the worker only sees the task description, not the conversation history."

## Worker Task Execution

### Task Watcher

Background goroutine on each worker:
- Polls for available tasks matching worker's capabilities
- Claims via CAS (submitted → assigned)
- Blocks further claiming while executing (one task at a time)

### Execution Flow

```
Task watcher polls for matching tasks
  |
Claim task (CAS: submitted -> assigned)
  |
Transition task to "working", update agent status
  |
Create git worktree:
  git fetch origin
  git worktree add {tempdir} -b agent/{agentID}/{taskID} origin/{baseBranch}
  |
Create conversation (user_initiated=false) with cwd = worktree path
  |
Insert system message directly into DB:
  "You are a worker agent on branch agent/{id}/{taskID}.
   Do NOT create or switch branches. Your task: {title}. {description}."
  |
Send task description as user message -> AcceptUserMessage()
  |
Poll IsAgentWorking() every 500ms (like SubagentRunner)
  |
On completion:
  - Get last assistant message as summary
  - Detect branch from git state (not LLM output)
  - Tasks.Complete(taskID, result)
  - Locks.ReleaseByAgent(agentID)
  - Registry.UpdateStatus(agentID, idle, "")
  - Clean up worktree: git worktree remove {tempdir}
  |
On error/timeout:
  - Tasks.Fail(taskID, result with error summary)
  - Same cleanup
```

### Key Design Details

**Completion detection**: `Loop.Go()` never returns on its own (infinite poll loop). Worker uses the subagent polling pattern: check `IsAgentWorking()` every 500ms until end-of-turn.

**Git isolation**: Programmatically create git worktrees before execution. Don't rely on LLM to create branches. Each task gets its own directory, solving the user-chat-vs-task race condition.

**System prompt**: `Hydrate()` skips prompt generation for non-user, non-subagent conversations. Worker manually inserts a system message into the DB before calling `AcceptUserMessage()`.

**Task status**: Explicit `assigned -> working` transition before execution begins (add `TaskQueue.SetWorking()` method).

**LLM service**: Use `Server.llmManager.GetService(modelID)`, same path as subagents.

## Orchestrator Dependency Resolution

Background goroutine on the orchestrator:

- Subscribes to NATS subject `task.*.status` for real-time events
- On task completed: calls `ResolveDependencies()` to unblock dependents
- On task failed: retry on another agent if `retries < maxRetries` (default 1), else mark permanently failed
- Runs `MarkStaleAgentsOffline()` every 60s
- Re-queues stuck tasks from dead agents (reset to `submitted`)

### Crash Recovery

When stale agent detected:
1. Find tasks assigned to dead agent (status = assigned or working)
2. Reset those tasks to `submitted`
3. Release all file locks via `ReleaseByAgent()`
4. Add retry counter to prevent infinite loops

## Cluster Dashboard UI

Side panel in the React frontend (hidden in solo mode):

**Enhanced API endpoint** `GET /api/cluster/status`:

```json
{
  "agents": [
    {"id": "agent-abc", "name": "backend", "status": "working", "current_task": "t1", "capabilities": ["go","sql"]}
  ],
  "tasks": [
    {"id": "t1", "title": "Add JWT library", "status": "working", "assigned_to": "agent-abc"},
    {"id": "t2", "title": "Update login", "status": "submitted", "depends_on": ["t1"]},
    {"id": "t3", "title": "Update auth context", "status": "completed", "result": {"branch": "agent/xyz/t3"}}
  ],
  "plan_summary": {"total": 3, "submitted": 1, "working": 1, "completed": 1, "failed": 0}
}
```

**UI features:**
- Agent cards with status indicators (idle/working/offline)
- Task list with dependency arrows and status badges
- Auto-refreshes via SSE (cluster status broadcast through existing subpub)
- Only visible when cluster mode is active

## What's NOT in This Phase

- Parallel task execution per worker (future: multiple worktrees)
- LLM-assisted merge conflict resolution (designed previously, not wired yet)
- A2A external gateway
- Per-task timeout configuration (uses default 12h loop timeout)
