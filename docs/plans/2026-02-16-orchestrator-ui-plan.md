# Orchestrator UI Implementation Plan

Note: Historical planning document (Feb 2026). References may be outdated; see `architecture_nat.md` and `README.md` for current state.

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the orchestrator a full Percy server with dispatch tool, worker task execution, and cluster dashboard.

**Architecture:** Remove `orchestrate` subcommand; `--cluster` flag on `serve` handles everything. Add `dispatch_tasks` LLM tool, worker task watcher with git worktree isolation, event-driven dependency resolution, and React dashboard panel.

**Tech Stack:** Go, NATS JetStream, existing Percy loop/server/claudetool, React/TypeScript UI.

**Review process:** Codex code review after each phase.

---

## Phase 1: CLI Simplification

### Task 1.1: Remove orchestrate subcommand

**Files:**
- Modify: `cmd/percy/main.go`

**Step 1: Remove orchestrate usage string**

Remove the line:
```go
fmt.Fprintf(flag.CommandLine.Output(), "  orchestrate [flags]           Start cluster orchestrator\n")
```

**Step 2: Remove orchestrate case from switch**

Remove:
```go
case "orchestrate":
    runOrchestrate(global, args[1:])
```

**Step 3: Remove runOrchestrate function**

Delete the entire `runOrchestrate` function (lines 224-267).

**Step 4: Clean up unused imports**

After removing `runOrchestrate`, check if `os/signal` and `syscall` are still used. They are NOT used by `runServe` (the server handles signals internally in `Server.Start()`). Remove them from the import block. Keep `path/filepath` (used in the cluster block of `runServe`).

**Step 5: Verify build**

Run: `go build ./cmd/percy/`
Expected: Clean build.

**Step 6: Verify existing behavior**

Run: `go build ./... && go test ./... -count=1 -timeout 120s`
Expected: All pass.

**Step 7: Commit**

```bash
git add cmd/percy/main.go
git commit -m "refactor(cli): remove orchestrate subcommand, --cluster flag on serve is sufficient"
```

---

### Task 1.2: Phase 1 review

Codex review: verify orchestrate is fully removed, no dead code, imports clean, serve --cluster still works.

---

## Phase 2: Task Status Transitions & Retry Support

### Task 2.1: Add SetWorking and retry counter to task model

**Files:**
- Modify: `cluster/task.go`
- Modify: `cluster/task_test.go`

**Step 1: Add Retries field to Task struct and SetWorking method**

In `cluster/task.go`, add `Retries` field to `Task`:

```go
type Task struct {
    // ... existing fields ...
    Retries    int        `json:"retries"`
}
```

Add `SetWorking` method to `TaskQueue`:

```go
// SetWorking transitions a task from assigned to working.
func (tq *TaskQueue) SetWorking(ctx context.Context, taskID string) error {
    kv, err := tq.taskKV(ctx)
    if err != nil {
        return err
    }

    entry, err := kv.Get(ctx, taskID)
    if err != nil {
        return fmt.Errorf("get task %s: %w", taskID, err)
    }

    var task Task
    if err := json.Unmarshal(entry.Value(), &task); err != nil {
        return fmt.Errorf("unmarshal task: %w", err)
    }

    if task.Status != TaskStatusAssigned {
        return fmt.Errorf("task %s is %s, not assigned", taskID, task.Status)
    }

    task.Status = TaskStatusWorking
    task.UpdatedAt = time.Now()

    data, err := json.Marshal(task)
    if err != nil {
        return fmt.Errorf("marshal task: %w", err)
    }

    if _, err := kv.Update(ctx, taskID, data, entry.Revision()); err != nil {
        return fmt.Errorf("set working task %s: %w", taskID, err)
    }

    tq.nc.Publish("task."+task.ID+".status", data)
    return nil
}
```

Add `Requeue` method for failed/stuck tasks:

```go
// Requeue resets a task to submitted, incrementing the retry counter.
func (tq *TaskQueue) Requeue(ctx context.Context, taskID string) error {
    kv, err := tq.taskKV(ctx)
    if err != nil {
        return err
    }

    entry, err := kv.Get(ctx, taskID)
    if err != nil {
        return fmt.Errorf("get task %s: %w", taskID, err)
    }

    var task Task
    if err := json.Unmarshal(entry.Value(), &task); err != nil {
        return fmt.Errorf("unmarshal task: %w", err)
    }

    task.Status = TaskStatusSubmitted
    task.AssignedTo = ""
    task.Retries++
    task.UpdatedAt = time.Now()

    data, err := json.Marshal(task)
    if err != nil {
        return fmt.Errorf("marshal task: %w", err)
    }

    if _, err := kv.Update(ctx, taskID, data, entry.Revision()); err != nil {
        return fmt.Errorf("requeue task %s: %w", taskID, err)
    }

    tq.nc.Publish("task."+task.ID+".status", data)
    return nil
}
```

**Step 2: Write tests**

In `cluster/task_test.go`, add:

```go
func TestSetWorking(t *testing.T) {
    tq, ctx := setupTestTaskQueue(t)

    tq.Submit(ctx, Task{ID: "t1", Type: TaskTypeImplement, Title: "test", CreatedBy: "orch"})
    tq.Claim(ctx, "t1", "agent-1")
    if err := tq.SetWorking(ctx, "t1"); err != nil {
        t.Fatal(err)
    }

    got, _ := tq.Get(ctx, "t1")
    if got.Status != TaskStatusWorking {
        t.Fatalf("got %q, want working", got.Status)
    }
}

func TestSetWorkingRejectsNonAssigned(t *testing.T) {
    tq, ctx := setupTestTaskQueue(t)

    tq.Submit(ctx, Task{ID: "t1", Type: TaskTypeImplement, Title: "test", CreatedBy: "orch"})
    err := tq.SetWorking(ctx, "t1")
    if err == nil {
        t.Fatal("expected error on non-assigned task")
    }
}

func TestRequeue(t *testing.T) {
    tq, ctx := setupTestTaskQueue(t)

    tq.Submit(ctx, Task{ID: "t1", Type: TaskTypeImplement, Title: "test", CreatedBy: "orch"})
    tq.Claim(ctx, "t1", "agent-1")
    tq.SetWorking(ctx, "t1")
    tq.Fail(ctx, "t1", TaskResult{Summary: "crashed"})

    if err := tq.Requeue(ctx, "t1"); err != nil {
        t.Fatal(err)
    }

    got, _ := tq.Get(ctx, "t1")
    if got.Status != TaskStatusSubmitted {
        t.Fatalf("got %q, want submitted", got.Status)
    }
    if got.Retries != 1 {
        t.Fatalf("got retries %d, want 1", got.Retries)
    }
    if got.AssignedTo != "" {
        t.Fatalf("got assigned_to %q, want empty", got.AssignedTo)
    }
}
```

**Step 3: Run tests**

Run: `go test ./cluster/ -run "TestSetWorking|TestRequeue" -v`
Expected: All PASS.

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add SetWorking transition and Requeue with retry counter"
```

---

### Task 2.2: Phase 2 review

Codex review: CAS on SetWorking, retry counter increments, status transition validation.

---

## Phase 3: Worker Task Watcher

### Task 3.1: Task watcher goroutine

**Files:**
- Create: `cluster/worker.go`
- Create: `cluster/worker_test.go`

**Step 1: Write test for task watcher claiming a task**

```go
// cluster/worker_test.go
package cluster

import (
    "context"
    "sync"
    "testing"
    "time"
)

func TestWorkerClaimsTask(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    node, err := StartNode(ctx, NodeConfig{
        AgentID: "worker-1", AgentName: "backend",
        Capabilities: []string{"go"},
        ListenAddr: ":0", StoreDir: t.TempDir(),
    })
    if err != nil {
        t.Fatal(err)
    }
    defer node.Stop()

    var claimed Task
    var mu sync.Mutex
    handler := func(ctx context.Context, task Task) TaskResult {
        mu.Lock()
        claimed = task
        mu.Unlock()
        return TaskResult{Branch: "test-branch", Summary: "done"}
    }

    w := NewWorker(node, handler)
    go w.Run(ctx)

    // Submit a task matching worker's capabilities
    node.Tasks.Submit(ctx, Task{
        ID: "t1", Type: TaskTypeImplement, Title: "Test task",
        Specialization: []string{"go"}, CreatedBy: "orch",
    })

    // Wait for worker to claim and execute
    deadline := time.After(5 * time.Second)
    for {
        select {
        case <-deadline:
            t.Fatal("worker did not claim task within 5s")
        default:
        }
        task, err := node.Tasks.Get(ctx, "t1")
        if err != nil {
            continue
        }
        if task.Status == TaskStatusCompleted {
            mu.Lock()
            if claimed.ID != "t1" {
                t.Fatalf("claimed wrong task: %q", claimed.ID)
            }
            mu.Unlock()
            return
        }
        time.Sleep(100 * time.Millisecond)
    }
}

func TestWorkerSkipsNonMatchingTask(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    node, err := StartNode(ctx, NodeConfig{
        AgentID: "worker-1", AgentName: "frontend",
        Capabilities: []string{"ts", "react"},
        ListenAddr: ":0", StoreDir: t.TempDir(),
    })
    if err != nil {
        t.Fatal(err)
    }
    defer node.Stop()

    handler := func(ctx context.Context, task Task) TaskResult {
        t.Fatal("should not be called for non-matching task")
        return TaskResult{}
    }

    w := NewWorker(node, handler)
    go w.Run(ctx)

    // Submit a Go task -- worker only handles ts/react
    node.Tasks.Submit(ctx, Task{
        ID: "t1", Type: TaskTypeImplement, Title: "Go task",
        Specialization: []string{"go"}, CreatedBy: "orch",
    })

    // Wait a bit, task should remain submitted
    time.Sleep(1 * time.Second)
    task, _ := node.Tasks.Get(ctx, "t1")
    if task.Status != TaskStatusSubmitted {
        t.Fatalf("task should still be submitted, got %s", task.Status)
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cluster/ -run TestWorker -v`
Expected: FAIL (Worker type not defined).

**Step 3: Write implementation**

```go
// cluster/worker.go
package cluster

import (
    "context"
    "log/slog"
    "time"
)

// TaskHandler is called when a worker claims a task.
// It should execute the task and return the result.
type TaskHandler func(ctx context.Context, task Task) TaskResult

// Worker watches for available tasks and executes them.
type Worker struct {
    node    *Node
    handler TaskHandler
    busy    bool
}

// NewWorker creates a task watcher for the given node.
func NewWorker(node *Node, handler TaskHandler) *Worker {
    return &Worker{node: node, handler: handler}
}

// Run polls for tasks matching this worker's capabilities and executes them.
// Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if w.busy {
                continue
            }
            w.tryClaimAndExecute(ctx)
        }
    }
}

func (w *Worker) tryClaimAndExecute(ctx context.Context) {
    tasks, err := w.node.Tasks.ListByStatus(ctx, TaskStatusSubmitted)
    if err != nil {
        return
    }

    for _, task := range tasks {
        if !w.matchesCapabilities(task) {
            continue
        }

        // Try to claim
        if err := w.node.Tasks.Claim(ctx, task.ID, w.node.Config.AgentID); err != nil {
            continue // someone else claimed it
        }

        w.busy = true
        w.execute(ctx, task)
        w.busy = false
        return // one task at a time
    }
}

func (w *Worker) matchesCapabilities(task Task) bool {
    if len(task.Specialization) == 0 {
        return true // no specialization = any worker
    }
    caps := make(map[string]bool, len(w.node.Config.Capabilities))
    for _, c := range w.node.Config.Capabilities {
        caps[c] = true
    }
    for _, s := range task.Specialization {
        if caps[s] {
            return true
        }
    }
    return false
}

func (w *Worker) execute(ctx context.Context, task Task) {
    // Update status to working
    if err := w.node.Tasks.SetWorking(ctx, task.ID); err != nil {
        slog.Error("set working failed", "task", task.ID, "error", err)
        return
    }
    w.node.Registry.UpdateStatus(ctx, w.node.Config.AgentID, AgentStatusWorking, task.ID)

    // Call handler
    result := w.handler(ctx, task)

    // Report result
    if result.Summary != "" || result.Branch != "" {
        w.node.Tasks.Complete(ctx, task.ID, result)
    } else {
        w.node.Tasks.Fail(ctx, task.ID, result)
    }

    // Clean up
    w.node.Registry.UpdateStatus(ctx, w.node.Config.AgentID, AgentStatusIdle, "")
}
```

**Step 4: Run tests**

Run: `go test ./cluster/ -run TestWorker -v -timeout 30s`
Expected: All PASS.

**Step 5: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add Worker task watcher with capability matching"
```

---

### Task 3.2: Phase 3 review

Codex review: worker polling, capability matching, one-at-a-time constraint, cleanup on completion.

---

## Phase 4: Server-Side Task Execution

### Task 4.1: Wire worker into server for real task execution

**Files:**
- Create: `server/cluster_worker.go`
- Modify: `server/server.go` (add worker startup)

**Step 1: Create server-side task execution bridge**

This file bridges the cluster worker into the server's conversation machinery. When the worker claims a task, this code:
1. Creates a git worktree
2. Creates a conversation
3. Inserts a system prompt
4. Sends the task description as a user message
5. Polls IsAgentWorking() until done
6. Reports the result back

```go
// server/cluster_worker.go
package server

import (
    "context"
    "fmt"
    "log/slog"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/tgruben-circuit/percy/cluster"
    "github.com/tgruben-circuit/percy/llm"
)

// startClusterWorker starts the background task watcher if in cluster mode.
func (s *Server) startClusterWorker() {
    if s.clusterNode == nil {
        return
    }

    handler := func(ctx context.Context, task cluster.Task) cluster.TaskResult {
        return s.executeClusterTask(ctx, task)
    }

    worker := cluster.NewWorker(s.clusterNode, handler)
    go worker.Run(context.Background())
    s.logger.Info("Cluster worker started", "agent", s.clusterNode.Config.AgentID)
}

func (s *Server) executeClusterTask(ctx context.Context, task cluster.Task) cluster.TaskResult {
    agentID := s.clusterNode.Config.AgentID
    taskID := task.ID
    branchName := fmt.Sprintf("agent/%s/%s", agentID, taskID)

    // 1. Create git worktree
    worktreeDir, err := s.createWorktree(ctx, task, branchName)
    if err != nil {
        s.logger.Error("Failed to create worktree", "task", taskID, "error", err)
        return cluster.TaskResult{Summary: fmt.Sprintf("worktree creation failed: %v", err)}
    }
    defer s.cleanupWorktree(worktreeDir)

    // 2. Create conversation
    slug := fmt.Sprintf("task-%s", taskID)
    cwd := worktreeDir
    modelID := s.defaultModel
    conv, err := s.db.CreateConversation(ctx, &slug, false, &cwd, &modelID)
    if err != nil {
        s.logger.Error("Failed to create conversation", "task", taskID, "error", err)
        return cluster.TaskResult{Summary: fmt.Sprintf("conversation creation failed: %v", err)}
    }

    // 3. Insert system prompt
    systemPrompt := fmt.Sprintf(
        "You are a worker agent executing a task from the cluster orchestrator.\n"+
            "You are on branch %s. Do NOT create or switch branches.\n\n"+
            "Your task: %s\n\n%s",
        branchName, task.Title, task.Description,
    )
    systemMsg := llm.Message{
        Role:    llm.MessageRoleUser,
        Content: []llm.Content{{Type: llm.ContentTypeText, Text: systemPrompt}},
    }
    s.recordMessage(ctx, conv.ConversationID, systemMsg, llm.Usage{}, "system")

    // 4. Get conversation manager and send task
    manager, err := s.getOrCreateConversationManager(ctx, conv.ConversationID)
    if err != nil {
        return cluster.TaskResult{Summary: fmt.Sprintf("manager creation failed: %v", err)}
    }

    llmService, err := s.llmManager.GetService(modelID)
    if err != nil {
        return cluster.TaskResult{Summary: fmt.Sprintf("llm service failed: %v", err)}
    }

    userMsg := llm.Message{
        Role:    llm.MessageRoleUser,
        Content: []llm.Content{{Type: llm.ContentTypeText, Text: task.Description}},
    }
    if _, err := manager.AcceptUserMessage(ctx, llmService, modelID, userMsg); err != nil {
        return cluster.TaskResult{Summary: fmt.Sprintf("accept message failed: %v", err)}
    }

    // 5. Poll until done (subagent pattern)
    for {
        select {
        case <-ctx.Done():
            return cluster.TaskResult{Summary: "cancelled"}
        case <-time.After(500 * time.Millisecond):
        }

        if !manager.IsAgentWorking() {
            break
        }
    }

    // 6. Get result
    summary := s.getLastAssistantText(ctx, conv.ConversationID)
    return cluster.TaskResult{
        Branch:  branchName,
        Summary: summary,
    }
}

func (s *Server) createWorktree(ctx context.Context, task cluster.Task, branchName string) (string, error) {
    baseBranch := task.Context.BaseBranch
    if baseBranch == "" {
        baseBranch = "main"
    }

    worktreeDir := filepath.Join("/tmp", "percy-worktree-"+task.ID)

    // Fetch latest
    fetch := exec.CommandContext(ctx, "git", "fetch", "origin")
    fetch.Dir = "."
    fetch.Run() // best-effort

    // Create worktree
    cmd := exec.CommandContext(ctx, "git", "worktree", "add", worktreeDir,
        "-b", branchName, "origin/"+baseBranch)
    if out, err := cmd.CombinedOutput(); err != nil {
        return "", fmt.Errorf("git worktree add: %s: %w", out, err)
    }

    return worktreeDir, nil
}

func (s *Server) cleanupWorktree(dir string) {
    exec.Command("git", "worktree", "remove", "--force", dir).Run()
}

func (s *Server) getLastAssistantText(ctx context.Context, conversationID string) string {
    msg, err := s.db.GetLatestMessage(ctx, conversationID)
    if err != nil {
        return ""
    }
    if msg.LlmData == nil {
        return ""
    }
    var m llm.Message
    if err := json.Unmarshal([]byte(*msg.LlmData), &m); err != nil {
        return ""
    }
    for _, c := range m.Content {
        if c.Type == llm.ContentTypeText && c.Text != "" {
            return c.Text
        }
    }
    return ""
}
```

**Step 2: Start worker in Server.Start()**

In `server/server.go`, in the `Start` method (after route registration), add:

```go
s.startClusterWorker()
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: Clean build.

**Step 4: Commit**

```bash
git add server/cluster_worker.go server/server.go
git commit -m "feat(server): add cluster task execution via conversation machinery"
```

---

### Task 4.2: Phase 4 review

Codex review: worktree lifecycle, conversation creation, polling pattern, cleanup, error handling.

---

## Phase 5: Dispatch Tasks Tool

### Task 5.1: Create dispatch_tasks tool

**Files:**
- Create: `claudetool/dispatch.go`
- Modify: `claudetool/toolset.go` (register tool)

**Step 1: Create the dispatch tool**

```go
// claudetool/dispatch.go
package claudetool

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/tgruben-circuit/percy/cluster"
    "github.com/tgruben-circuit/percy/llm"
)

type DispatchTool struct {
    node *cluster.Node
}

func NewDispatchTool(node *cluster.Node) *DispatchTool {
    return &DispatchTool{node: node}
}

type dispatchInput struct {
    Tasks []dispatchTaskInput `json:"tasks"`
}

type dispatchTaskInput struct {
    ID             string   `json:"id"`
    Title          string   `json:"title"`
    Description    string   `json:"description"`
    Specialization []string `json:"specialization,omitempty"`
    DependsOn      []string `json:"depends_on,omitempty"`
}

func (d *DispatchTool) Tool() *llm.Tool {
    return &llm.Tool{
        Name:        "dispatch_tasks",
        Type:        "custom",
        Description: "Dispatch subtasks to worker agents in the cluster. Each task is assigned to a worker based on specialization. Tasks with depends_on will wait until their dependencies complete.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "tasks": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "id": {"type": "string", "description": "Unique task ID"},
                            "title": {"type": "string", "description": "Short task title"},
                            "description": {"type": "string", "description": "Detailed task description for the worker agent"},
                            "specialization": {"type": "array", "items": {"type": "string"}, "description": "Required capabilities (e.g. go, ts, react)"},
                            "depends_on": {"type": "array", "items": {"type": "string"}, "description": "Task IDs that must complete first"}
                        },
                        "required": ["id", "title", "description"]
                    }
                }
            },
            "required": ["tasks"]
        }`),
        Run: d.run,
    }
}

func (d *DispatchTool) run(ctx context.Context, input json.RawMessage) llm.ToolOut {
    var in dispatchInput
    if err := json.Unmarshal(input, &in); err != nil {
        return llm.ToolOut{Content: fmt.Sprintf("Error parsing input: %v", err)}
    }

    if len(in.Tasks) == 0 {
        return llm.ToolOut{Content: "No tasks provided."}
    }

    // Check if workers are available
    agents, err := d.node.Registry.List(ctx)
    if err != nil {
        return llm.ToolOut{Content: fmt.Sprintf("Error listing agents: %v", err)}
    }
    workerCount := 0
    for _, a := range agents {
        if a.ID != d.node.Config.AgentID {
            workerCount++
        }
    }
    if workerCount == 0 {
        return llm.ToolOut{Content: "No workers connected to the cluster. Cannot dispatch tasks."}
    }

    // Build plan
    var planned []cluster.PlannedTask
    for _, t := range in.Tasks {
        planned = append(planned, cluster.PlannedTask{
            Task: cluster.Task{
                ID:             t.ID,
                Type:           cluster.TaskTypeImplement,
                Title:          t.Title,
                Description:    t.Description,
                Specialization: t.Specialization,
            },
            DependsOn: t.DependsOn,
        })
    }

    orch := cluster.NewOrchestrator(d.node)
    if err := orch.SubmitPlan(ctx, cluster.TaskPlan{Tasks: planned}); err != nil {
        return llm.ToolOut{Content: fmt.Sprintf("Error submitting plan: %v", err)}
    }

    // Build summary
    var immediate, blocked []string
    for _, pt := range planned {
        if len(pt.DependsOn) == 0 {
            immediate = append(immediate, pt.Task.Title)
        } else {
            blocked = append(blocked, fmt.Sprintf("%s (waiting on %s)", pt.Task.Title, strings.Join(pt.DependsOn, ", ")))
        }
    }

    summary := fmt.Sprintf("Dispatched %d tasks to %d workers.\n", len(planned), workerCount)
    if len(immediate) > 0 {
        summary += fmt.Sprintf("Ready now: %s\n", strings.Join(immediate, ", "))
    }
    if len(blocked) > 0 {
        summary += fmt.Sprintf("Waiting: %s\n", strings.Join(blocked, ", "))
    }

    return llm.ToolOut{Content: summary}
}
```

**Step 2: Register tool in ToolSetConfig and NewToolSet**

In `claudetool/toolset.go`, add to `ToolSetConfig`:

```go
ClusterNode any // *cluster.Node, typed as any to avoid import cycle
```

In `NewToolSet`, after the MemorySearchTool block (around line 177), add:

```go
if config.ClusterNode != nil {
    if node, ok := config.ClusterNode.(*cluster.Node); ok {
        tools = append(tools, NewDispatchTool(node).Tool())
    }
}
```

Note: This requires importing `cluster` in `toolset.go`. If this causes an import cycle, use the `any` typed config field and type-assert in the tool registration.

**Step 3: Wire ClusterNode into ToolSetConfig from server**

In `cmd/percy/main.go`, in the cluster block of `runServe`, after `svr.SetClusterNode(node)`:

```go
// Already done via svr.SetClusterNode; the server passes it to toolSetConfig
```

In `server/server.go`, in `SetClusterNode`:

```go
func (s *Server) SetClusterNode(node *cluster.Node) {
    s.clusterNode = node
    s.toolSetConfig.ClusterNode = node
}
```

**Step 4: Verify build**

Run: `go build ./...`
Expected: Clean build.

**Step 5: Commit**

```bash
git add claudetool/dispatch.go claudetool/toolset.go server/server.go
git commit -m "feat(claudetool): add dispatch_tasks tool for cluster orchestration"
```

---

### Task 5.2: Add cluster-mode system prompt enhancement

**Files:**
- Modify: `server/convo.go` (or system prompt generation)

In the system prompt generation, when `s.clusterNode != nil` and agents are connected, append:

```
You are the orchestrator of a cluster of Percy agents. When the user gives you a large task, break it into subtasks and use the dispatch_tasks tool to send them to available workers. Each worker has its own LLM and tools. Describe each subtask clearly -- the worker only sees the task description, not the conversation history.
```

Find the system prompt generation in `createSystemPrompt` and add a cluster section conditionally.

**Step 1: Commit**

```bash
git add server/
git commit -m "feat(server): add cluster orchestrator context to system prompt"
```

---

### Task 5.3: Phase 5 review

Codex review: dispatch tool schema, orchestrator integration, system prompt, import cycle handling.

---

## Phase 6: Orchestrator Monitor

### Task 6.1: Event-driven dependency resolution

**Files:**
- Create: `cluster/monitor.go`
- Create: `cluster/monitor_test.go`

**Step 1: Write test for monitor detecting completion and resolving deps**

```go
// cluster/monitor_test.go
package cluster

import (
    "context"
    "testing"
    "time"
)

func TestMonitorResolvesDependencies(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    node, err := StartNode(ctx, NodeConfig{
        AgentID: "orch", AgentName: "orchestrator",
        ListenAddr: ":0", StoreDir: t.TempDir(),
    })
    if err != nil {
        t.Fatal(err)
    }
    defer node.Stop()

    orch := NewOrchestrator(node)
    plan := TaskPlan{
        Tasks: []PlannedTask{
            {Task: Task{ID: "t1", Title: "Step 1", Type: TaskTypeImplement}},
            {Task: Task{ID: "t2", Title: "Step 2", Type: TaskTypeTest}, DependsOn: []string{"t1"}},
        },
    }
    orch.SubmitPlan(ctx, plan)

    mon := NewMonitor(node, orch)
    go mon.Run(ctx)

    // Complete t1 -- monitor should auto-resolve and submit t2
    node.Tasks.Claim(ctx, "t1", "worker-1")
    node.Tasks.SetWorking(ctx, "t1")
    node.Tasks.Complete(ctx, "t1", TaskResult{Branch: "b1", Summary: "done"})

    // Wait for t2 to appear as submitted
    deadline := time.After(5 * time.Second)
    for {
        select {
        case <-deadline:
            t.Fatal("t2 was not unblocked within 5s")
        default:
        }
        task, err := node.Tasks.Get(ctx, "t2")
        if err == nil && task.Status == TaskStatusSubmitted {
            return // success
        }
        time.Sleep(100 * time.Millisecond)
    }
}
```

**Step 2: Write implementation**

```go
// cluster/monitor.go
package cluster

import (
    "context"
    "log/slog"
    "time"

    "github.com/nats-io/nats.go"
)

// Monitor watches for task status changes and resolves dependencies.
type Monitor struct {
    node         *Node
    orchestrator *Orchestrator
}

func NewMonitor(node *Node, orch *Orchestrator) *Monitor {
    return &Monitor{node: node, orchestrator: orch}
}

// Run starts the monitor. It subscribes to task status events and
// periodically checks for stale agents. Blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
    // Subscribe to task status events
    sub, err := m.node.nc.Subscribe("task.*.status", func(msg *nats.Msg) {
        m.orchestrator.ResolveDependencies(ctx)
    })
    if err != nil {
        slog.Error("monitor subscribe failed", "error", err)
        return
    }
    defer sub.Unsubscribe()

    // Periodic stale agent check
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            stale := MarkStaleAgentsOffline(ctx, m.node.Registry, 90*time.Second)
            for _, agent := range stale {
                m.requeueAgentTasks(ctx, agent.ID)
            }
        }
    }
}

func (m *Monitor) requeueAgentTasks(ctx context.Context, agentID string) {
    // Find tasks assigned to dead agent
    for _, status := range []TaskStatus{TaskStatusAssigned, TaskStatusWorking} {
        tasks, err := m.node.Tasks.ListByStatus(ctx, status)
        if err != nil {
            continue
        }
        for _, task := range tasks {
            if task.AssignedTo == agentID {
                if err := m.node.Tasks.Requeue(ctx, task.ID); err != nil {
                    slog.Error("requeue failed", "task", task.ID, "error", err)
                }
            }
        }
    }

    // Release locks
    m.node.Locks.ReleaseByAgent(ctx, agentID)
}
```

**Step 3: Run tests**

Run: `go test ./cluster/ -run TestMonitor -v -timeout 30s`
Expected: PASS.

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add Monitor for event-driven dependency resolution and stale recovery"
```

---

### Task 6.2: Phase 6 review

Codex review: NATS subscription, dependency resolution timing, stale agent recovery, task requeuing.

---

## Phase 7: Enhanced Cluster Status API

### Task 7.1: Return full task and agent details

**Files:**
- Modify: `server/server.go` (enhance handleClusterStatus)

**Step 1: Enhance the handler to return full details**

Replace the current `handleClusterStatus` with a version that returns full task objects, agent details, and plan summary:

```go
func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
    if s.clusterNode == nil {
        http.Error(w, "not in cluster mode", http.StatusNotFound)
        return
    }

    ctx := r.Context()
    agents, _ := s.clusterNode.Registry.List(ctx)

    // Get all tasks across all statuses
    var allTasks []cluster.Task
    for _, status := range []cluster.TaskStatus{
        cluster.TaskStatusSubmitted, cluster.TaskStatusAssigned,
        cluster.TaskStatusWorking, cluster.TaskStatusCompleted,
        cluster.TaskStatusFailed,
    } {
        tasks, _ := s.clusterNode.Tasks.ListByStatus(ctx, status)
        allTasks = append(allTasks, tasks...)
    }

    // Build summary
    summary := map[string]int{"total": len(allTasks)}
    for _, t := range allTasks {
        summary[string(t.Status)]++
    }

    status := map[string]any{
        "agents":       agents,
        "tasks":        allTasks,
        "plan_summary": summary,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: Clean build.

**Step 3: Commit**

```bash
git add server/server.go
git commit -m "feat(server): enhance cluster status API with full task and agent details"
```

---

### Task 7.2: Phase 7 review

Codex review: API response shape, error handling, performance concerns.

---

## Phase 8: Cluster Dashboard UI

### Task 8.1: Create React cluster dashboard component

**Files:**
- Create: `ui/src/components/ClusterDashboard.tsx`
- Modify: `ui/src/App.tsx` (add dashboard panel)

This task creates a collapsible side panel showing cluster status. The component polls `/api/cluster/status` and displays:
- Agent cards with status badges
- Task list with status and dependency info
- Summary counts

The exact React implementation will depend on the existing UI patterns. The implementer should:
1. Read existing UI components to match patterns
2. Create ClusterDashboard.tsx
3. Add it to App.tsx, conditionally rendered when cluster mode is active
4. Style it consistently with existing UI

**Step 1: Implement component**
**Step 2: Build UI: `cd ui && pnpm run build`**
**Step 3: Verify: `make build`**
**Step 4: Commit**

```bash
git add ui/
git commit -m "feat(ui): add cluster dashboard panel"
```

---

### Task 8.2: Phase 8 review

Codex review: React component, API integration, conditional rendering, styling.

---

## Phase 9: Integration Test

### Task 9.1: End-to-end orchestrator dispatches to worker

**Files:**
- Create: `cluster/e2e_test.go`

Write a test that:
1. Starts an orchestrator node with embedded NATS
2. Starts a worker node with a mock task handler
3. Orchestrator submits a plan with dependencies
4. Worker claims, executes, and completes tasks
5. Monitor resolves dependencies
6. All tasks complete successfully

This builds on the existing integration tests but adds the Worker and Monitor components.

**Step 1: Write and run test**
**Step 2: Commit**

```bash
git add cluster/
git commit -m "test(cluster): add end-to-end orchestrator-worker integration test"
```

---

### Task 9.2: Phase 9 review (final)

Final Codex review of the entire orchestrator UI implementation. Verify:
- Full build: `go build ./...`
- All tests: `go test ./... -count=1 -timeout 120s`
- No regressions in existing behavior
