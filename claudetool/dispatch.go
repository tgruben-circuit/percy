package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tgruben-circuit/percy/cluster"
	"github.com/tgruben-circuit/percy/llm"
)

// DispatchTool lets the orchestrator's LLM break work into subtasks and send
// them to worker agents via the cluster task queue.
type DispatchTool struct {
	node *cluster.Node
}

// NewDispatchTool creates a DispatchTool backed by the given cluster node.
func NewDispatchTool(node *cluster.Node) *DispatchTool {
	return &DispatchTool{node: node}
}

type dispatchInput struct {
	Tasks []dispatchTaskInput `json:"tasks"`
}

type dispatchTaskInput struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Specialization []string `json:"specialization,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
}

const (
	dispatchName        = "dispatch_tasks"
	dispatchDescription = `Dispatch subtasks to worker agents in the cluster. Break down complex work into independent or dependent tasks that workers will execute in parallel.

Each task needs a unique id, title, and description. Use specialization to hint at required capabilities (e.g. ["go","testing"]). Use depends_on to list task IDs that must complete first.`

	dispatchInputSchema = `{
  "type": "object",
  "required": ["tasks"],
  "properties": {
    "tasks": {
      "type": "array",
      "description": "The list of tasks to dispatch to workers",
      "items": {
        "type": "object",
        "required": ["id", "title", "description"],
        "properties": {
          "id": {
            "type": "string",
            "description": "Unique identifier for this task"
          },
          "title": {
            "type": "string",
            "description": "Short title describing the task"
          },
          "description": {
            "type": "string",
            "description": "Detailed description of what the worker should do"
          },
          "specialization": {
            "type": "array",
            "items": {"type": "string"},
            "description": "Capability hints for worker selection (e.g. go, typescript, testing)"
          },
          "depends_on": {
            "type": "array",
            "items": {"type": "string"},
            "description": "IDs of tasks that must complete before this one starts"
          }
        }
      }
    }
  }
}`
)

// Tool returns the llm.Tool definition for dispatch_tasks.
func (d *DispatchTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        dispatchName,
		Type:        "custom",
		Description: dispatchDescription,
		InputSchema: llm.MustSchema(dispatchInputSchema),
		Run:         d.Run,
	}
}

// Run executes the dispatch_tasks tool.
func (d *DispatchTool) Run(ctx context.Context, input json.RawMessage) llm.ToolOut {
	var req dispatchInput
	if err := json.Unmarshal(input, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse dispatch_tasks input: %w", err)
	}
	if len(req.Tasks) == 0 {
		return llm.ErrorfToolOut("tasks array is empty")
	}

	// Check that workers are connected (agents other than self).
	agents, err := d.node.Registry.List(ctx)
	if err != nil {
		return llm.ErrorfToolOut("list agents: %w", err)
	}
	workers := 0
	for _, a := range agents {
		if a.ID != d.node.Config.AgentID {
			workers++
		}
	}
	if workers == 0 {
		return llm.ToolOut{
			LLMContent: llm.TextContent("No workers connected. Start worker instances with `percy worker` before dispatching tasks."),
		}
	}

	// Build TaskPlan from input.
	plan := cluster.TaskPlan{
		Tasks: make([]cluster.PlannedTask, len(req.Tasks)),
	}
	for i, t := range req.Tasks {
		plan.Tasks[i] = cluster.PlannedTask{
			Task: cluster.Task{
				ID:             t.ID,
				Type:           cluster.TaskTypeImplement,
				Title:          t.Title,
				Description:    t.Description,
				Specialization: t.Specialization,
			},
			DependsOn: t.DependsOn,
		}
	}

	// Submit plan via orchestrator.
	orch := cluster.NewOrchestrator(d.node)
	if err := orch.SubmitPlan(ctx, plan); err != nil {
		return llm.ErrorfToolOut("submit plan: %w", err)
	}

	// Build summary.
	var sb strings.Builder
	fmt.Fprintf(&sb, "Dispatched %d task(s) to %d worker(s):\n", len(req.Tasks), workers)
	for _, t := range req.Tasks {
		fmt.Fprintf(&sb, "  - %s: %s", t.ID, t.Title)
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&sb, " (depends on: %s)", strings.Join(t.DependsOn, ", "))
		}
		sb.WriteString("\n")
	}

	pending := orch.PendingTasks()
	if len(pending) > 0 {
		fmt.Fprintf(&sb, "\n%d task(s) waiting on dependencies.", len(pending))
	}

	return llm.ToolOut{
		LLMContent: llm.TextContent(sb.String()),
	}
}
