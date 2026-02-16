package cluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// putTestCard writes an AgentCard directly to the KV store, bypassing
// Register so we can set arbitrary timestamps.
func putTestCard(t *testing.T, ctx context.Context, js jetstream.JetStream, card AgentCard) {
	t.Helper()
	kv, err := js.KeyValue(ctx, BucketAgents)
	if err != nil {
		t.Fatalf("KeyValue: %v", err)
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := kv.Put(ctx, card.ID, data); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// setupTestRegistryWithJS is like setupTestRegistry but also returns the
// JetStream instance so tests can write cards directly.
func setupTestRegistryWithJS(t *testing.T) (*AgentRegistry, jetstream.JetStream, context.Context) {
	t.Helper()

	dir := t.TempDir()
	srv, err := StartEmbeddedNATS(dir, 0)
	if err != nil {
		t.Fatalf("StartEmbeddedNATS: %v", err)
	}
	t.Cleanup(srv.Shutdown)

	ctx := context.Background()
	nc, err := Connect(ctx, srv.ClientURL())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err := SetupJetStream(ctx, nc)
	if err != nil {
		t.Fatalf("SetupJetStream: %v", err)
	}

	reg, err := NewAgentRegistry(js)
	if err != nil {
		t.Fatalf("NewAgentRegistry: %v", err)
	}
	return reg, js, ctx
}

func TestFindStaleAgents_DetectsOldHeartbeat(t *testing.T) {
	reg, js, ctx := setupTestRegistryWithJS(t)

	putTestCard(t, ctx, js, AgentCard{
		ID:            "stale-agent",
		Name:          "Stale Worker",
		Status:        AgentStatusWorking,
		LastHeartbeat: time.Now().Add(-5 * time.Minute),
	})

	stale := FindStaleAgents(ctx, reg, 2*time.Minute)
	if len(stale) != 1 {
		t.Fatalf("FindStaleAgents: got %d agents, want 1", len(stale))
	}
	if stale[0].ID != "stale-agent" {
		t.Errorf("ID: got %q, want %q", stale[0].ID, "stale-agent")
	}
}

func TestFindStaleAgents_SkipsRecentHeartbeat(t *testing.T) {
	reg, _, ctx := setupTestRegistryWithJS(t)

	// Register normally — LastHeartbeat will be time.Now().
	if err := reg.Register(ctx, AgentCard{
		ID:   "fresh-agent",
		Name: "Fresh Worker",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	stale := FindStaleAgents(ctx, reg, 2*time.Minute)
	if len(stale) != 0 {
		t.Fatalf("FindStaleAgents: got %d agents, want 0", len(stale))
	}
}

func TestFindStaleAgents_SkipsOfflineAgents(t *testing.T) {
	reg, js, ctx := setupTestRegistryWithJS(t)

	// Already offline with an old heartbeat — should be skipped.
	putTestCard(t, ctx, js, AgentCard{
		ID:            "offline-agent",
		Name:          "Offline Worker",
		Status:        AgentStatusOffline,
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
	})

	stale := FindStaleAgents(ctx, reg, 2*time.Minute)
	if len(stale) != 0 {
		t.Fatalf("FindStaleAgents: got %d agents, want 0 (offline should be skipped)", len(stale))
	}
}

func TestMarkStaleAgentsOffline(t *testing.T) {
	reg, js, ctx := setupTestRegistryWithJS(t)

	// One stale working agent.
	putTestCard(t, ctx, js, AgentCard{
		ID:            "stale-worker",
		Name:          "Stale Worker",
		Status:        AgentStatusWorking,
		LastHeartbeat: time.Now().Add(-5 * time.Minute),
	})

	// One fresh idle agent.
	if err := reg.Register(ctx, AgentCard{
		ID:   "fresh-idle",
		Name: "Fresh Idle",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// One already-offline agent with old heartbeat.
	putTestCard(t, ctx, js, AgentCard{
		ID:            "already-offline",
		Name:          "Already Offline",
		Status:        AgentStatusOffline,
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
	})

	marked := MarkStaleAgentsOffline(ctx, reg, 2*time.Minute)
	if len(marked) != 1 {
		t.Fatalf("MarkStaleAgentsOffline: got %d agents, want 1", len(marked))
	}
	if marked[0].ID != "stale-worker" {
		t.Errorf("ID: got %q, want %q", marked[0].ID, "stale-worker")
	}

	// Verify the agent is now offline in the registry.
	got, err := reg.Get(ctx, "stale-worker")
	if err != nil {
		t.Fatalf("Get after mark: %v", err)
	}
	if got.Status != AgentStatusOffline {
		t.Errorf("Status: got %q, want %q", got.Status, AgentStatusOffline)
	}

	// Verify the fresh agent was NOT changed.
	fresh, err := reg.Get(ctx, "fresh-idle")
	if err != nil {
		t.Fatalf("Get fresh: %v", err)
	}
	if fresh.Status != AgentStatusIdle {
		t.Errorf("Fresh status: got %q, want %q", fresh.Status, AgentStatusIdle)
	}
}
