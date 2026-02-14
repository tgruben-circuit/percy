package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shelley.exe.dev/llm"
)

// TodoWriteTool manages a TODO.md checklist in the working directory.
type TodoWriteTool struct {
	WorkingDir *MutableWorkingDir
}

const (
	todoWriteName        = "todo_write"
	todoWriteDescription = `Manage a TODO.md checklist file in the working directory.

Supports three operations:
- add: Append a new task to TODO.md (creates the file if it doesn't exist)
- update_status: Update a task's status by its 1-based index among checklist items
- list: Display the current contents of TODO.md

Tasks use markdown checklist format:
- [ ] pending
- [~] in_progress
- [x] done
`
	todoWriteInputSchema = `{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["add", "update_status", "list"],
      "description": "The operation to perform"
    },
    "task": {
      "type": "string",
      "description": "The task description (for add operation)"
    },
    "task_id": {
      "type": "integer",
      "description": "1-based index of the task among checklist items (for update_status operation)"
    },
    "status": {
      "type": "string",
      "enum": ["pending", "in_progress", "done"],
      "description": "The new status for the task (for update_status operation)"
    }
  }
}`
)

type todoWriteInput struct {
	Operation string `json:"operation"`
	Task      string `json:"task"`
	TaskID    int    `json:"task_id"`
	Status    string `json:"status"`
}

// Tool returns an llm.Tool for managing TODO.md.
func (t *TodoWriteTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        todoWriteName,
		Description: todoWriteDescription,
		InputSchema: llm.MustSchema(todoWriteInputSchema),
		Run:         t.Run,
	}
}

// Run executes the todo_write tool.
func (t *TodoWriteTool) Run(ctx context.Context, m json.RawMessage) llm.ToolOut {
	var req todoWriteInput
	if err := json.Unmarshal(m, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse todo_write input: %w", err)
	}

	todoPath := filepath.Join(t.WorkingDir.Get(), "TODO.md")

	switch req.Operation {
	case "add":
		return t.add(todoPath, req.Task)
	case "update_status":
		return t.updateStatus(todoPath, req.TaskID, req.Status)
	case "list":
		return t.list(todoPath)
	default:
		return llm.ErrorfToolOut("unknown operation: %s", req.Operation)
	}
}

func (t *TodoWriteTool) add(todoPath, task string) llm.ToolOut {
	if task == "" {
		return llm.ErrorfToolOut("task is required for add operation")
	}

	// Read existing content (may not exist yet).
	content, _ := os.ReadFile(todoPath)

	// Ensure a trailing newline before appending.
	s := string(content)
	if len(s) > 0 && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += fmt.Sprintf("- [ ] %s\n", task)

	if err := os.WriteFile(todoPath, []byte(s), 0o644); err != nil {
		return llm.ErrorfToolOut("failed to write TODO.md: %w", err)
	}

	return llm.ToolOut{
		LLMContent: llm.TextContent(fmt.Sprintf("Added task: %s", task)),
	}
}

func (t *TodoWriteTool) updateStatus(todoPath string, taskID int, status string) llm.ToolOut {
	if taskID < 1 {
		return llm.ErrorfToolOut("task_id must be >= 1")
	}
	if status == "" {
		return llm.ErrorfToolOut("status is required for update_status operation")
	}

	var marker string
	switch status {
	case "pending":
		marker = "[ ]"
	case "in_progress":
		marker = "[~]"
	case "done":
		marker = "[x]"
	default:
		return llm.ErrorfToolOut("unknown status: %s (must be pending, in_progress, or done)", status)
	}

	data, err := os.ReadFile(todoPath)
	if err != nil {
		return llm.ErrorfToolOut("failed to read TODO.md: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	checklistIndex := 0
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ] ") || strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "- [~] ") {
			checklistIndex++
			if checklistIndex == taskID {
				// Replace the checkbox marker, preserving leading whitespace.
				prefix := line[:len(line)-len(trimmed)]
				// Extract task text after "- [.] "
				taskText := trimmed[6:]
				lines[i] = fmt.Sprintf("%s- %s %s", prefix, marker, taskText)
				found = true
				break
			}
		}
	}

	if !found {
		return llm.ErrorfToolOut("task_id %d not found (found %d checklist items)", taskID, checklistIndex)
	}

	if err := os.WriteFile(todoPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return llm.ErrorfToolOut("failed to write TODO.md: %w", err)
	}

	return llm.ToolOut{
		LLMContent: llm.TextContent(fmt.Sprintf("Updated task %d to %s", taskID, status)),
	}
}

func (t *TodoWriteTool) list(todoPath string) llm.ToolOut {
	data, err := os.ReadFile(todoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return llm.ToolOut{
				LLMContent: llm.TextContent("TODO.md does not exist yet. Use the add operation to create it."),
			}
		}
		return llm.ErrorfToolOut("failed to read TODO.md: %w", err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return llm.ToolOut{
			LLMContent: llm.TextContent("TODO.md is empty."),
		}
	}

	return llm.ToolOut{
		LLMContent: llm.TextContent(content),
	}
}
