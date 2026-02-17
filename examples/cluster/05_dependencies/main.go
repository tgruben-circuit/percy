// Example 05_dependencies demonstrates the Orchestrator's dependency DAG execution.
// It shows how tasks with dependencies are held back until their prerequisites
// complete, giving you a topological-order execution of a task graph.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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

	// Keep track of the execution order for the final summary.
	var executionOrder []string

	// --- Step 1: Create a temp directory for JetStream storage ---------------
	storeDir, err := os.MkdirTemp("", "percy-deps-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(storeDir)
	fmt.Println("📁 Created temp store dir:", storeDir)

	// --- Step 2: Start a node with embedded NATS ----------------------------
	fmt.Println("\n🚀 Starting node with embedded NATS...")
	node, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "orchestrator-1",
		AgentName:    "Orchestrator",
		Capabilities: []string{"plan", "orchestrate"},
		ListenAddr:   ":0",
		StoreDir:     storeDir,
	})
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}
	defer node.Stop()
	fmt.Printf("✅ Node started — NATS URL: %s\n", node.ClientURL())

	// --- Step 3: Create an Orchestrator -------------------------------------
	fmt.Println("\n🎯 Creating Orchestrator...")
	orch := cluster.NewOrchestrator(node)
	fmt.Println("✅ Orchestrator created")

	// --- Step 4: Define a TaskPlan with 3 tasks and dependencies ------------
	//
	//   DAG visualization:
	//
	//       ┌─────────┐
	//       │  Task A  │   "Setup database schema"
	//       │ (no deps)│
	//       └────┬─────┘
	//            │
	//       ┌────┴─────┐
	//       ▼          │
	//   ┌─────────┐    │
	//   │  Task B  │   │  "Build API endpoints" (depends on A)
	//   └────┬────┘    │
	//        │         │
	//        ▼         ▼
	//      ┌─────────────┐
	//      │   Task C     │  "Integration tests" (depends on A AND B)
	//      └─────────────┘
	//

	fmt.Println("\n📋 Defining task plan (DAG):")
	fmt.Println("   Task A: Setup database schema       — no dependencies")
	fmt.Println("   Task B: Build API endpoints         — depends on A")
	fmt.Println("   Task C: Integration tests           — depends on A, B")

	plan := cluster.TaskPlan{
		Tasks: []cluster.PlannedTask{
			{
				Task: cluster.Task{
					ID:          "task-a",
					Type:        cluster.TaskTypeImplement,
					Priority:    1,
					Title:       "Setup database schema",
					Description: "Create tables, indexes, and seed data for the application database.",
				},
				DependsOn: nil, // root task — no deps
			},
			{
				Task: cluster.Task{
					ID:          "task-b",
					Type:        cluster.TaskTypeImplement,
					Priority:    2,
					Title:       "Build API endpoints",
					Description: "Implement REST endpoints using the database schema from Task A.",
				},
				DependsOn: []string{"task-a"},
			},
			{
				Task: cluster.Task{
					ID:          "task-c",
					Type:        cluster.TaskTypeTest,
					Priority:    3,
					Title:       "Integration tests",
					Description: "Run end-to-end tests against the API with a live database.",
				},
				DependsOn: []string{"task-a", "task-b"},
			},
		},
	}

	// --- Step 5: Submit the plan — only root tasks get submitted ------------
	fmt.Println("\n📤 Submitting plan...")
	if err := orch.SubmitPlan(ctx, plan); err != nil {
		return fmt.Errorf("submit plan: %w", err)
	}

	submitted, err := node.Tasks.ListByStatus(ctx, cluster.TaskStatusSubmitted)
	if err != nil {
		return fmt.Errorf("list submitted: %w", err)
	}
	fmt.Printf("✅ Submitted tasks: %d\n", len(submitted))
	for _, t := range submitted {
		fmt.Printf("   🟢 %s — %q\n", t.ID, t.Title)
	}

	// --- Step 6: Show pending tasks (blocked by dependencies) ---------------
	fmt.Println("\n⏳ Pending tasks (blocked by dependencies):")
	pending := orch.PendingTasks()
	fmt.Printf("   Count: %d\n", len(pending))
	for _, pt := range pending {
		fmt.Printf("   🔒 %s — %q  (needs: %s)\n",
			pt.Task.ID, pt.Task.Title, strings.Join(pt.DependsOn, ", "))
	}

	// --- Step 7: Manually complete Task A -----------------------------------
	fmt.Println("\n⚙️  Simulating work on Task A...")
	if err := completeTask(ctx, node, "task-a", "Schema created: users, orders, products tables."); err != nil {
		return fmt.Errorf("complete task-a: %w", err)
	}
	executionOrder = append(executionOrder, "task-a")
	fmt.Println("✅ Task A completed!")

	// --- Step 8: Resolve dependencies — Task B should unblock --------------
	fmt.Println("\n🔓 Resolving dependencies after Task A completion...")
	unblocked, err := orch.ResolveDependencies(ctx)
	if err != nil {
		return fmt.Errorf("resolve deps (round 1): %w", err)
	}
	fmt.Printf("   Newly unblocked: %d\n", len(unblocked))
	for _, t := range unblocked {
		fmt.Printf("   🟢 %s — %q\n", t.ID, t.Title)
	}

	// Verify Task C is still pending
	fmt.Println("\n🔍 Checking if Task C is still blocked...")
	taskC, err := node.Tasks.Get(ctx, "task-c")
	if err != nil {
		// Task C hasn't been submitted to the queue yet — it's only in the plan.
		fmt.Println("   🔒 Task C is NOT in the queue yet (still pending in the plan)")
	} else {
		fmt.Printf("   Task C status: %s\n", taskC.Status)
	}

	// Double-check via PendingTasks — but note PendingTasks returns all tasks
	// with deps, regardless of whether they've been submitted. Instead, check
	// that task-c was NOT in the unblocked set.
	cStillBlocked := true
	for _, t := range unblocked {
		if t.ID == "task-c" {
			cStillBlocked = false
		}
	}
	if cStillBlocked {
		fmt.Println("   ✅ Task C is still blocked (needs Task B to complete)")
	}

	// --- Step 9: Manually complete Task B -----------------------------------
	fmt.Println("\n⚙️  Simulating work on Task B...")
	if err := completeTask(ctx, node, "task-b", "API endpoints: /users, /orders, /products implemented."); err != nil {
		return fmt.Errorf("complete task-b: %w", err)
	}
	executionOrder = append(executionOrder, "task-b")
	fmt.Println("✅ Task B completed!")

	// --- Step 10: Resolve dependencies again — Task C should unblock --------
	fmt.Println("\n🔓 Resolving dependencies after Task B completion...")
	unblocked, err = orch.ResolveDependencies(ctx)
	if err != nil {
		return fmt.Errorf("resolve deps (round 2): %w", err)
	}
	fmt.Printf("   Newly unblocked: %d\n", len(unblocked))
	for _, t := range unblocked {
		fmt.Printf("   🟢 %s — %q\n", t.ID, t.Title)
	}

	// Simulate completing Task C
	fmt.Println("\n⚙️  Simulating work on Task C...")
	if err := completeTask(ctx, node, "task-c", "All 47 integration tests passing."); err != nil {
		return fmt.Errorf("complete task-c: %w", err)
	}
	executionOrder = append(executionOrder, "task-c")
	fmt.Println("✅ Task C completed!")

	// --- Step 11: Print the full DAG execution order ------------------------
	fmt.Println("\n" + strings.Repeat("═", 60))
	fmt.Println("📊 DAG Execution Summary")
	fmt.Println(strings.Repeat("═", 60))
	fmt.Println()

	// Print DAG visualization
	fmt.Println("   Dependency Graph:")
	fmt.Println("   ┌─────────────────────────────────┐")
	fmt.Println("   │  Task A: Setup database schema   │")
	fmt.Println("   └──────────┬──────────────────┬────┘")
	fmt.Println("              │                  │")
	fmt.Println("              ▼                  │")
	fmt.Println("   ┌──────────────────────────┐   │")
	fmt.Println("   │  Task B: Build API endpts │   │")
	fmt.Println("   └─────────────┬────────────┘   │")
	fmt.Println("                 │                │")
	fmt.Println("                 ▼                ▼")
	fmt.Println("   ┌──────────────────────────────────┐")
	fmt.Println("   │  Task C: Integration tests        │")
	fmt.Println("   └──────────────────────────────────┘")
	fmt.Println()

	fmt.Println("   Execution order:")
	for i, id := range executionOrder {
		task, err := node.Tasks.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("get task %s: %w", id, err)
		}
		fmt.Printf("   %d. ✅ %s — %q\n", i+1, task.ID, task.Title)
		fmt.Printf("      Result: %s\n", task.Result.Summary)
	}

	// List all completed tasks from the queue
	fmt.Println("\n   Final task statuses:")
	completed, err := node.Tasks.ListByStatus(ctx, cluster.TaskStatusCompleted)
	if err != nil {
		return fmt.Errorf("list completed: %w", err)
	}
	for _, t := range completed {
		deps := "none"
		if len(t.DependsOn) > 0 {
			deps = strings.Join(t.DependsOn, ", ")
		}
		fmt.Printf("   ✅ %-8s %-30s deps: [%s]\n", t.ID, t.Title, deps)
	}

	fmt.Println("\n" + strings.Repeat("═", 60))

	// --- Cleanup ------------------------------------------------------------
	fmt.Println("\n🛑 Stopping node...")
	node.Stop()
	fmt.Println("✅ Node stopped cleanly")

	fmt.Println("\n🎉 Dependency DAG execution example complete!")
	return nil
}

// completeTask simulates an agent claiming, working on, and completing a task.
func completeTask(ctx context.Context, node *cluster.Node, taskID, summary string) error {
	agentID := node.Config.AgentID

	// Claim the task
	if err := node.Tasks.Claim(ctx, taskID, agentID); err != nil {
		return fmt.Errorf("claim %s: %w", taskID, err)
	}

	// Transition to working
	if err := node.Tasks.SetWorking(ctx, taskID); err != nil {
		return fmt.Errorf("set working %s: %w", taskID, err)
	}

	// Complete with a result
	result := cluster.TaskResult{
		Summary: summary,
	}
	if err := node.Tasks.Complete(ctx, taskID, result); err != nil {
		return fmt.Errorf("complete %s: %w", taskID, err)
	}

	return nil
}
