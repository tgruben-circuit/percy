package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMergePipelineE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create a git repo with a working branch (feature/test).
	repoDir := setupGitRepo(t, "feature/test")

	// 2. Start orchestrator node with embedded NATS.
	orchNode, err := StartNode(ctx, NodeConfig{
		AgentID:    "orchestrator",
		AgentName:  "orchestrator",
		ListenAddr: ":0",
		StoreDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer orchNode.Stop()

	// 3. Create a mock worker handler that makes real git commits.
	var mu sync.Mutex
	var executed []string

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %s: %v", args, out, err)
		}
	}

	handler := func(ctx context.Context, task Task) TaskResult {
		branchName := fmt.Sprintf("agent/worker-1/%s", task.ID)

		// Create branch from feature/test.
		run(repoDir, "git", "checkout", "-b", branchName, "feature/test")

		// Make a change.
		filename := task.ID + ".go"
		if err := os.WriteFile(filepath.Join(repoDir, filename), []byte("package main\n"), 0o644); err != nil {
			t.Errorf("write file: %v", err)
			return TaskResult{Summary: "write failed"}
		}
		run(repoDir, "git", "add", ".")
		run(repoDir, "git", "commit", "-m", "worker: "+task.Title)

		// Switch back to feature/test.
		run(repoDir, "git", "checkout", "feature/test")

		mu.Lock()
		executed = append(executed, task.ID)
		mu.Unlock()

		return TaskResult{Branch: branchName, Summary: "done"}
	}

	// 4. Start worker node connecting to orchestrator.
	workerNode, err := StartNode(ctx, NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "worker",
		Capabilities: []string{"go"},
		NATSUrl:      orchNode.ClientURL(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer workerNode.Stop()

	worker := NewWorker(workerNode, handler)
	go worker.Run(ctx)

	// 5. Create orchestrator and merge worktree.
	orch := NewOrchestrator(orchNode)
	orch.SetWorkingBranch("feature/test")

	mw, err := NewMergeWorktree(repoDir, "orch", "feature/test")
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Cleanup()

	// 6. Create monitor with merge worktree and nil resolver (no conflicts expected).
	mon := NewMonitor(orchNode, orch, mw, nil)
	go mon.Run(ctx)

	// 7. Submit a plan with two tasks: t1 has no deps, t2 depends on t1.
	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:    "t1",
					Type:  TaskTypeImplement,
					Title: "Add hello.go",
					Context: TaskContext{
						Repo:       repoDir,
						BaseBranch: "feature/test",
					},
				},
			},
			{
				Task: Task{
					ID:    "t2",
					Type:  TaskTypeImplement,
					Title: "Add world.go",
					Context: TaskContext{
						Repo:       repoDir,
						BaseBranch: "feature/test",
					},
				},
				DependsOn: []string{"t1"},
			},
		},
	}
	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}

	// 8. Wait for both tasks to be completed AND merged.
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			t1, _ := orchNode.Tasks.Get(ctx, "t1")
			t2, _ := orchNode.Tasks.Get(ctx, "t2")
			t.Fatalf("timed out waiting for merge. t1=%+v t2=%+v", t1, t2)
		default:
		}

		t1, err := orchNode.Tasks.Get(ctx, "t1")
		if err != nil || t1.Result.MergeStatus == "" {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		t2, err := orchNode.Tasks.Get(ctx, "t2")
		if err != nil || t2.Result.MergeStatus == "" {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		break
	}

	// 9. Verify results.

	// Both files exist in the merge worktree.
	for _, name := range []string{"t1.go", "t2.go"} {
		path := filepath.Join(mw.Dir(), name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist in merge worktree: %v", name, err)
		}
	}

	// Both tasks have MergeStatus = "merged".
	t1, err := orchNode.Tasks.Get(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if t1.Result.MergeStatus != "merged" {
		t.Fatalf("t1 MergeStatus: got %q, want 'merged'", t1.Result.MergeStatus)
	}

	t2, err := orchNode.Tasks.Get(ctx, "t2")
	if err != nil {
		t.Fatal(err)
	}
	if t2.Result.MergeStatus != "merged" {
		t.Fatalf("t2 MergeStatus: got %q, want 'merged'", t2.Result.MergeStatus)
	}

	// Worker branches were deleted after merge.
	for _, branch := range []string{"agent/worker-1/t1", "agent/worker-1/t2"} {
		cmd := exec.Command("git", "branch", "--list", branch)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git branch --list %s: %s: %v", branch, out, err)
		}
		if strings.TrimSpace(string(out)) != "" {
			t.Fatalf("expected branch %q to be deleted after merge, but it still exists", branch)
		}
	}

	// Both tasks were executed by the worker.
	mu.Lock()
	if len(executed) != 2 {
		t.Fatalf("expected 2 tasks executed, got %d: %v", len(executed), executed)
	}
	mu.Unlock()
}
