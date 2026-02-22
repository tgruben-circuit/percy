# Merge Pipeline Design: LLM-Assisted Conflict Resolution

Note: Historical planning document (Feb 2026). References may be outdated; see `architecture_nat.md` and `README.md` for current state.

**Date**: 2026-02-16
**Status**: Approved

## Problem

Workers complete tasks on isolated branches but nothing merges them back. The orchestrator needs a merge pipeline that integrates worker branches into the user's working branch, with LLM-assisted conflict resolution.

## Key Decisions

- **Merge into working branch** (not main) -- user PRs working branch into main
- **Dedicated merge worktree** -- merges happen in their own worktree, not the server's working directory
- **Explicit working branch** -- stored at orchestrator creation, not derived from HEAD at merge time
- **Transactional merge** -- `git merge --abort` on any failure, task requeued for retry
- **LLM resolves conflicts** with ours/theirs/base context, 2-min timeout, always auto-commits
- **Sequential merges** -- one at a time, NATS callback serialization prevents races
- **Single machine** -- workers and orchestrator share a git repo (no push required)
- **Codex review after each implementation phase**

## Merge Pipeline

### Orchestrator Initialization

```
1. Record workingBranch = git rev-parse --abbrev-ref HEAD
2. Create persistent merge worktree:
   git worktree add /tmp/percy-merge-{agentID} workingBranch
```

### On Task Completion (Monitor Event)

```
1. In merge worktree: git merge agent/worker-1/t1 --no-ff
2. If clean merge:
   - Commit
   - TaskResult.MergeStatus = "merged"
3. If conflict:
   - Save HEAD: git rev-parse HEAD
   - For each conflicted file, call LLM
   - If ANY resolution fails -> git merge --abort -> Requeue task
   - If all resolved -> commit -> MergeStatus = "conflict_resolved"
4. After merge:
   - git branch -d agent/worker-1/t1 (cleanup worker branch)
   - TaskResult.MergeCommit = git rev-parse HEAD
5. Call ResolveDependencies() (only considers tasks with MergeStatus set)
```

### On Orchestrator Shutdown

```
git worktree remove /tmp/percy-merge-{agentID}
```

## LLM Conflict Resolution

### Per-File Resolution Flow

```
Skip if binary (git diff --numstat shows "-")

Gather context:
  - ours:   git show HEAD:{path}
  - theirs: git show {branchName}:{path}
  - base:   git show $(git merge-base HEAD {branchName}):{path}
  - task description (what the worker was trying to do)

Call LLM with 2-minute timeout:
  "Resolve this merge conflict for {path}.

   The worker was doing: {task.Title}: {task.Description}

   BASE version (before either change):
   {base content}

   OURS version (working branch):
   {ours content}

   THEIRS version (worker's change):
   {theirs content}

   Output ONLY the resolved file content. No explanation, no markdown
   fences, no line numbers. Just the raw file content."

Parse response, write to file, git add {path}

If LLM call fails or times out -> git merge --abort -> Requeue task
```

### After All Files Resolved

```
git commit (merge commit message includes task title)
TaskResult.MergeStatus = "conflict_resolved"
TaskResult.MergeCommit = git rev-parse HEAD
```

## TaskResult Changes

```go
type TaskResult struct {
    Branch      string `json:"branch"`
    Summary     string `json:"summary"`
    MergeStatus string `json:"merge_status,omitempty"` // "merged", "conflict_resolved", "merge_failed"
    MergeCommit string `json:"merge_commit,omitempty"`
}
```

## Remaining Wiring

### Start Monitor from Server

`startClusterMonitor()` on Server:
- Only runs on orchestrator node (check `IsOrchestrator()`)
- Creates Orchestrator with explicit working branch
- Starts Monitor in background goroutine tied to shutdownCh

### System Prompt Enhancement

When `clusterNode != nil` and workers are connected, append to system prompt:
> "You are the orchestrator of a cluster of Percy agents. Use the dispatch_tasks tool to break large tasks into subtasks for available workers."

### Node.IsOrchestrator()

Expose embedded NATS check: `func (n *Node) IsOrchestrator() bool { return n.embedded != nil }`

## What's NOT in This Phase

- Post-merge validation (compile check, tests)
- Multi-machine branch pushing
- Push to remote (user controls)
