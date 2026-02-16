package cluster

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestWorkerClaimsTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node, err := StartNode(ctx, NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "Worker 1",
		Capabilities: []string{"go", "review"},
		ListenAddr:   ":0",
		StoreDir:     t.TempDir(),
		Logger:       slog.Default(),
	})
	if err != nil {
		t.Fatalf("StartNode: %v", err)
	}
	defer node.Stop()

	handler := func(ctx context.Context, task Task) TaskResult {
		return TaskResult{
			Branch:  "feature/task-1",
			Summary: "implemented",
		}
	}

	w := NewWorker(node, handler)
	go w.Run(ctx)

	// Submit a task that matches the worker's capabilities.
	task := Task{
		ID:             "task-1",
		Type:           TaskTypeImplement,
		Specialization: []string{"go"},
		Priority:       1,
		CreatedBy:      "orchestrator",
		Title:          "Implement feature",
		Context:        TaskContext{Repo: "percy", BaseBranch: "main"},
	}
	if err := node.Tasks.Submit(ctx, task); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Poll until the task is completed (max 5s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := node.Tasks.Get(ctx, "task-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status == TaskStatusCompleted {
			if got.Result.Branch != "feature/task-1" {
				t.Errorf("Result.Branch: got %q, want %q", got.Result.Branch, "feature/task-1")
			}
			if got.Result.Summary != "implemented" {
				t.Errorf("Result.Summary: got %q, want %q", got.Result.Summary, "implemented")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task was not completed within 5s")
}

func TestWorkerSkipsNonMatchingTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node, err := StartNode(ctx, NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "Worker 1",
		Capabilities: []string{"ts", "react"},
		ListenAddr:   ":0",
		StoreDir:     t.TempDir(),
		Logger:       slog.Default(),
	})
	if err != nil {
		t.Fatalf("StartNode: %v", err)
	}
	defer node.Stop()

	handler := func(ctx context.Context, task Task) TaskResult {
		return TaskResult{Branch: "feature/done", Summary: "done"}
	}

	w := NewWorker(node, handler)
	go w.Run(ctx)

	// Submit a "go" task -- worker only has ["ts","react"].
	task := Task{
		ID:             "task-go",
		Type:           TaskTypeImplement,
		Specialization: []string{"go"},
		Priority:       1,
		CreatedBy:      "orchestrator",
		Title:          "Go task",
		Context:        TaskContext{Repo: "percy", BaseBranch: "main"},
	}
	if err := node.Tasks.Submit(ctx, task); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Sleep 1s -- proving a negative (task should stay submitted).
	time.Sleep(1 * time.Second)

	got, err := node.Tasks.Get(ctx, "task-go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != TaskStatusSubmitted {
		t.Errorf("Status: got %q, want %q", got.Status, TaskStatusSubmitted)
	}
}
