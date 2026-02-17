// Example 03: Task Lifecycle with CAS Concurrency
//
// Demonstrates the full lifecycle of a task from submission to completion,
// and shows how CAS (compare-and-swap) prevents double-claiming.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tgruben-circuit/percy/cluster"
)

func main() {
	ctx := context.Background()

	// Create a temporary directory for NATS storage.
	tmpDir, err := os.MkdirTemp("", "percy-task-lifecycle-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Start a node with embedded NATS.
	node, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "orchestrator-1",
		AgentName:    "Orchestrator",
		Capabilities: []string{"orchestrate"},
		ListenAddr:   ":0",
		StoreDir:     tmpDir,
	})
	if err != nil {
		log.Fatalf("start node: %v", err)
	}
	defer node.Stop()

	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println(" Scenario 1: Full Task Lifecycle")
	fmt.Println("═══════════════════════════════════════════════════")

	// ── Step 1: Submit a task ──────────────────────────────────────────
	task := cluster.Task{
		ID:    "task-auth-001",
		Type:  cluster.TaskTypeImplement,
		Title: "Add user authentication",
		Description: "Implement JWT-based authentication middleware " +
			"with login and refresh token endpoints.",
		Specialization: []string{"go", "security"},
		Priority:       1,
		Context: cluster.TaskContext{
			Repo:       "github.com/example/myapp",
			BaseBranch: "main",
		},
		DependsOn: []string{},
	}

	if err := node.Tasks.Submit(ctx, task); err != nil {
		log.Fatalf("submit task: %v", err)
	}

	printTaskState(ctx, node, task.ID, "📋 Submitted")

	// ── Step 2: Claim the task for an agent ────────────────────────────
	if err := node.Tasks.Claim(ctx, task.ID, "agent-alpha"); err != nil {
		log.Fatalf("claim task: %v", err)
	}

	printTaskState(ctx, node, task.ID, "🤝 Claimed by agent")

	// ── Step 3: Set task to working ────────────────────────────────────
	if err := node.Tasks.SetWorking(ctx, task.ID); err != nil {
		log.Fatalf("set working: %v", err)
	}

	printTaskState(ctx, node, task.ID, "🔨 Agent is working")

	// ── Step 4: Complete with result ───────────────────────────────────
	result := cluster.TaskResult{
		Branch:  "feat/user-authentication",
		Summary: "Added JWT auth middleware, login endpoint, and refresh token rotation.",
	}
	if err := node.Tasks.Complete(ctx, task.ID, result); err != nil {
		log.Fatalf("complete task: %v", err)
	}

	printTaskState(ctx, node, task.ID, "✅ Completed")

	// Print the full final state.
	final, err := node.Tasks.Get(ctx, task.ID)
	if err != nil {
		log.Fatalf("get final task: %v", err)
	}

	fmt.Println()
	fmt.Println("── Final Task State ──")
	fmt.Printf("   ID:          %s\n", final.ID)
	fmt.Printf("   Type:        %s\n", final.Type)
	fmt.Printf("   Title:       %s\n", final.Title)
	fmt.Printf("   Status:      %s\n", final.Status)
	fmt.Printf("   AssignedTo:  %s\n", final.AssignedTo)
	fmt.Printf("   Branch:      %s\n", final.Result.Branch)
	fmt.Printf("   Summary:     %s\n", final.Result.Summary)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println(" Scenario 2: CAS Double-Claim Prevention")
	fmt.Println("═══════════════════════════════════════════════════")

	// Submit a second task.
	task2 := cluster.Task{
		ID:       "task-refactor-002",
		Type:     cluster.TaskTypeRefactor,
		Title:    "Refactor database layer",
		Priority: 2,
		Context: cluster.TaskContext{
			Repo:       "github.com/example/myapp",
			BaseBranch: "main",
		},
	}

	if err := node.Tasks.Submit(ctx, task2); err != nil {
		log.Fatalf("submit task2: %v", err)
	}

	printTaskState(ctx, node, task2.ID, "📋 Submitted")

	// Agent A claims the task first — this should succeed.
	if err := node.Tasks.Claim(ctx, task2.ID, "agent-a"); err != nil {
		log.Fatalf("agent-a claim: %v", err)
	}

	fmt.Println("\n🏁 agent-a claimed the task successfully")
	printTaskState(ctx, node, task2.ID, "🤝 Claimed by agent-a")

	// Agent B tries to claim the same task — this should fail.
	fmt.Println("\n⏳ agent-b attempts to claim the same task...")
	err = node.Tasks.Claim(ctx, task2.ID, "agent-b")
	if err != nil {
		fmt.Println("\n🛡️  CAS prevented double-claim!")
		fmt.Printf("   Error: %s\n", err)

		// Verify it's the expected error: status is "assigned", not "submitted".
		if strings.Contains(err.Error(), string(cluster.TaskStatusAssigned)) {
			fmt.Println("   ✓ Correctly rejected: task is already assigned")
		}
	} else {
		log.Fatal("❌ agent-b should NOT have been able to claim the task")
	}

	// Confirm the task is still assigned to agent-a.
	verify, err := node.Tasks.Get(ctx, task2.ID)
	if err != nil {
		log.Fatalf("verify task: %v", err)
	}

	fmt.Println()
	fmt.Println("── Verification ──")
	fmt.Printf("   Task %s is still assigned to: %s\n", verify.ID, verify.AssignedTo)
	fmt.Printf("   Status: %s\n", verify.Status)

	fmt.Println()
	fmt.Println("✨ All scenarios completed successfully!")
}

// printTaskState fetches a task and prints its current status with a label.
func printTaskState(ctx context.Context, node *cluster.Node, taskID, label string) {
	t, err := node.Tasks.Get(ctx, taskID)
	if err != nil {
		log.Fatalf("get task %q: %v", taskID, err)
	}
	fmt.Printf("\n%s\n", label)
	fmt.Printf("   → status=%s  assignedTo=%q\n", t.Status, t.AssignedTo)
}
