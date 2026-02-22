# Merge Pipeline Implementation Plan

Note: Historical planning document (Feb 2026). References may be outdated; see `architecture_nat.md` and `README.md` for current state.

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a merge pipeline that integrates worker branches into the orchestrator's working branch with LLM-assisted conflict resolution, plus wire remaining loose ends (monitor startup, system prompt).

**Architecture:** Orchestrator creates a dedicated merge worktree at startup. When tasks complete, Monitor calls MergeAndResolve which merges the worker branch, resolves conflicts via direct LLM calls, and then unblocks dependents. Transactional: abort on failure, requeue task.

**Tech Stack:** Go, git (exec.Command), existing Percy LLM service, NATS cluster package.

**Review process:** Codex code review after each phase.

---

## Phase 1: Node & TaskResult Changes

### Task 1.1: Add IsOrchestrator to Node and MergeStatus to TaskResult

**Files:**
- Modify: `cluster/node.go`
- Modify: `cluster/task.go`

**Step 1: Add IsOrchestrator method to Node**

In `cluster/node.go`, add:

```go
// IsOrchestrator returns true if this node is running an embedded NATS server.
func (n *Node) IsOrchestrator() bool {
    return n.embedded != nil
}
```

**Step 2: Add MergeStatus and MergeCommit to TaskResult**

In `cluster/task.go`, update `TaskResult`:

```go
type TaskResult struct {
    Branch      string `json:"branch"`
    Summary     string `json:"summary"`
    MergeStatus string `json:"merge_status,omitempty"`
    MergeCommit string `json:"merge_commit,omitempty"`
}
```

**Step 3: Verify build**

Run: `go build ./cluster/ && go test ./cluster/ -count=1 -timeout 30s`
Expected: All pass (additive changes only).

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add IsOrchestrator and MergeStatus to TaskResult"
```

---

### Task 1.2: Add WorkingBranch to Orchestrator

**Files:**
- Modify: `cluster/orchestrator.go`
- Modify: `cluster/orchestrator_test.go`

**Step 1: Add WorkingBranch field and setter**

In `cluster/orchestrator.go`, add to `Orchestrator` struct:

```go
type Orchestrator struct {
    node          *Node
    plan          *TaskPlan
    submitted     map[string]bool
    workingBranch string
}

// SetWorkingBranch records the branch that worker branches merge into.
func (o *Orchestrator) SetWorkingBranch(branch string) {
    o.workingBranch = branch
}

// WorkingBranch returns the configured working branch.
func (o *Orchestrator) WorkingBranch() string {
    return o.workingBranch
}
```

**Step 2: Add test**

```go
func TestOrchestratorWorkingBranch(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    node, err := StartNode(ctx, NodeConfig{
        AgentID: "orch", AgentName: "orch",
        ListenAddr: ":0", StoreDir: t.TempDir(),
    })
    if err != nil {
        t.Fatal(err)
    }
    defer node.Stop()

    orch := NewOrchestrator(node)
    orch.SetWorkingBranch("feature/auth")

    if got := orch.WorkingBranch(); got != "feature/auth" {
        t.Fatalf("got %q, want feature/auth", got)
    }
}
```

**Step 3: Run tests**

Run: `go test ./cluster/ -run TestOrchestratorWorkingBranch -v`
Expected: PASS.

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add WorkingBranch to Orchestrator"
```

---

### Task 1.3: Phase 1 review

Codex review: additive changes, no breakage.

---

## Phase 2: Merge Worktree Management

### Task 2.1: Merge worktree lifecycle on Orchestrator

**Files:**
- Create: `cluster/merge.go`
- Create: `cluster/merge_test.go`

**Step 1: Write test for merge worktree creation and cleanup**

```go
// cluster/merge_test.go
package cluster

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
    "time"
)

// setupGitRepo creates a temporary git repo with an initial commit on a branch.
func setupGitRepo(t *testing.T, branch string) string {
    t.Helper()
    dir := t.TempDir()
    run := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = dir
        if out, err := cmd.CombinedOutput(); err != nil {
            t.Fatalf("git %v: %s: %v", args, out, err)
        }
    }
    run("init", "-b", branch)
    run("config", "user.email", "test@test.com")
    run("config", "user.name", "Test")

    // Create initial commit
    f := filepath.Join(dir, "README.md")
    os.WriteFile(f, []byte("# Test\n"), 0644)
    run("add", ".")
    run("commit", "-m", "initial")

    return dir
}

func TestMergeWorktreeCreateAndCleanup(t *testing.T) {
    repoDir := setupGitRepo(t, "feature/test")

    mw, err := NewMergeWorktree(repoDir, "test-orch", "feature/test")
    if err != nil {
        t.Fatal(err)
    }

    // Worktree directory should exist
    if _, err := os.Stat(mw.Dir()); os.IsNotExist(err) {
        t.Fatal("merge worktree directory does not exist")
    }

    mw.Cleanup()

    // Directory should be gone
    if _, err := os.Stat(mw.Dir()); !os.IsNotExist(err) {
        t.Fatal("merge worktree directory still exists after cleanup")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cluster/ -run TestMergeWorktree -v`
Expected: FAIL.

**Step 3: Write implementation**

```go
// cluster/merge.go
package cluster

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
)

// MergeWorktree manages a dedicated git worktree for merge operations.
type MergeWorktree struct {
    repoDir string
    dir     string
    branch  string
}

// NewMergeWorktree creates a git worktree for merging into the given branch.
func NewMergeWorktree(repoDir, agentID, branch string) (*MergeWorktree, error) {
    dir := filepath.Join(os.TempDir(), "percy-merge-"+agentID)

    // Remove stale worktree if it exists
    exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", dir).Run()
    os.RemoveAll(dir)

    cmd := exec.Command("git", "-C", repoDir, "worktree", "add", dir, branch)
    if out, err := cmd.CombinedOutput(); err != nil {
        return nil, fmt.Errorf("create merge worktree: %s: %w", string(out), err)
    }

    return &MergeWorktree{repoDir: repoDir, dir: dir, branch: branch}, nil
}

// Dir returns the worktree directory path.
func (mw *MergeWorktree) Dir() string {
    return mw.dir
}

// Cleanup removes the merge worktree.
func (mw *MergeWorktree) Cleanup() {
    exec.Command("git", "-C", mw.repoDir, "worktree", "remove", "--force", mw.dir).Run()
    os.RemoveAll(mw.dir)
}
```

**Step 4: Run tests**

Run: `go test ./cluster/ -run TestMergeWorktree -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add MergeWorktree for dedicated merge operations"
```

---

### Task 2.2: Phase 2 review

Codex review: worktree lifecycle, stale cleanup, git commands.

---

## Phase 3: MergeAndResolve (Clean Merges)

### Task 3.1: Merge a worker branch (no conflicts)

**Files:**
- Modify: `cluster/merge.go`
- Modify: `cluster/merge_test.go`

**Step 1: Write test for clean merge**

```go
func TestMergeClean(t *testing.T) {
    repoDir := setupGitRepo(t, "feature/test")
    run := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = repoDir
        if out, err := cmd.CombinedOutput(); err != nil {
            t.Fatalf("git %v: %s: %v", args, out, err)
        }
    }

    // Create a worker branch with a change
    run("checkout", "-b", "agent/worker/t1")
    os.WriteFile(filepath.Join(repoDir, "new-file.go"), []byte("package main\n"), 0644)
    run("add", ".")
    run("commit", "-m", "worker: add new file")

    // Go back to working branch
    run("checkout", "feature/test")

    mw, err := NewMergeWorktree(repoDir, "test-orch", "feature/test")
    if err != nil {
        t.Fatal(err)
    }
    defer mw.Cleanup()

    result, err := mw.Merge(context.Background(), "agent/worker/t1", "Add new file", nil)
    if err != nil {
        t.Fatal(err)
    }
    if result.MergeStatus != "merged" {
        t.Fatalf("got status %q, want merged", result.MergeStatus)
    }
    if result.MergeCommit == "" {
        t.Fatal("merge commit is empty")
    }

    // Verify file exists in merge worktree
    if _, err := os.Stat(filepath.Join(mw.Dir(), "new-file.go")); err != nil {
        t.Fatal("merged file not found in worktree")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cluster/ -run TestMergeClean -v`
Expected: FAIL.

**Step 3: Write Merge method**

Add to `cluster/merge.go`:

```go
import (
    "context"
    "strings"
)

// ConflictResolver is called for each conflicted file during a merge.
// It receives the file path, ours/theirs/base content, and task context.
// Returns the resolved file content.
type ConflictResolver func(ctx context.Context, path, ours, theirs, base, taskTitle, taskDesc string) (string, error)

// MergeResult contains the outcome of a merge operation.
type MergeResult struct {
    MergeStatus string // "merged", "conflict_resolved", "merge_failed"
    MergeCommit string
}

// Merge merges the given branch into the worktree's branch.
// If conflicts occur and resolver is non-nil, attempts LLM resolution.
// If resolver is nil or resolution fails, aborts the merge.
func (mw *MergeWorktree) Merge(ctx context.Context, branchName, taskTitle string, resolver ConflictResolver) (MergeResult, error) {
    // Attempt merge
    mergeCmd := exec.CommandContext(ctx, "git", "merge", branchName, "--no-ff",
        "-m", fmt.Sprintf("Merge %s: %s", branchName, taskTitle))
    mergeCmd.Dir = mw.dir
    mergeOut, mergeErr := mergeCmd.CombinedOutput()

    if mergeErr == nil {
        // Clean merge
        commit, _ := mw.headCommit(ctx)
        return MergeResult{MergeStatus: "merged", MergeCommit: commit}, nil
    }

    // Check if it's a conflict (not some other error)
    if !strings.Contains(string(mergeOut), "CONFLICT") {
        return MergeResult{MergeStatus: "merge_failed"}, fmt.Errorf("merge %s: %s: %w", branchName, string(mergeOut), mergeErr)
    }

    // No resolver? Abort.
    if resolver == nil {
        mw.abortMerge(ctx)
        return MergeResult{MergeStatus: "merge_failed"}, fmt.Errorf("merge conflict in %s, no resolver", branchName)
    }

    // Resolve conflicts
    if err := mw.resolveConflicts(ctx, branchName, taskTitle, "", resolver); err != nil {
        mw.abortMerge(ctx)
        return MergeResult{MergeStatus: "merge_failed"}, fmt.Errorf("conflict resolution failed: %w", err)
    }

    // Commit the resolution
    commitCmd := exec.CommandContext(ctx, "git", "commit", "--no-edit")
    commitCmd.Dir = mw.dir
    if out, err := commitCmd.CombinedOutput(); err != nil {
        mw.abortMerge(ctx)
        return MergeResult{MergeStatus: "merge_failed"}, fmt.Errorf("commit resolution: %s: %w", string(out), err)
    }

    commit, _ := mw.headCommit(ctx)
    return MergeResult{MergeStatus: "conflict_resolved", MergeCommit: commit}, nil
}

func (mw *MergeWorktree) headCommit(ctx context.Context) (string, error) {
    cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
    cmd.Dir = mw.dir
    out, err := cmd.Output()
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(string(out)), nil
}

func (mw *MergeWorktree) abortMerge(ctx context.Context) {
    cmd := exec.CommandContext(ctx, "git", "merge", "--abort")
    cmd.Dir = mw.dir
    cmd.Run()
}

func (mw *MergeWorktree) resolveConflicts(ctx context.Context, branchName, taskTitle, taskDesc string, resolver ConflictResolver) error {
    // Get list of conflicted files
    cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U")
    cmd.Dir = mw.dir
    out, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("list conflicts: %w", err)
    }

    files := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, path := range files {
        if path == "" {
            continue
        }

        // Skip binary files
        if mw.isBinary(ctx, path) {
            return fmt.Errorf("binary file conflict: %s", path)
        }

        ours := mw.gitShow(ctx, "HEAD", path)
        theirs := mw.gitShow(ctx, branchName, path)
        base := mw.gitShowBase(ctx, branchName, path)

        resolved, err := resolver(ctx, path, ours, theirs, base, taskTitle, taskDesc)
        if err != nil {
            return fmt.Errorf("resolve %s: %w", path, err)
        }

        // Write resolved content
        fullPath := filepath.Join(mw.dir, path)
        if err := os.WriteFile(fullPath, []byte(resolved), 0644); err != nil {
            return fmt.Errorf("write resolved %s: %w", path, err)
        }

        // Stage
        addCmd := exec.CommandContext(ctx, "git", "add", path)
        addCmd.Dir = mw.dir
        if out, err := addCmd.CombinedOutput(); err != nil {
            return fmt.Errorf("git add %s: %s: %w", path, string(out), err)
        }
    }

    return nil
}

func (mw *MergeWorktree) gitShow(ctx context.Context, ref, path string) string {
    cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
    cmd.Dir = mw.dir
    out, _ := cmd.Output()
    return string(out)
}

func (mw *MergeWorktree) gitShowBase(ctx context.Context, branchName, path string) string {
    // Find merge base
    baseCmd := exec.CommandContext(ctx, "git", "merge-base", "HEAD", branchName)
    baseCmd.Dir = mw.dir
    baseOut, err := baseCmd.Output()
    if err != nil {
        return ""
    }
    base := strings.TrimSpace(string(baseOut))
    return mw.gitShow(ctx, base, path)
}

func (mw *MergeWorktree) isBinary(ctx context.Context, path string) bool {
    cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "--diff-filter=U", "--", path)
    cmd.Dir = mw.dir
    out, _ := cmd.Output()
    return strings.HasPrefix(string(out), "-\t-\t")
}

// DeleteBranch removes a merged worker branch.
func (mw *MergeWorktree) DeleteBranch(ctx context.Context, branchName string) {
    cmd := exec.CommandContext(ctx, "git", "-C", mw.repoDir, "branch", "-d", branchName)
    cmd.Run()
}
```

**Step 4: Run tests**

Run: `go test ./cluster/ -run TestMerge -v`
Expected: All PASS.

**Step 5: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add Merge method with conflict resolution support"
```

---

### Task 3.2: Test merge with conflicts and resolver

**Files:**
- Modify: `cluster/merge_test.go`

**Step 1: Write test for conflict resolution**

```go
func TestMergeWithConflict(t *testing.T) {
    repoDir := setupGitRepo(t, "feature/test")
    run := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = repoDir
        if out, err := cmd.CombinedOutput(); err != nil {
            t.Fatalf("git %v: %s: %v", args, out, err)
        }
    }

    // Create conflicting changes
    // Working branch: modify README
    os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Working branch change\n"), 0644)
    run("add", ".")
    run("commit", "-m", "working: update readme")

    // Worker branch: also modify README differently
    run("checkout", "-b", "agent/worker/t1", "HEAD~1")
    os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Worker branch change\n"), 0644)
    run("add", ".")
    run("commit", "-m", "worker: update readme")

    // Go back to working branch
    run("checkout", "feature/test")

    mw, err := NewMergeWorktree(repoDir, "test-orch", "feature/test")
    if err != nil {
        t.Fatal(err)
    }
    defer mw.Cleanup()

    // Mock resolver that combines both changes
    resolver := func(ctx context.Context, path, ours, theirs, base, title, desc string) (string, error) {
        return "# Merged: both changes\n", nil
    }

    result, err := mw.Merge(context.Background(), "agent/worker/t1", "Update readme", resolver)
    if err != nil {
        t.Fatal(err)
    }
    if result.MergeStatus != "conflict_resolved" {
        t.Fatalf("got status %q, want conflict_resolved", result.MergeStatus)
    }

    // Verify resolved content
    content, _ := os.ReadFile(filepath.Join(mw.Dir(), "README.md"))
    if string(content) != "# Merged: both changes\n" {
        t.Fatalf("got content %q", string(content))
    }
}

func TestMergeConflictNoResolver(t *testing.T) {
    repoDir := setupGitRepo(t, "feature/test")
    run := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = repoDir
        if out, err := cmd.CombinedOutput(); err != nil {
            t.Fatalf("git %v: %s: %v", args, out, err)
        }
    }

    // Create conflicting changes (same pattern as above)
    os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Working\n"), 0644)
    run("add", ".")
    run("commit", "-m", "working")
    run("checkout", "-b", "agent/worker/t1", "HEAD~1")
    os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Worker\n"), 0644)
    run("add", ".")
    run("commit", "-m", "worker")
    run("checkout", "feature/test")

    mw, err := NewMergeWorktree(repoDir, "test-orch", "feature/test")
    if err != nil {
        t.Fatal(err)
    }
    defer mw.Cleanup()

    // No resolver -- should abort and fail
    _, err = mw.Merge(context.Background(), "agent/worker/t1", "Conflict", nil)
    if err == nil {
        t.Fatal("expected error for unresolved conflict")
    }
}
```

**Step 2: Run tests**

Run: `go test ./cluster/ -run TestMerge -v`
Expected: All PASS.

**Step 3: Commit**

```bash
git add cluster/
git commit -m "test(cluster): add merge conflict and abort tests"
```

---

### Task 3.3: Phase 3 review

Codex review: merge logic, conflict detection, abort on failure, resolver callback pattern.

---

## Phase 4: LLM Conflict Resolver

### Task 4.1: Create LLM-based ConflictResolver

**Files:**
- Create: `cluster/llm_resolver.go`
- Create: `cluster/llm_resolver_test.go`

**Step 1: Write implementation**

```go
// cluster/llm_resolver.go
package cluster

import (
    "context"
    "fmt"
    "time"

    "github.com/tgruben-circuit/percy/llm"
)

// NewLLMConflictResolver creates a ConflictResolver that uses an LLM service
// to resolve merge conflicts. Each file resolution has a 2-minute timeout.
func NewLLMConflictResolver(service llm.Service) ConflictResolver {
    return func(ctx context.Context, path, ours, theirs, base, taskTitle, taskDesc string) (string, error) {
        resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
        defer cancel()

        prompt := fmt.Sprintf(
            "Resolve this merge conflict for %s.\n\n"+
                "The worker was doing: %s: %s\n\n"+
                "BASE version (before either change):\n%s\n\n"+
                "OURS version (working branch):\n%s\n\n"+
                "THEIRS version (worker's change):\n%s\n\n"+
                "Output ONLY the resolved file content. No explanation, no markdown fences, no line numbers. Just the raw file content.",
            path, taskTitle, taskDesc, base, ours, theirs,
        )

        req := &llm.Request{
            Messages: []llm.Message{
                {
                    Role:    llm.MessageRoleUser,
                    Content: []llm.Content{{Type: llm.ContentTypeText, Text: prompt}},
                },
            },
        }

        resp, err := service.Do(resolveCtx, req)
        if err != nil {
            return "", fmt.Errorf("llm resolve %s: %w", path, err)
        }

        // Extract text from response
        for _, c := range resp.Content {
            if c.Type == llm.ContentTypeText && c.Text != "" {
                return c.Text, nil
            }
        }

        return "", fmt.Errorf("llm returned no text for %s", path)
    }
}
```

**Step 2: Write test using predictable LLM**

```go
// cluster/llm_resolver_test.go
package cluster

import (
    "context"
    "testing"
)

// mockLLMService is a minimal LLM service for testing conflict resolution.
type mockLLMService struct {
    response string
}

func (m *mockLLMService) Do(ctx context.Context, req *llm.Request) (*llm.Response, error) {
    return &llm.Response{
        Content: []llm.Content{{Type: llm.ContentTypeText, Text: m.response}},
    }, nil
}

func (m *mockLLMService) TokenContextWindow() int { return 100000 }
func (m *mockLLMService) MaxImageDimension() int  { return 0 }

func TestLLMConflictResolver(t *testing.T) {
    mock := &mockLLMService{response: "resolved content"}
    resolver := NewLLMConflictResolver(mock)

    result, err := resolver(context.Background(), "test.go", "ours", "theirs", "base", "Task", "Description")
    if err != nil {
        t.Fatal(err)
    }
    if result != "resolved content" {
        t.Fatalf("got %q, want %q", result, "resolved content")
    }
}
```

Note: You'll need to import `"github.com/tgruben-circuit/percy/llm"` in the test file.

**Step 3: Run tests**

Run: `go test ./cluster/ -run TestLLMConflict -v`
Expected: PASS.

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): add LLM-based merge conflict resolver"
```

---

### Task 4.2: Phase 4 review

Codex review: prompt quality, timeout, response extraction, mock test validity.

---

## Phase 5: Wire Monitor with Merge Pipeline

### Task 5.1: Update Monitor to call MergeAndResolve

**Files:**
- Modify: `cluster/monitor.go`
- Modify: `cluster/orchestrator.go`

**Step 1: Add MergeAndResolve to Orchestrator**

In `cluster/orchestrator.go`, add:

```go
// MergeAndResolve merges a completed task's branch, then resolves dependencies.
func (o *Orchestrator) MergeAndResolve(ctx context.Context, taskID string, mw *MergeWorktree, resolver ConflictResolver) error {
    task, err := o.node.Tasks.Get(ctx, taskID)
    if err != nil {
        return fmt.Errorf("get task %s: %w", taskID, err)
    }

    if task.Status != TaskStatusCompleted {
        return nil // not completed, nothing to merge
    }

    if task.Result.Branch == "" {
        // No branch (task failed without producing a branch) -- just resolve deps
        o.ResolveDependencies(ctx)
        return nil
    }

    if task.Result.MergeStatus != "" {
        // Already merged -- just resolve deps
        o.ResolveDependencies(ctx)
        return nil
    }

    // Merge the branch
    result, err := mw.Merge(ctx, task.Result.Branch, task.Title, resolver)
    if err != nil {
        slog.Error("merge failed", "task", taskID, "branch", task.Result.Branch, "error", err)
        // Requeue for retry
        o.node.Tasks.Requeue(ctx, taskID)
        return err
    }

    // Update task with merge result
    task.Result.MergeStatus = result.MergeStatus
    task.Result.MergeCommit = result.MergeCommit
    // Need to update task in KV -- use Complete again to update the result
    o.node.Tasks.Complete(ctx, taskID, task.Result)

    // Clean up worker branch
    mw.DeleteBranch(ctx, task.Result.Branch)

    // Now resolve dependencies
    o.ResolveDependencies(ctx)
    return nil
}
```

Note: This calls `Complete` again to update the result with merge info. The CAS in `setResult` will work because we're the only writer at this point (sequential processing).

**Step 2: Update Monitor to use MergeAndResolve**

In `cluster/monitor.go`, add `mergeWorktree` and `resolver` fields:

```go
type Monitor struct {
    node           *Node
    orchestrator   *Orchestrator
    mergeWorktree  *MergeWorktree
    resolver       ConflictResolver
}

func NewMonitor(node *Node, orch *Orchestrator, mw *MergeWorktree, resolver ConflictResolver) *Monitor {
    return &Monitor{
        node:          node,
        orchestrator:  orch,
        mergeWorktree: mw,
        resolver:      resolver,
    }
}
```

Update the NATS subscription callback in `Run`:

```go
// Instead of just calling ResolveDependencies, call MergeAndResolve
sub, err := m.node.NC().Subscribe("task.*.status", func(msg *nats.Msg) {
    // Extract task ID from subject (task.{id}.status)
    parts := strings.Split(msg.Subject, ".")
    if len(parts) != 3 {
        return
    }
    taskID := parts[1]

    if m.mergeWorktree != nil {
        m.orchestrator.MergeAndResolve(ctx, taskID, m.mergeWorktree, m.resolver)
    } else {
        m.orchestrator.ResolveDependencies(ctx)
    }
})
```

**IMPORTANT**: This changes the `NewMonitor` signature. Update all callers:
- `cluster/monitor_test.go` -- add `nil, nil` for mergeWorktree and resolver
- `cluster/e2e_test.go` -- add `nil, nil`

**Step 3: Verify all tests pass**

Run: `go test ./cluster/ -count=1 -timeout 30s`
Expected: All pass.

**Step 4: Commit**

```bash
git add cluster/
git commit -m "feat(cluster): wire MergeAndResolve into Monitor for merge pipeline"
```

---

### Task 5.2: Phase 5 review

Codex review: MergeAndResolve flow, Monitor changes, backward compatibility with nil worktree.

---

## Phase 6: Server Integration (Monitor Startup + System Prompt)

### Task 6.1: Start monitor from server

**Files:**
- Create: `server/cluster_monitor.go`
- Modify: `server/server.go` (call startup)

**Step 1: Create cluster monitor startup**

```go
// server/cluster_monitor.go
package server

import (
    "context"
    "os/exec"
    "strings"

    "github.com/tgruben-circuit/percy/cluster"
)

// startClusterMonitor starts the orchestrator monitor if this node is the orchestrator.
func (s *Server) startClusterMonitor() {
    if s.clusterNode == nil || !s.clusterNode.IsOrchestrator() {
        return
    }

    // Detect working branch
    workingBranch := detectWorkingBranch(s.toolSetConfig.WorkingDir)
    if workingBranch == "" {
        s.logger.Warn("Could not detect working branch, skipping cluster monitor")
        return
    }

    // Create orchestrator
    orch := cluster.NewOrchestrator(s.clusterNode)
    orch.SetWorkingBranch(workingBranch)

    // Create merge worktree
    mw, err := cluster.NewMergeWorktree(s.toolSetConfig.WorkingDir, s.clusterNode.Config.AgentID, workingBranch)
    if err != nil {
        s.logger.Error("Failed to create merge worktree", "error", err)
        return
    }

    // Create LLM resolver
    var resolver cluster.ConflictResolver
    llmService, err := s.llmManager.GetService(s.defaultModel)
    if err == nil {
        resolver = cluster.NewLLMConflictResolver(llmService)
    }

    // Start monitor
    mon := cluster.NewMonitor(s.clusterNode, orch, mw, resolver)
    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        <-s.shutdownCh
        cancel()
        mw.Cleanup()
    }()
    go mon.Run(ctx)

    s.logger.Info("Cluster monitor started", "branch", workingBranch)
}

func detectWorkingBranch(dir string) string {
    cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
    cmd.Dir = dir
    out, err := cmd.Output()
    if err != nil {
        return ""
    }
    branch := strings.TrimSpace(string(out))
    if branch == "HEAD" {
        return "" // detached HEAD
    }
    return branch
}
```

**Step 2: Call from server startup**

In `server/server.go`, after `s.startClusterWorker()`, add:

```go
s.startClusterMonitor()
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: Clean.

**Step 4: Commit**

```bash
git add server/
git commit -m "feat(server): start cluster monitor with merge pipeline on orchestrator"
```

---

### Task 6.2: Add cluster context to system prompt

**Files:**
- Modify: `server/convo.go` (find `createSystemPrompt`)

In `createSystemPrompt`, after the existing system prompt content is built, add a cluster section:

```go
// If in cluster mode with workers connected, add orchestrator context
if cm.clusterNode != nil {
    agents, _ := cm.clusterNode.Registry.List(ctx)
    if len(agents) > 1 { // more than just self
        prompt += "\n\nYou are the orchestrator of a cluster of Percy agents. " +
            "Use the dispatch_tasks tool to break large tasks into subtasks for available workers. " +
            "Each worker has its own LLM and tools. Describe subtasks clearly -- " +
            "workers only see the task description, not the conversation history."
    }
}
```

Note: `ConversationManager` doesn't currently have access to the cluster node. You'll need to either:
- Pass it through `ToolSetConfig.ClusterNode` (already available)
- Or add a `clusterNode` field to `ConversationManager`

Check what's available and use the simplest path.

**Step 1: Verify build**

Run: `go build ./...`

**Step 2: Commit**

```bash
git add server/
git commit -m "feat(server): add cluster orchestrator context to system prompt"
```

---

### Task 6.3: Phase 6 review

Codex review: monitor startup, shutdown cleanup, system prompt, detectWorkingBranch reliability.

---

## Phase 7: Integration Test

### Task 7.1: End-to-end merge pipeline test

**Files:**
- Create: `cluster/merge_e2e_test.go`

Write a test that:
1. Creates a git repo with a working branch
2. Starts orchestrator and worker nodes
3. Creates a merge worktree
4. Submits a plan where the worker modifies a file
5. Worker executes (mock handler that creates a commit on the worker branch)
6. Monitor detects completion, merges the branch
7. Verify the merge happened on the working branch

This test exercises the full pipeline: submit → worker claims → worker completes → monitor merges → dependencies resolve.

**Step 1: Write and run test**
**Step 2: Verify all tests pass: `go test ./cluster/ -count=1 -timeout 60s`**
**Step 3: Commit**

```bash
git add cluster/
git commit -m "test(cluster): add end-to-end merge pipeline integration test"
```

---

### Task 7.2: Phase 7 review (final)

Final Codex review of the entire merge pipeline. Verify:
- Full build: `go build ./...`
- All cluster tests: `go test ./cluster/ -count=1 -timeout 60s`
- No regressions
