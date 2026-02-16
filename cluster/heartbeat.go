package cluster

import (
	"context"
	"log"
	"time"
)

// FindStaleAgents returns agents whose last heartbeat is older than maxAge.
// Agents already marked offline are skipped.
func FindStaleAgents(ctx context.Context, reg *AgentRegistry, maxAge time.Duration) []AgentCard {
	agents, err := reg.List(ctx)
	if err != nil {
		log.Printf("find stale agents: list: %v", err)
		return nil
	}

	cutoff := time.Now().Add(-maxAge)
	var stale []AgentCard
	for _, a := range agents {
		if a.Status == AgentStatusOffline {
			continue
		}
		if a.LastHeartbeat.Before(cutoff) {
			stale = append(stale, a)
		}
	}
	return stale
}

// MarkStaleAgentsOffline finds stale agents and marks them offline.
// It returns the list of agents that were marked offline.
func MarkStaleAgentsOffline(ctx context.Context, reg *AgentRegistry, maxAge time.Duration) []AgentCard {
	stale := FindStaleAgents(ctx, reg, maxAge)
	var marked []AgentCard
	for _, a := range stale {
		if err := reg.UpdateStatus(ctx, a.ID, AgentStatusOffline, ""); err != nil {
			log.Printf("mark stale agent %q offline: %v", a.ID, err)
			continue
		}
		marked = append(marked, a)
	}
	return marked
}
