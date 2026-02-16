package cluster

import (
	"context"
	"fmt"
	"log/slog"
)

// PlannedTask is a task bundled with its dependency list.
type PlannedTask struct {
	Task      Task     `json:"task"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// TaskPlan is an ordered list of tasks with dependency edges.
type TaskPlan struct {
	Tasks []PlannedTask `json:"tasks"`
}

// Orchestrator manages a task plan with dependency tracking. It submits
// tasks to the node's TaskQueue as their dependencies are satisfied.
type Orchestrator struct {
	node          *Node
	plan          *TaskPlan
	submitted     map[string]bool // task IDs already submitted to the queue
	workingBranch string
}

// SetWorkingBranch records the branch that worker branches merge into.
func (o *Orchestrator) SetWorkingBranch(branch string) {
	o.workingBranch = branch
}

// WorkingBranch returns the configured working branch.
func (o *Orchestrator) WorkingBranch() string {
	return o.workingBranch
}

// NewOrchestrator creates an Orchestrator tied to the given cluster node.
func NewOrchestrator(node *Node) *Orchestrator {
	return &Orchestrator{
		node:      node,
		submitted: make(map[string]bool),
	}
}

// SubmitPlan stores the plan and immediately submits all tasks that have no
// dependencies. Each submitted task gets its CreatedBy set to the node's
// agent ID.
func (o *Orchestrator) SubmitPlan(ctx context.Context, plan TaskPlan) error {
	o.plan = &plan

	for _, pt := range plan.Tasks {
		if len(pt.DependsOn) == 0 {
			task := pt.Task
			task.DependsOn = pt.DependsOn
			if err := o.submitTask(ctx, task); err != nil {
				return fmt.Errorf("submit plan: %w", err)
			}
		}
	}
	return nil
}

// ResolveDependencies checks for plan tasks whose dependencies are all
// completed and that have not already been submitted. It submits them and
// returns the newly unblocked tasks. The method is idempotent: calling it
// multiple times without new completions produces no duplicates.
func (o *Orchestrator) ResolveDependencies(ctx context.Context) ([]Task, error) {
	if o.plan == nil {
		return nil, nil
	}

	completed, err := o.completedSet(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve dependencies: %w", err)
	}

	var unblocked []Task
	for _, pt := range o.plan.Tasks {
		if len(pt.DependsOn) == 0 {
			continue // already handled by SubmitPlan
		}
		if o.submitted[pt.Task.ID] {
			continue // already submitted
		}
		if !allIn(pt.DependsOn, completed) {
			continue // not all deps satisfied
		}

		task := pt.Task
		task.DependsOn = pt.DependsOn
		if err := o.submitTask(ctx, task); err != nil {
			return nil, fmt.Errorf("resolve dependencies: %w", err)
		}
		unblocked = append(unblocked, task)
	}
	return unblocked, nil
}

// PendingTasks returns plan tasks that have dependencies (i.e. tasks that
// were not immediately submitted by SubmitPlan).
func (o *Orchestrator) PendingTasks() []PlannedTask {
	if o.plan == nil {
		return nil
	}
	var pending []PlannedTask
	for _, pt := range o.plan.Tasks {
		if len(pt.DependsOn) > 0 {
			pending = append(pending, pt)
		}
	}
	return pending
}

// submitTask sets CreatedBy and submits the task to the node's queue.
func (o *Orchestrator) submitTask(ctx context.Context, task Task) error {
	task.CreatedBy = o.node.Config.AgentID
	if err := o.node.Tasks.Submit(ctx, task); err != nil {
		return err
	}
	o.submitted[task.ID] = true
	return nil
}

// completedSet builds a set of task IDs that are currently completed.
func (o *Orchestrator) completedSet(ctx context.Context) (map[string]bool, error) {
	tasks, err := o.node.Tasks.ListByStatus(ctx, TaskStatusCompleted)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		set[t.ID] = true
	}
	return set, nil
}

// MergeAndResolve merges a completed task's branch into the working branch,
// then resolves dependencies to unblock waiting tasks.
func (o *Orchestrator) MergeAndResolve(ctx context.Context, taskID string, mw *MergeWorktree, resolver ConflictResolver) error {
	task, err := o.node.Tasks.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("merge: get task %s: %w", taskID, err)
	}

	// Only merge completed tasks with a branch
	if task.Status != TaskStatusCompleted {
		return nil
	}
	if task.Result.Branch == "" {
		o.ResolveDependencies(ctx)
		return nil
	}
	if task.Result.MergeStatus != "" {
		// Already merged
		o.ResolveDependencies(ctx)
		return nil
	}

	// Merge the branch
	result, err := mw.Merge(ctx, task.Result.Branch, task.Title, resolver)
	if err != nil {
		slog.Error("merge failed, requeuing", "task", taskID, "error", err)
		// Fail first (Requeue requires assigned/working/failed status)
		o.node.Tasks.Fail(ctx, taskID, TaskResult{Summary: fmt.Sprintf("merge failed: %v", err)})
		o.node.Tasks.Requeue(ctx, taskID)
		return err
	}

	// Update task result with merge info
	task.Result.MergeStatus = result.MergeStatus
	task.Result.MergeCommit = result.MergeCommit
	o.node.Tasks.Complete(ctx, taskID, task.Result)

	// Clean up worker branch
	mw.DeleteBranch(ctx, task.Result.Branch)

	// Resolve dependencies
	o.ResolveDependencies(ctx)
	return nil
}

// allIn returns true if every element of ids is present in the set.
func allIn(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
}
