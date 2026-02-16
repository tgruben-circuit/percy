package cluster

import (
	"context"
	"log/slog"
	"testing"
)

// setupTestOrchestrator creates a Node with embedded NATS and returns an
// Orchestrator ready for testing.
func setupTestOrchestrator(t *testing.T) (*Orchestrator, *Node, context.Context) {
	t.Helper()
	ctx := context.Background()

	node, err := StartNode(ctx, NodeConfig{
		AgentID:      "orch-agent",
		AgentName:    "Orchestrator Agent",
		Capabilities: []string{"orchestrate"},
		ListenAddr:   ":0",
		StoreDir:     t.TempDir(),
		Logger:       slog.Default(),
	})
	if err != nil {
		t.Fatalf("StartNode: %v", err)
	}
	t.Cleanup(node.Stop)

	orch := NewOrchestrator(node)
	return orch, node, ctx
}

func TestSubmitPlanOnlySubmitsNoDeps(t *testing.T) {
	orch, node, ctx := setupTestOrchestrator(t)

	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:    "a",
					Type:  TaskTypeImplement,
					Title: "Task A (no deps)",
					Context: TaskContext{
						Repo:       "percy",
						BaseBranch: "main",
					},
				},
				// No dependencies -- should be submitted immediately.
			},
			{
				Task: Task{
					ID:    "b",
					Type:  TaskTypeTest,
					Title: "Task B (depends on A)",
					Context: TaskContext{
						Repo:       "percy",
						BaseBranch: "main",
					},
				},
				DependsOn: []string{"a"},
			},
			{
				Task: Task{
					ID:    "c",
					Type:  TaskTypeReview,
					Title: "Task C (no deps)",
					Context: TaskContext{
						Repo:       "percy",
						BaseBranch: "main",
					},
				},
				// No dependencies -- should be submitted immediately.
			},
		},
	}

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	// Tasks A and C should be submitted (no deps).
	submitted, err := node.Tasks.ListByStatus(ctx, TaskStatusSubmitted)
	if err != nil {
		t.Fatalf("ListByStatus(submitted): %v", err)
	}
	if len(submitted) != 2 {
		t.Fatalf("expected 2 submitted tasks, got %d", len(submitted))
	}

	ids := map[string]bool{}
	for _, task := range submitted {
		ids[task.ID] = true
	}
	if !ids["a"] {
		t.Error("task 'a' should be submitted")
	}
	if !ids["c"] {
		t.Error("task 'c' should be submitted")
	}

	// Task B should NOT be submitted yet (it depends on A).
	_, err = node.Tasks.Get(ctx, "b")
	if err == nil {
		t.Error("task 'b' should not exist in the queue yet")
	}

	// CreatedBy should be set to the node's agent ID.
	taskA, err := node.Tasks.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	if taskA.CreatedBy != "orch-agent" {
		t.Errorf("CreatedBy: got %q, want %q", taskA.CreatedBy, "orch-agent")
	}
}

func TestResolveDependenciesUnblocks(t *testing.T) {
	orch, node, ctx := setupTestOrchestrator(t)

	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:      "a",
					Type:    TaskTypeImplement,
					Title:   "Task A",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
			},
			{
				Task: Task{
					ID:      "b",
					Type:    TaskTypeTest,
					Title:   "Task B (depends on A)",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
				DependsOn: []string{"a"},
			},
		},
	}

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	// Before completing A, ResolveDependencies should return nothing new.
	unblocked, err := orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (before complete): %v", err)
	}
	if len(unblocked) != 0 {
		t.Fatalf("expected 0 unblocked tasks, got %d", len(unblocked))
	}

	// Complete task A.
	if err := node.Tasks.Claim(ctx, "a", "worker-1"); err != nil {
		t.Fatalf("Claim(a): %v", err)
	}
	if err := node.Tasks.Complete(ctx, "a", TaskResult{Summary: "done"}); err != nil {
		t.Fatalf("Complete(a): %v", err)
	}

	// Now ResolveDependencies should unblock B.
	unblocked, err = orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (after complete): %v", err)
	}
	if len(unblocked) != 1 {
		t.Fatalf("expected 1 unblocked task, got %d", len(unblocked))
	}
	if unblocked[0].ID != "b" {
		t.Errorf("unblocked task ID: got %q, want %q", unblocked[0].ID, "b")
	}

	// B should now be in the task queue.
	taskB, err := node.Tasks.Get(ctx, "b")
	if err != nil {
		t.Fatalf("Get(b): %v", err)
	}
	if taskB.Status != TaskStatusSubmitted {
		t.Errorf("task B status: got %q, want %q", taskB.Status, TaskStatusSubmitted)
	}
	if taskB.CreatedBy != "orch-agent" {
		t.Errorf("task B CreatedBy: got %q, want %q", taskB.CreatedBy, "orch-agent")
	}
}

func TestMultipleDependenciesAllMustComplete(t *testing.T) {
	orch, node, ctx := setupTestOrchestrator(t)

	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:      "a",
					Type:    TaskTypeImplement,
					Title:   "Task A",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
			},
			{
				Task: Task{
					ID:      "b",
					Type:    TaskTypeImplement,
					Title:   "Task B",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
			},
			{
				Task: Task{
					ID:      "c",
					Type:    TaskTypeTest,
					Title:   "Task C (depends on A and B)",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
				DependsOn: []string{"a", "b"},
			},
		},
	}

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	// Complete only A -- C should NOT unblock yet.
	if err := node.Tasks.Claim(ctx, "a", "worker-1"); err != nil {
		t.Fatalf("Claim(a): %v", err)
	}
	if err := node.Tasks.Complete(ctx, "a", TaskResult{Summary: "done"}); err != nil {
		t.Fatalf("Complete(a): %v", err)
	}

	unblocked, err := orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (only A complete): %v", err)
	}
	if len(unblocked) != 0 {
		t.Fatalf("expected 0 unblocked tasks when only A is complete, got %d", len(unblocked))
	}

	// Now complete B -- C should unblock.
	if err := node.Tasks.Claim(ctx, "b", "worker-2"); err != nil {
		t.Fatalf("Claim(b): %v", err)
	}
	if err := node.Tasks.Complete(ctx, "b", TaskResult{Summary: "done"}); err != nil {
		t.Fatalf("Complete(b): %v", err)
	}

	unblocked, err = orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (A and B complete): %v", err)
	}
	if len(unblocked) != 1 {
		t.Fatalf("expected 1 unblocked task, got %d", len(unblocked))
	}
	if unblocked[0].ID != "c" {
		t.Errorf("unblocked task ID: got %q, want %q", unblocked[0].ID, "c")
	}
}

func TestResolveDependenciesIdempotent(t *testing.T) {
	orch, node, ctx := setupTestOrchestrator(t)

	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:      "a",
					Type:    TaskTypeImplement,
					Title:   "Task A",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
			},
			{
				Task: Task{
					ID:      "b",
					Type:    TaskTypeTest,
					Title:   "Task B (depends on A)",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
				DependsOn: []string{"a"},
			},
		},
	}

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	// Complete A.
	if err := node.Tasks.Claim(ctx, "a", "worker-1"); err != nil {
		t.Fatalf("Claim(a): %v", err)
	}
	if err := node.Tasks.Complete(ctx, "a", TaskResult{Summary: "done"}); err != nil {
		t.Fatalf("Complete(a): %v", err)
	}

	// First call should unblock B.
	unblocked1, err := orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (first call): %v", err)
	}
	if len(unblocked1) != 1 {
		t.Fatalf("first call: expected 1 unblocked task, got %d", len(unblocked1))
	}

	// Second call should return nothing new (idempotent).
	unblocked2, err := orch.ResolveDependencies(ctx)
	if err != nil {
		t.Fatalf("ResolveDependencies (second call): %v", err)
	}
	if len(unblocked2) != 0 {
		t.Fatalf("second call: expected 0 unblocked tasks, got %d", len(unblocked2))
	}

	// Verify B appears exactly once in the task queue.
	submitted, err := node.Tasks.ListByStatus(ctx, TaskStatusSubmitted)
	if err != nil {
		t.Fatalf("ListByStatus(submitted): %v", err)
	}
	count := 0
	for _, task := range submitted {
		if task.ID == "b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("task 'b' submitted %d times, want exactly 1", count)
	}
}

func TestPendingTasks(t *testing.T) {
	orch, _, ctx := setupTestOrchestrator(t)

	plan := TaskPlan{
		Tasks: []PlannedTask{
			{
				Task: Task{
					ID:      "a",
					Type:    TaskTypeImplement,
					Title:   "Task A",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
			},
			{
				Task: Task{
					ID:      "b",
					Type:    TaskTypeTest,
					Title:   "Task B (depends on A)",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
				DependsOn: []string{"a"},
			},
			{
				Task: Task{
					ID:      "c",
					Type:    TaskTypeReview,
					Title:   "Task C (depends on A and B)",
					Context: TaskContext{Repo: "percy", BaseBranch: "main"},
				},
				DependsOn: []string{"a", "b"},
			},
		},
	}

	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	pending := orch.PendingTasks()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", len(pending))
	}

	ids := map[string]bool{}
	for _, pt := range pending {
		ids[pt.Task.ID] = true
	}
	if !ids["b"] {
		t.Error("expected task 'b' in pending")
	}
	if !ids["c"] {
		t.Error("expected task 'c' in pending")
	}
}
