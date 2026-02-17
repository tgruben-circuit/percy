// Example 04_worker demonstrates the Worker auto-execution loop with
// capability-based task matching. A Worker polls the task queue, claims tasks
// whose specialization overlaps its node's capabilities, executes a handler,
// and writes the result back. Tasks with non-matching specializations are
// silently skipped.
package main

import (
	"context"
	"fmt"
	"os"
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
	ctx := context.Background()

	// --- Setup: temp dir + node with backend/go capabilities ----------------
	storeDir, err := os.MkdirTemp("", "percy-worker-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(storeDir)
	fmt.Println("📁 Created temp store dir:", storeDir)

	fmt.Println("\n🚀 Starting node with capabilities [backend, go]...")
	node, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "Backend Worker",
		Capabilities: []string{"backend", "go"},
		ListenAddr:   ":0",
		StoreDir:     storeDir,
	})
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}
	defer node.Stop()
	fmt.Printf("✅ Node started — NATS URL: %s\n", node.ClientURL())

	// =========================================================================
	// Scenario 1: Worker claims and executes a task with no specialization
	// =========================================================================
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📌 Scenario 1: Worker Claims and Executes")
	fmt.Println(strings.Repeat("=", 60))

	// --- Step 1: Create a Worker with a handler ----------------------------
	fmt.Println("\n🔧 Creating worker with task handler...")
	worker := cluster.NewWorker(node, func(ctx context.Context, task cluster.Task) cluster.TaskResult {
		fmt.Printf("⚙️  Executing task %q: %s\n", task.ID, task.Title)
		return cluster.TaskResult{
			Branch:  "agent/worker-1/task-1",
			Summary: "Implemented auth module",
		}
	})

	// --- Step 2: Submit a task with no specialization ----------------------
	fmt.Println("\n📝 Submitting task (no specialization — matches any worker)...")
	task1 := cluster.Task{
		ID:        "task-1",
		Type:      cluster.TaskTypeImplement,
		Priority:  1,
		CreatedBy: "orchestrator",
		Title:     "Implement auth module",
		Context: cluster.TaskContext{
			Repo:       "github.com/example/app",
			BaseBranch: "main",
			FilesHint:  []string{"pkg/auth/auth.go"},
		},
	}
	if err := node.Tasks.Submit(ctx, task1); err != nil {
		return fmt.Errorf("submit task-1: %w", err)
	}
	fmt.Println("✅ Task submitted")

	// --- Step 3: Start worker in a goroutine --------------------------------
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	fmt.Println("\n🏃 Starting worker loop in background...")
	go worker.Run(workerCtx)

	// --- Step 4: Poll until the task is completed (timeout 5s) -------------
	fmt.Println("\n⏳ Polling for task completion (timeout 5s)...")
	deadline := time.Now().Add(5 * time.Second)
	var completed *cluster.Task
	for time.Now().Before(deadline) {
		t, err := node.Tasks.Get(ctx, "task-1")
		if err != nil {
			return fmt.Errorf("get task-1: %w", err)
		}
		if t.Status == cluster.TaskStatusCompleted {
			completed = t
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if completed == nil {
		return fmt.Errorf("task-1 did not complete within 5s")
	}

	// --- Step 5: Print completed task with result ---------------------------
	fmt.Println("\n✅ Task completed!")
	fmt.Println("\n📋 Completed Task:")
	fmt.Printf("   ID:          %s\n", completed.ID)
	fmt.Printf("   Title:       %s\n", completed.Title)
	fmt.Printf("   Status:      %s\n", completed.Status)
	fmt.Printf("   Assigned To: %s\n", completed.AssignedTo)
	fmt.Printf("   Branch:      %s\n", completed.Result.Branch)
	fmt.Printf("   Summary:     %s\n", completed.Result.Summary)

	// Stop the worker before scenario 2 so it doesn't interfere.
	workerCancel()
	time.Sleep(100 * time.Millisecond) // let the goroutine exit

	// =========================================================================
	// Scenario 2: Worker skips a task with non-matching specialization
	// =========================================================================
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📌 Scenario 2: Worker Skips Non-Matching Task")
	fmt.Println(strings.Repeat("=", 60))

	// --- Step 1: Submit a task requiring [frontend, react] -----------------
	fmt.Println("\n📝 Submitting task with specialization [frontend, react]...")
	task2 := cluster.Task{
		ID:             "task-2",
		Type:           cluster.TaskTypeImplement,
		Priority:       1,
		Specialization: []string{"frontend", "react"},
		CreatedBy:      "orchestrator",
		Title:          "Build login form component",
		Context: cluster.TaskContext{
			Repo:       "github.com/example/app",
			BaseBranch: "main",
			FilesHint:  []string{"src/components/LoginForm.tsx"},
		},
	}
	if err := node.Tasks.Submit(ctx, task2); err != nil {
		return fmt.Errorf("submit task-2: %w", err)
	}
	fmt.Println("✅ Task submitted")

	// --- Step 2: Start a fresh worker (same node: capabilities [backend, go])
	workerCtx2, workerCancel2 := context.WithCancel(ctx)
	defer workerCancel2()

	worker2 := cluster.NewWorker(node, func(ctx context.Context, task cluster.Task) cluster.TaskResult {
		// This should never be called for task-2.
		fmt.Printf("⚠️  UNEXPECTED: handler called for task %q\n", task.ID)
		return cluster.TaskResult{Branch: "unexpected"}
	})

	fmt.Println("\n🏃 Starting worker (capabilities [backend, go])...")
	go worker2.Run(workerCtx2)

	// --- Step 3: Wait 1 second and verify task is still submitted ----------
	fmt.Println("\n⏳ Waiting 1 second for worker to (not) pick up the task...")
	time.Sleep(1 * time.Second)

	t2, err := node.Tasks.Get(ctx, "task-2")
	if err != nil {
		return fmt.Errorf("get task-2: %w", err)
	}

	fmt.Println("\n📋 Task Status After 1 Second:")
	fmt.Printf("   ID:             %s\n", t2.ID)
	fmt.Printf("   Title:          %s\n", t2.Title)
	fmt.Printf("   Specialization: [%s]\n", strings.Join(t2.Specialization, ", "))
	fmt.Printf("   Status:         %s\n", t2.Status)

	if t2.Status == cluster.TaskStatusSubmitted {
		fmt.Println("\n✅ Correct! Worker with [backend, go] skipped [frontend, react] task")
	} else {
		fmt.Printf("\n❌ Unexpected status: %s (expected submitted)\n", t2.Status)
	}

	// Cleanup.
	workerCancel2()

	fmt.Println("\n🛑 Stopping node...")
	node.Stop()
	fmt.Println("✅ Node stopped cleanly")

	fmt.Println("\n🎉 Worker example complete!")
	return nil
}
