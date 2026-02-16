package cluster

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestE2EWorkerMonitorFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Start orchestrator node (embedded NATS).
	orchNode, err := StartNode(ctx, NodeConfig{
		AgentID:    "orchestrator",
		AgentName:  "orchestrator",
		ListenAddr: ":0",
		StoreDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer orchNode.Stop()

	// 2. Start worker node connecting to orchestrator.
	workerNode, err := StartNode(ctx, NodeConfig{
		AgentID:      "worker-1",
		AgentName:    "backend",
		Capabilities: []string{"go", "sql"},
		NATSUrl:      orchNode.ClientURL(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer workerNode.Stop()

	// 3. Track executed tasks via mock handler.
	var executed []string
	var mu sync.Mutex
	handler := func(ctx context.Context, task Task) TaskResult {
		mu.Lock()
		executed = append(executed, task.ID)
		mu.Unlock()
		return TaskResult{Branch: "agent/worker-1/" + task.ID, Summary: "completed " + task.Title}
	}

	// 4. Start worker.
	worker := NewWorker(workerNode, handler)
	go worker.Run(ctx)

	// 5. Create orchestrator and submit plan with dependencies.
	orch := NewOrchestrator(orchNode)
	plan := TaskPlan{
		Tasks: []PlannedTask{
			{Task: Task{ID: "t1", Title: "Add library", Type: TaskTypeImplement, Specialization: []string{"go"}}},
			{Task: Task{ID: "t2", Title: "Write tests", Type: TaskTypeTest, Specialization: []string{"go"}}, DependsOn: []string{"t1"}},
		},
	}
	if err := orch.SubmitPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}

	// 6. Start monitor for dependency resolution.
	mon := NewMonitor(orchNode, orch, nil, nil)
	go mon.Run(ctx)

	// 7. Wait for both tasks to complete (monitor should resolve t2 after t1 completes).
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("timed out. executed: %v", executed)
			mu.Unlock()
		default:
		}

		t2, err := orchNode.Tasks.Get(ctx, "t2")
		if err == nil && t2.Status == TaskStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 8. Verify both tasks completed.
	mu.Lock()
	if len(executed) != 2 {
		t.Fatalf("expected 2 tasks executed, got %d: %v", len(executed), executed)
	}
	mu.Unlock()

	t1, _ := orchNode.Tasks.Get(ctx, "t1")
	if t1.Status != TaskStatusCompleted {
		t.Fatalf("t1 status: %s", t1.Status)
	}
	if t1.Result.Branch != "agent/worker-1/t1" {
		t.Fatalf("t1 branch: %s", t1.Result.Branch)
	}

	t2, _ := orchNode.Tasks.Get(ctx, "t2")
	if t2.Status != TaskStatusCompleted {
		t.Fatalf("t2 status: %s", t2.Status)
	}
}
