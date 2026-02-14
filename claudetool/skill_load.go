package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"shelley.exe.dev/llm"
	"shelley.exe.dev/skills"
)

// SkillLoadTool loads the full content of a skill's SKILL.md file by name.
type SkillLoadTool struct {
	skills []skills.Skill
}

const (
	skillLoadName        = "skill_load"
	skillLoadDescription = "Load a skill's full content by name. Returns the complete SKILL.md content for the requested skill."
	skillLoadInputSchema = `{
  "type": "object",
  "required": ["name"],
  "properties": {
    "name": {
      "type": "string",
      "description": "The name of the skill to load"
    }
  }
}`
)

type skillLoadInput struct {
	Name string `json:"name"`
}

// Tool returns an llm.Tool for loading skill content.
func (s *SkillLoadTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        skillLoadName,
		Description: skillLoadDescription,
		InputSchema: llm.MustSchema(skillLoadInputSchema),
		Run:         s.Run,
	}
}

// Run executes the skill_load tool.
func (s *SkillLoadTool) Run(ctx context.Context, m json.RawMessage) llm.ToolOut {
	var req skillLoadInput
	if err := json.Unmarshal(m, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse skill_load input: %w", err)
	}

	if req.Name == "" {
		return llm.ErrorfToolOut("name is required")
	}

	for _, skill := range s.skills {
		if skill.Name == req.Name {
			content, err := os.ReadFile(skill.Path)
			if err != nil {
				return llm.ErrorfToolOut("failed to read skill file: %v", err)
			}
			return llm.ToolOut{
				LLMContent: llm.TextContent(string(content)),
			}
		}
	}

	// Skill not found — list available names.
	var names []string
	for _, skill := range s.skills {
		names = append(names, skill.Name)
	}
	return llm.ErrorfToolOut("skill %q not found. Available skills: %s", req.Name, fmt.Sprintf("[%s]", strings.Join(names, ", ")))
}
