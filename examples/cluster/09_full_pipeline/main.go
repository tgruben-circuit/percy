// Example 09: Full End-to-End Cluster Pipeline
//
// Capstone example that ties everything together: orchestrator node, worker
// node, task plan with dependencies, git branching, and monitoring.
//
// Pipeline:
//   task-1 (Create data model)  ──┐
//                                  ├──▶ task-3 (Integration tests)
//   task-2 (Add API routes)    ──┘
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgruben-circuit/percy/cluster"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Step 1: Create a temp git repo with initial commit ──────────────
	fmt.Println("📁 Step 1: Setting up temp git repository...")

	repoDir, err := os.MkdirTemp("", "percy-pipeline-repo-*")
	if err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}
	defer os.RemoveAll(repoDir)

	for _, args := range [][]string{
		{"git", "-C", repoDir, "init"},
		{"git", "-C", repoDir, "config", "user.email", "percy@example.com"},
		{"git", "-C", repoDir, "config", "user.name", "Percy Bot"},
	} {
		if out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s: %w", strings.Join(args, " "), out, err)
		}
	}

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Percy Pipeline Demo\n"), 0644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}

	for _, args := range [][]string{
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "Initial commit"},
		{"git", "-C", repoDir, "branch", "-M", "main"},
	} {
		if out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s: %w", strings.Join(args, " "), out, err)
		}
	}

	fmt.Printf("   ✅ Git repo initialised at %s\n", repoDir)

	// ── Step 2: Start orchestrator node (embedded NATS) ─────────────────
	fmt.Println("\n🚀 Step 2: Starting orchestrator node...")

	storeDir, err := os.MkdirTemp("", "percy-pipeline-store-*")
	if err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}
	defer os.RemoveAll(storeDir)

	orchNode, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "orch-1",
		AgentName:    "Orchestrator",
		Capabilities: []string{"orchestrator"},
		ListenAddr:   ":0",
		StoreDir:     storeDir,
	})
	if err != nil {
		return fmt.Errorf("start orchestrator node: %w", err)
	}
	defer orchNode.Stop()

	fmt.Printf("   ✅ Orchestrator running — NATS URL: %s\n", orchNode.ClientURL())

	// ── Step 3: Start worker node (connects to orchestrator) ────────────
	fmt.Println("\n🚀 Step 3: Starting worker node...")

	workerNode, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "Worker",
		Capabilities: []string{"backend", "frontend"},
		NATSUrl:      orchNode.ClientURL(),
	})
	if err != nil {
		return fmt.Errorf("start worker node: %w", err)
	}
	defer workerNode.Stop()

	fmt.Println("   ✅ Worker connected to cluster")

	// ── Step 4: Create task plan with dependencies ──────────────────────
	fmt.Println("\n📋 Step 4: Creating task plan...")

	plan := cluster.TaskPlan{
		Tasks: []cluster.PlannedTask{
			{
				Task: cluster.Task{
					ID:             "task-1",
					Type:           cluster.TaskTypeImplement,
					Title:          "Create data model",
					Description:    "Define the core data structures for the application.",
					Specialization: []string{"backend"},
					Priority:       1,
					Context:        cluster.TaskContext{Repo: repoDir, BaseBranch: "main"},
				},
				DependsOn: nil,
			},
			{
				Task: cluster.Task{
					ID:             "task-2",
					Type:           cluster.TaskTypeImplement,
					Title:          "Add API routes",
					Description:    "Implement RESTful API endpoints.",
					Specialization: []string{"backend"},
					Priority:       1,
					Context:        cluster.TaskContext{Repo: repoDir, BaseBranch: "main"},
				},
				DependsOn: nil,
			},
			{
				Task: cluster.Task{
					ID:             "task-3",
					Type:           cluster.TaskTypeTest,
					Title:          "Integration tests",
					Description:    "Write integration tests covering model and API layers.",
					Specialization: []string{"backend"},
					Priority:       2,
					Context:        cluster.TaskContext{Repo: repoDir, BaseBranch: "main"},
				},
				DependsOn: []string{"task-1", "task-2"},
			},
		},
	}

	fmt.Println("   task-1: Create data model   (implement, no deps)")
	fmt.Println("   task-2: Add API routes      (implement, no deps)")
	fmt.Println("   task-3: Integration tests   (test, depends on task-1 & task-2)")

	// ── Step 5: Create Orchestrator & set working branch ────────────────
	fmt.Println("\n⚙️  Step 5: Creating orchestrator and setting working branch...")

	orch := cluster.NewOrchestrator(orchNode)
	orch.SetWorkingBranch("main")

	fmt.Printf("   ✅ Working branch: %s\n", orch.WorkingBranch())

	// ── Step 6: Submit plan ─────────────────────────────────────────────
	fmt.Println("\n📤 Step 6: Submitting plan...")

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		return fmt.Errorf("submit plan: %w", err)
	}

	submitted, err := orchNode.Tasks.ListByStatus(ctx, cluster.TaskStatusSubmitted)
	if err != nil {
		return fmt.Errorf("list submitted: %w", err)
	}

	pending := orch.PendingTasks()

	fmt.Printf("   ✅ Submitted immediately: %d tasks\n", len(submitted))
	for _, t := range submitted {
		fmt.Printf("      • %s — %s\n", t.ID, t.Title)
	}
	fmt.Printf("   ⏳ Waiting on dependencies: %d tasks\n", len(pending))
	for _, pt := range pending {
		fmt.Printf("      • %s — depends on %v\n", pt.Task.ID, pt.DependsOn)
	}

	// ── Step 7: Create Worker with git-branching handler ────────────────
	fmt.Println("\n🔧 Step 7: Creating worker with git-branching handler...")

	handler := func(ctx context.Context, task cluster.Task) cluster.TaskResult {
		agentID := workerNode.Config.AgentID
		branchName := fmt.Sprintf("agent/%s/%s", agentID, task.ID)

		// Pick a filename based on task title.
		var filename, content string
		switch task.ID {
		case "task-1":
			filename = "model.go"
			content = "package main\n\n// User represents a user in the system.\ntype User struct {\n\tID   int\n\tName string\n}\n"
		case "task-2":
			filename = "routes.go"
			content = "package main\n\nimport \"net/http\"\n\nfunc setupRoutes(mux *http.ServeMux) {\n\tmux.HandleFunc(\"/users\", handleUsers)\n}\n"
		case "task-3":
			filename = "integration_test.go"
			content = "package main\n\nimport \"testing\"\n\nfunc TestIntegration(t *testing.T) {\n\t// Integration test placeholder\n}\n"
		default:
			filename = task.ID + ".go"
			content = fmt.Sprintf("package main // %s\n", task.Title)
		}

		fmt.Printf("      🛠️  [%s] Working on: %s → branch %s\n", agentID, task.Title, branchName)

		// Create branch from main.
		if out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "branch", branchName, "main").CombinedOutput(); err != nil {
			fmt.Printf("      ❌ branch create: %s: %v\n", out, err)
			return cluster.TaskResult{Summary: fmt.Sprintf("branch create failed: %v", err)}
		}

		// Add worktree.
		wtDir, err := os.MkdirTemp("", fmt.Sprintf("percy-wt-%s-*", task.ID))
		if err != nil {
			fmt.Printf("      ❌ mkdtemp: %v\n", err)
			return cluster.TaskResult{Summary: fmt.Sprintf("mkdtemp failed: %v", err)}
		}
		// worktree add needs an empty directory; remove the one MkdirTemp created.
		os.Remove(wtDir)

		if out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "worktree", "add", wtDir, branchName).CombinedOutput(); err != nil {
			fmt.Printf("      ❌ worktree add: %s: %v\n", out, err)
			return cluster.TaskResult{Summary: fmt.Sprintf("worktree add failed: %v", err)}
		}

		// Write the file.
		filePath := filepath.Join(wtDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			fmt.Printf("      ❌ write file: %v\n", err)
			return cluster.TaskResult{Summary: fmt.Sprintf("write file failed: %v", err)}
		}

		// Stage and commit.
		commitMsg := fmt.Sprintf("%s: %s", task.ID, task.Title)
		for _, args := range [][]string{
			{"git", "-C", wtDir, "add", "."},
			{"git", "-C", wtDir, "commit", "-m", commitMsg},
		} {
			if out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
				fmt.Printf("      ❌ %s: %s: %v\n", strings.Join(args[2:], " "), out, err)
				return cluster.TaskResult{Summary: fmt.Sprintf("git commit failed: %v", err)}
			}
		}

		// Remove worktree.
		exec.CommandContext(ctx, "git", "-C", repoDir, "worktree", "remove", wtDir).CombinedOutput()

		summary := fmt.Sprintf("Implemented %q → %s on %s", task.Title, filename, branchName)
		fmt.Printf("      ✅ [%s] Completed: %s\n", agentID, summary)

		return cluster.TaskResult{
			Branch:  branchName,
			Summary: summary,
		}
	}

	worker := cluster.NewWorker(workerNode, handler)
	fmt.Println("   ✅ Worker created with git-branching handler")

	// ── Step 8: Start Monitor (dependency resolution only, no merge) ────
	fmt.Println("\n👁️  Step 8: Starting monitor (dependency resolution mode)...")

	monitor := cluster.NewMonitor(orchNode, orch, nil, nil)

	go monitor.Run(ctx)
	fmt.Println("   ✅ Monitor running — watches task events, resolves dependencies")

	// ── Step 9: Start Worker.Run in goroutine ───────────────────────────
	fmt.Println("\n▶️  Step 9: Starting worker loop...")

	go worker.Run(ctx)
	fmt.Println("   ✅ Worker polling for tasks")

	// ── Step 10: Poll until all 3 tasks are completed ───────────────────
	fmt.Println("\n⏳ Step 10: Waiting for all tasks to complete...")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		completed, err := orchNode.Tasks.ListByStatus(ctx, cluster.TaskStatusCompleted)
		if err != nil {
			return fmt.Errorf("poll completed: %w", err)
		}
		if len(completed) >= 3 {
			fmt.Printf("   🎉 All %d tasks completed!\n", len(completed))
			break
		}

		// Show progress.
		var ids []string
		for _, t := range completed {
			ids = append(ids, t.ID)
		}
		if len(ids) > 0 {
			fmt.Printf("   … completed so far: [%s] (%d/3)\n", strings.Join(ids, ", "), len(ids))
		}

		time.Sleep(500 * time.Millisecond)
	}

	// Check for timeout.
	completed, err := orchNode.Tasks.ListByStatus(ctx, cluster.TaskStatusCompleted)
	if err != nil {
		return fmt.Errorf("final check: %w", err)
	}
	if len(completed) < 3 {
		return fmt.Errorf("timeout: only %d/3 tasks completed", len(completed))
	}

	// ── Step 11: Print final state of all tasks ─────────────────────────
	fmt.Println("\n📊 Step 11: Final task states:")
	fmt.Println("   ┌────────────┬──────────────────────┬───────────┬─────────────────────────────────────────┐")
	fmt.Println("   │ ID         │ Title                │ Status    │ Branch                                  │")
	fmt.Println("   ├────────────┼──────────────────────┼───────────┼─────────────────────────────────────────┤")

	for _, id := range []string{"task-1", "task-2", "task-3"} {
		t, err := orchNode.Tasks.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("get task %s: %w", id, err)
		}
		fmt.Printf("   │ %-10s │ %-20s │ %-9s │ %-39s │\n",
			t.ID, t.Title, t.Status, t.Result.Branch)
	}

	fmt.Println("   └────────────┴──────────────────────┴───────────┴─────────────────────────────────────────┘")

	// Print result summaries.
	fmt.Println("\n📝 Result summaries:")
	for _, id := range []string{"task-1", "task-2", "task-3"} {
		t, _ := orchNode.Tasks.Get(ctx, id)
		fmt.Printf("   %s: %s\n", id, t.Result.Summary)
	}

	// List git branches as verification.
	fmt.Println("\n🌿 Git branches in repo:")
	if out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "branch", "--list").CombinedOutput(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fmt.Printf("   %s\n", line)
		}
	}

	// ── Step 12: Cleanup ────────────────────────────────────────────────
	fmt.Println("\n🛑 Step 12: Stopping nodes...")

	cancel() // signal monitor and worker goroutines to stop

	workerNode.Stop()
	fmt.Println("   ✅ Worker node stopped")

	orchNode.Stop()
	fmt.Println("   ✅ Orchestrator node stopped")

	fmt.Println("\n✨ Full pipeline example complete!")
	fmt.Println("   • 2 nodes (orchestrator + worker) collaborated over NATS")
	fmt.Println("   • 3 tasks executed with dependency ordering (task-3 waited for task-1 & task-2)")
	fmt.Println("   • Each task created a git branch, wrote code, and committed")
	return nil
}
