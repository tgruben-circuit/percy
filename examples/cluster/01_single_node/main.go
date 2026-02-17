// Example 01_single_node demonstrates bootstrapping a single Percy cluster node
// with an embedded NATS server, registering itself, and querying the agent registry.
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

	// --- Step 1: Create a temp directory for JetStream storage ---------------
	storeDir, err := os.MkdirTemp("", "percy-single-node-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(storeDir)
	fmt.Println("📁 Created temp store dir:", storeDir)

	// --- Step 2: Start a node with embedded NATS on a random port -----------
	fmt.Println("\n🚀 Starting single node with embedded NATS...")
	node, err := cluster.StartNode(ctx, cluster.NodeConfig{
		AgentID:      "agent-alpha",
		AgentName:    "Alpha",
		Capabilities: []string{"code", "test", "review"},
		ListenAddr:   ":0", // random available port
		StoreDir:     storeDir,
	})
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}
	defer node.Stop()

	fmt.Printf("✅ Node started — NATS URL: %s\n", node.ClientURL())
	fmt.Printf("👑 Is orchestrator (embedded NATS): %v\n", node.IsOrchestrator())

	// --- Step 3: Verify the node registered itself --------------------------
	fmt.Println("\n🔍 Looking up agent-alpha in the registry...")
	card, err := node.Registry.Get(ctx, "agent-alpha")
	if err != nil {
		return fmt.Errorf("registry get: %w", err)
	}
	fmt.Println("✅ Agent found in registry")

	// --- Step 4: Print the agent card details --------------------------------
	fmt.Println("\n📇 Agent Card:")
	fmt.Printf("   ID:             %s\n", card.ID)
	fmt.Printf("   Name:           %s\n", card.Name)
	fmt.Printf("   Capabilities:   [%s]\n", strings.Join(card.Capabilities, ", "))
	fmt.Printf("   Status:         %s\n", card.Status)
	fmt.Printf("   Started At:     %s\n", card.StartedAt.Format("15:04:05.000"))
	fmt.Printf("   Last Heartbeat: %s\n", card.LastHeartbeat.Format("15:04:05.000"))

	// --- Step 5: List all agents (should be exactly one) --------------------
	fmt.Println("\n📋 Listing all agents in the cluster...")
	agents, err := node.Registry.List(ctx)
	if err != nil {
		return fmt.Errorf("registry list: %w", err)
	}
	fmt.Printf("   Total agents: %d\n", len(agents))
	for i, a := range agents {
		fmt.Printf("   [%d] %s (%s) — status: %s, capabilities: [%s]\n",
			i+1, a.Name, a.ID, a.Status, strings.Join(a.Capabilities, ", "))
	}

	// --- Step 6: Stop the node -----------------------------------------------
	fmt.Println("\n🛑 Stopping node...")
	node.Stop()
	fmt.Println("✅ Node stopped cleanly")

	fmt.Println("\n🎉 Single-node bootstrap example complete!")
	return nil
}
