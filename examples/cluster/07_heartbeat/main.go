// Example 07: Stale Agent Detection and Recovery
//
// Demonstrates how Percy detects agents that have stopped heartbeating,
// marks them offline, requeues their in-progress tasks, and releases
// their file locks — ensuring no work is permanently lost when an agent crashes.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tgruben-circuit/percy/cluster"
)

func main() {
	ctx := context.Background()

	// ── Step 1: Start a node with embedded NATS ──────────────────────────
	tmpDir, err := os.MkdirTemp("", "percy-heartbeat-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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
	fmt.Println(" 🩺 Stale Agent Detection & Recovery")
	fmt.Println("═══════════════════════════════════════════════════")

	// ── Step 2: Register a "phantom" agent that will never heartbeat ────
	phantomID := "agent-phantom"
	phantomCard := cluster.AgentCard{
		ID:           phantomID,
		Name:         "Phantom Worker",
		Capabilities: []string{"code", "test"},
	}
	if err := node.Registry.Register(ctx, phantomCard); err != nil {
		log.Fatalf("register phantom: %v", err)
	}

	card, err := node.Registry.Get(ctx, phantomID)
	if err != nil {
		log.Fatalf("get phantom: %v", err)
	}
	fmt.Printf("\n👻 Registered phantom agent\n")
	fmt.Printf("   ID:             %s\n", card.ID)
	fmt.Printf("   Status:         %s\n", card.Status)
	fmt.Printf("   Last Heartbeat: %s\n", card.LastHeartbeat.Format("15:04:05.000"))

	// ── Step 3: Submit a task, claim it for the phantom, set to working ──
	task := cluster.Task{
		ID:    "task-build-auth",
		Type:  cluster.TaskTypeImplement,
		Title: "Build authentication module",
		Context: cluster.TaskContext{
			Repo:       "github.com/example/myapp",
			BaseBranch: "main",
		},
	}
	if err := node.Tasks.Submit(ctx, task); err != nil {
		log.Fatalf("submit task: %v", err)
	}
	if err := node.Tasks.Claim(ctx, task.ID, phantomID); err != nil {
		log.Fatalf("claim task: %v", err)
	}
	if err := node.Tasks.SetWorking(ctx, task.ID); err != nil {
		log.Fatalf("set working: %v", err)
	}

	t, _ := node.Tasks.Get(ctx, task.ID)
	fmt.Printf("\n📋 Task assigned to phantom\n")
	fmt.Printf("   Task:       %s\n", t.ID)
	fmt.Printf("   Status:     %s\n", t.Status)
	fmt.Printf("   AssignedTo: %s\n", t.AssignedTo)

	// ── Step 4: Phantom acquires file locks ─────────────────────────────
	repo := "github.com/example/myapp"
	if err := node.Locks.Acquire(ctx, repo, "pkg/auth/handler.go", phantomID, task.ID); err != nil {
		log.Fatalf("acquire lock 1: %v", err)
	}
	if err := node.Locks.Acquire(ctx, repo, "pkg/auth/middleware.go", phantomID, task.ID); err != nil {
		log.Fatalf("acquire lock 2: %v", err)
	}
	fmt.Printf("\n🔒 Phantom holds 2 file locks\n")

	// ── Step 5: Wait for the phantom's heartbeat to go stale ────────────
	fmt.Printf("\n⏳ Waiting 2 seconds for heartbeat to become stale...\n")
	time.Sleep(2 * time.Second)

	maxAge := 1 * time.Second

	staleAgents := cluster.FindStaleAgents(ctx, node.Registry, maxAge)
	fmt.Printf("\n🔍 FindStaleAgents (maxAge=%s)\n", maxAge)
	fmt.Printf("   Stale agents found: %d\n", len(staleAgents))
	for _, a := range staleAgents {
		fmt.Printf("   → %s (%s) — last heartbeat: %s\n",
			a.ID, a.Name, a.LastHeartbeat.Format("15:04:05.000"))
	}

	// ── Step 6: Mark stale agents offline ────────────────────────────────
	marked := cluster.MarkStaleAgentsOffline(ctx, node.Registry, maxAge)
	fmt.Printf("\n🚫 MarkStaleAgentsOffline\n")
	fmt.Printf("   Agents marked offline: %d\n", len(marked))

	updated, err := node.Registry.Get(ctx, phantomID)
	if err != nil {
		log.Fatalf("get phantom after mark: %v", err)
	}
	fmt.Printf("   → %s status is now: %s\n", updated.ID, updated.Status)

	// ── Step 7: Requeue the phantom's tasks (same pattern as Monitor) ────
	fmt.Printf("\n♻️  Requeuing phantom's tasks...\n")
	requeued := 0
	for _, status := range []cluster.TaskStatus{cluster.TaskStatusAssigned, cluster.TaskStatusWorking} {
		tasks, err := node.Tasks.ListByStatus(ctx, status)
		if err != nil {
			log.Fatalf("list tasks by status %s: %v", status, err)
		}
		for _, tk := range tasks {
			if tk.AssignedTo != phantomID {
				continue
			}
			if err := node.Tasks.Requeue(ctx, tk.ID); err != nil {
				log.Fatalf("requeue task %s: %v", tk.ID, err)
			}
			fmt.Printf("   → requeued task: %s\n", tk.ID)
			requeued++
		}
	}
	fmt.Printf("   Total requeued: %d\n", requeued)

	// ── Step 8: Release locks held by the phantom ──────────────────────
	released, err := node.Locks.ReleaseByAgent(ctx, phantomID)
	if err != nil {
		log.Fatalf("release locks: %v", err)
	}
	fmt.Printf("\n🔓 Released %d lock(s) held by %s\n", released, phantomID)

	// ── Step 9: Verify the task is back to submitted ────────────────────
	recovered, err := node.Tasks.Get(ctx, task.ID)
	if err != nil {
		log.Fatalf("get recovered task: %v", err)
	}
	fmt.Printf("\n✅ Task recovery complete\n")
	fmt.Printf("   Task:       %s\n", recovered.ID)
	fmt.Printf("   Status:     %s  (ready for a new worker!)\n", recovered.Status)
	fmt.Printf("   AssignedTo: %q\n", recovered.AssignedTo)

	// ── Step 10: Summary ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println(" 📊 Recovery Summary")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("   Stale agents detected:  %d\n", len(staleAgents))
	fmt.Printf("   Agents marked offline:  %d\n", len(marked))
	fmt.Printf("   Tasks requeued:         %d\n", requeued)
	fmt.Printf("   Locks released:         %d\n", released)
	fmt.Println()
	fmt.Println("🎉 Stale agent detection & recovery complete!")
}
