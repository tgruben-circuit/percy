package cluster

import (
	"context"
	"log/slog"
	"time"
)

// TaskHandler is called when a worker claims a task.
type TaskHandler func(ctx context.Context, task Task) TaskResult

// Worker watches for available tasks and executes them.
type Worker struct {
	node    *Node
	handler TaskHandler
	busy    bool
}

// NewWorker creates a Worker that polls the node's task queue and dispatches
// matching tasks to the given handler.
func NewWorker(node *Node, handler TaskHandler) *Worker {
	return &Worker{
		node:    node,
		handler: handler,
	}
}

// Run polls for tasks every 500ms. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if w.busy {
				continue
			}
			if err := w.tryClaimAndExecute(ctx); err != nil {
				slog.Error("worker: claim and execute", "error", err)
			}
		}
	}
}

// tryClaimAndExecute lists submitted tasks, finds one matching capabilities,
// claims it, and executes it.
func (w *Worker) tryClaimAndExecute(ctx context.Context) error {
	tasks, err := w.node.Tasks.ListByStatus(ctx, TaskStatusSubmitted)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if !w.matchesCapabilities(task) {
			continue
		}
		if err := w.node.Tasks.Claim(ctx, task.ID, w.node.Config.AgentID); err != nil {
			// Another worker may have claimed it; skip.
			continue
		}
		w.execute(ctx, task)
		return nil
	}
	return nil
}

// matchesCapabilities returns true if the task can be handled by this worker.
// A task with no specialization matches any worker. Otherwise, any overlap
// between the task's specialization and the worker's capabilities is a match.
func (w *Worker) matchesCapabilities(task Task) bool {
	if len(task.Specialization) == 0 {
		return true
	}
	caps := make(map[string]bool, len(w.node.Config.Capabilities))
	for _, c := range w.node.Config.Capabilities {
		caps[c] = true
	}
	for _, s := range task.Specialization {
		if caps[s] {
			return true
		}
	}
	return false
}

// execute runs the handler for a claimed task and updates status accordingly.
func (w *Worker) execute(ctx context.Context, task Task) {
	w.busy = true
	defer func() { w.busy = false }()

	agentID := w.node.Config.AgentID

	if err := w.node.Tasks.SetWorking(ctx, task.ID); err != nil {
		slog.Error("worker: set working", "task", task.ID, "error", err)
		return
	}

	if err := w.node.Registry.UpdateStatus(ctx, agentID, AgentStatusWorking, task.ID); err != nil {
		slog.Error("worker: update status working", "task", task.ID, "error", err)
		return
	}

	result := w.handler(ctx, task)

	if result.Branch != "" || result.Summary != "" {
		if err := w.node.Tasks.Complete(ctx, task.ID, result); err != nil {
			slog.Error("worker: complete task", "task", task.ID, "error", err)
		}
	} else {
		if err := w.node.Tasks.Fail(ctx, task.ID, result); err != nil {
			slog.Error("worker: fail task", "task", task.ID, "error", err)
		}
	}

	if err := w.node.Registry.UpdateStatus(ctx, agentID, AgentStatusIdle, ""); err != nil {
		slog.Error("worker: update status idle", "task", task.ID, "error", err)
	}
}
