// Example 06_locks demonstrates distributed file locking across Percy cluster nodes.
//
// Two nodes share a NATS JetStream KV-backed lock store. The example walks
// through acquiring, contending, releasing, and bulk-releasing locks to show
// how Percy prevents concurrent edits to the same file.
package main

import (
	"context"
	"fmt"
	"os"

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

	// --- Step 1: Start two nodes (node1 embedded NATS, node2 connects) ------
	fmt.Println("🚀 Step 1: Starting two cluster nodes...")

	storeDir, err := os.MkdirTemp("", "percy-locks-06-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(storeDir)

	node1, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "agent-1",
		AgentName:    "Agent One",
		Capabilities: []string{"code", "review"},
		ListenAddr:   ":0",
		StoreDir:     storeDir,
	})
	if err != nil {
		return fmt.Errorf("start node1: %w", err)
	}
	defer node1.Stop()

	node2, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "agent-2",
		AgentName:    "Agent Two",
		Capabilities: []string{"code", "test"},
		NATSUrl:      node1.ClientURL(),
	})
	if err != nil {
		return fmt.Errorf("start node2: %w", err)
	}
	defer node2.Stop()

	fmt.Printf("   ✅ node1 (agent-1) running — NATS URL: %s\n", node1.ClientURL())
	fmt.Println("   ✅ node2 (agent-2) running")

	// --- Step 2: Node1 acquires lock on repo "myproject", file "src/main.go" -
	fmt.Println("\n🔒 Step 2: agent-1 acquires lock on myproject/src/main.go...")

	err = node1.Locks.Acquire(ctx, "myproject", "src/main.go", "agent-1", "task-1")
	if err != nil {
		return fmt.Errorf("agent-1 acquire lock: %w", err)
	}
	fmt.Println("   ✅ Lock acquired by agent-1")

	// --- Step 3: Show the lock info -------------------------------------------
	fmt.Println("\n🔍 Step 3: Inspecting lock info...")

	lock, err := node1.Locks.Get(ctx, "myproject", "src/main.go")
	if err != nil {
		return fmt.Errorf("get lock info: %w", err)
	}
	fmt.Printf("   📋 Lock holder:  %s\n", lock.AgentID)
	fmt.Printf("   📋 Task:         %s\n", lock.TaskID)
	fmt.Printf("   📋 Locked at:    %s\n", lock.LockedAt.Format("15:04:05.000"))

	// --- Step 4: Node2 tries to acquire same lock — contention! --------------
	fmt.Println("\n⚡ Step 4: agent-2 tries to acquire the SAME lock...")

	err = node2.Locks.Acquire(ctx, "myproject", "src/main.go", "agent-2", "task-2")

	// --- Step 5: Print the contention error -----------------------------------
	if err != nil {
		fmt.Printf("   🚫 Contention! agent-2 failed: %v\n", err)
	} else {
		return fmt.Errorf("expected contention error but lock was acquired")
	}

	// --- Step 6: Node1 releases the lock --------------------------------------
	fmt.Println("\n🔓 Step 6: agent-1 releases the lock...")

	err = node1.Locks.Release(ctx, "myproject", "src/main.go")
	if err != nil {
		return fmt.Errorf("agent-1 release lock: %w", err)
	}
	fmt.Println("   ✅ Lock released by agent-1")

	// --- Step 7: Node2 acquires the lock successfully -------------------------
	fmt.Println("\n🔒 Step 7: agent-2 acquires the lock now...")

	err = node2.Locks.Acquire(ctx, "myproject", "src/main.go", "agent-2", "task-2")
	if err != nil {
		return fmt.Errorf("agent-2 acquire lock: %w", err)
	}
	fmt.Println("   ✅ Lock acquired by agent-2")

	// --- Step 8: Show the lock is now held by agent-2 -------------------------
	fmt.Println("\n🔍 Step 8: Verifying lock is now held by agent-2...")

	lock, err = node2.Locks.Get(ctx, "myproject", "src/main.go")
	if err != nil {
		return fmt.Errorf("get lock info: %w", err)
	}
	fmt.Printf("   📋 Lock holder:  %s\n", lock.AgentID)
	fmt.Printf("   📋 Task:         %s\n", lock.TaskID)
	fmt.Printf("   📋 Locked at:    %s\n", lock.LockedAt.Format("15:04:05.000"))

	// --- Step 9: ReleaseByAgent — bulk release --------------------------------
	fmt.Println("\n🔒 Step 9: agent-2 acquires 3 more locks on different files...")

	extraFiles := []string{"src/utils.go", "src/config.go", "src/handler.go"}
	for _, f := range extraFiles {
		err = node2.Locks.Acquire(ctx, "myproject", f, "agent-2", "task-2")
		if err != nil {
			return fmt.Errorf("agent-2 acquire lock on %s: %w", f, err)
		}
		fmt.Printf("   🔒 Locked: %s\n", f)
	}

	fmt.Println("\n🧹 ReleaseByAgent: releasing ALL locks held by agent-2...")

	count, err := node2.Locks.ReleaseByAgent(ctx, "agent-2")
	if err != nil {
		return fmt.Errorf("release by agent: %w", err)
	}

	// --- Step 10: Show the count of released locks ----------------------------
	fmt.Printf("\n📊 Step 10: Released %d lock(s) held by agent-2\n", count)

	// --- Cleanup --------------------------------------------------------------
	fmt.Println("\n🛑 Stopping nodes...")
	node2.Stop()
	fmt.Println("   ✅ node2 stopped")
	node1.Stop()
	fmt.Println("   ✅ node1 stopped")

	fmt.Println("\n✨ Distributed file locking example complete!")
	return nil
}
