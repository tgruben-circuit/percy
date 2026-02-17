// Example 02_two_nodes demonstrates multi-node clustering with Percy.
//
// It starts two nodes — an orchestrator with embedded NATS and a worker
// that connects to it — then verifies both appear in the shared agent
// registry as seen from either node.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/tgruben-circuit/percy/cluster"
)

func main() {
	ctx := context.Background()

	// --- Step 1: Start node1 (orchestrator) with embedded NATS on a random port ---
	fmt.Println("🚀 Step 1: Starting node1 (orchestrator) with embedded NATS...")

	storeDir, err := os.MkdirTemp("", "percy-cluster-02-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(storeDir)

	node1, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "node-1",
		AgentName:    "Orchestrator",
		Capabilities: []string{"plan", "review"},
		ListenAddr:   ":0",
		StoreDir:     storeDir,
	})
	if err != nil {
		log.Fatalf("start node1: %v", err)
	}
	defer node1.Stop()

	fmt.Printf("   ✅ node1 running — NATS URL: %s\n", node1.ClientURL())

	// --- Step 2: Start node2 (worker) connecting to node1's NATS ---
	fmt.Println("\n🚀 Step 2: Starting node2 (worker) connecting to node1...")

	node2, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "node-2",
		AgentName:    "Worker",
		Capabilities: []string{"code", "test"},
		NATSUrl:      node1.ClientURL(),
	})
	if err != nil {
		log.Fatalf("start node2: %v", err)
	}
	defer node2.Stop()

	fmt.Println("   ✅ node2 running")

	// --- Step 3: List agents from node1's registry ---
	fmt.Println("\n📋 Step 3: Listing agents from node1's registry...")

	agents1, err := node1.Registry.List(ctx)
	if err != nil {
		log.Fatalf("list agents from node1: %v", err)
	}

	for _, a := range agents1 {
		fmt.Printf("   • %s (%s) — capabilities: %v, status: %s\n",
			a.Name, a.ID, a.Capabilities, a.Status)
	}

	// --- Step 4: List agents from node2's registry (proves distributed state) ---
	fmt.Println("\n📋 Step 4: Listing agents from node2's registry...")

	agents2, err := node2.Registry.List(ctx)
	if err != nil {
		log.Fatalf("list agents from node2: %v", err)
	}

	for _, a := range agents2 {
		fmt.Printf("   • %s (%s) — capabilities: %v, status: %s\n",
			a.Name, a.ID, a.Capabilities, a.Status)
	}

	// --- Step 5: Show orchestrator vs worker roles ---
	fmt.Println("\n🏷️  Step 5: Node roles...")

	printRole := func(name string, n *cluster.Node) {
		role := "worker"
		if n.IsOrchestrator() {
			role = "orchestrator"
		}
		fmt.Printf("   %s → %s\n", name, role)
	}

	printRole("node1", node1)
	printRole("node2", node2)

	// --- Step 6: Stop both nodes ---
	fmt.Println("\n🛑 Step 6: Stopping nodes...")

	node2.Stop()
	fmt.Println("   ✅ node2 stopped")

	node1.Stop()
	fmt.Println("   ✅ node1 stopped")

	// --- Summary ---
	fmt.Println("\n✨ Two-node cluster example complete!")
	fmt.Printf("   Agents seen from node1: %d\n", len(agents1))
	fmt.Printf("   Agents seen from node2: %d\n", len(agents2))
}
