# Co-Command Skills Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add model override to the subagent tool and create three bundled co-command skills (co-brainstorm, co-plan, co-validate) that use a different-model subagent for independent second opinions.

**Architecture:** Extend the existing `SubagentRunner` interface with an optional model parameter. The subagent tool passes this through to `server/subagent.go`, which uses it to resolve a different LLM service. Three new SKILL.md files in `bundled_skills/` instruct the agent to use `gpt-5.3-codex` when spawning co-command subagents.

**Tech Stack:** Go (backend changes), Markdown (skills)

---

### Task 1: Add model parameter to SubagentRunner interface and subagent tool

**Files:**
- Modify: `claudetool/subagent.go:15-19` (interface), `claudetool/subagent.go:54-77` (schema), `claudetool/subagent.go:80-85` (struct), `claudetool/subagent.go:97-154` (Run method)
- Modify: `claudetool/subagent_test.go:37-42` (mock runner)

**Step 1: Write the failing test**

Add a test to `claudetool/subagent_test.go` that passes a model in the subagent input and verifies it reaches the runner:

```go
// mockSubagentRunnerWithModel tracks the model parameter.
type mockSubagentRunnerWithModel struct {
	response string
	err      error
	lastModel string
}

func (m *mockSubagentRunnerWithModel) RunSubagent(ctx context.Context, conversationID, prompt string, wait bool, timeout time.Duration, model string) (string, error) {
	m.lastModel = model
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestSubagentTool_ModelOverride(t *testing.T) {
	wd := NewMutableWorkingDir("/tmp")
	db := newMockSubagentDB()
	runner := &mockSubagentRunnerWithModel{response: "Done"}

	tool := &SubagentTool{
		DB:                   db,
		ParentConversationID: "parent-123",
		WorkingDir:           wd,
		Runner:               runner,
	}

	input := subagentInput{
		Slug:   "co-brainstorm",
		Prompt: "Brainstorm ideas",
		Model:  "gpt-5.3-codex",
	}
	inputJSON, _ := json.Marshal(input)

	result := tool.Run(context.Background(), inputJSON)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if runner.lastModel != "gpt-5.3-codex" {
		t.Errorf("expected model 'gpt-5.3-codex', got %q", runner.lastModel)
	}
}

func TestSubagentTool_ModelDefaultEmpty(t *testing.T) {
	wd := NewMutableWorkingDir("/tmp")
	db := newMockSubagentDB()
	runner := &mockSubagentRunnerWithModel{response: "Done"}

	tool := &SubagentTool{
		DB:                   db,
		ParentConversationID: "parent-123",
		WorkingDir:           wd,
		Runner:               runner,
	}

	input := subagentInput{
		Slug:   "task",
		Prompt: "Do something",
	}
	inputJSON, _ := json.Marshal(input)

	result := tool.Run(context.Background(), inputJSON)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if runner.lastModel != "" {
		t.Errorf("expected empty model, got %q", runner.lastModel)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./claudetool -run TestSubagentTool_Model -v`
Expected: Compilation errors — `RunSubagent` signature mismatch, `Model` field doesn't exist on `subagentInput`.

**Step 3: Implement the changes**

In `claudetool/subagent.go`:

1. Update the `SubagentRunner` interface:
```go
type SubagentRunner interface {
	RunSubagent(ctx context.Context, conversationID, prompt string, wait bool, timeout time.Duration, model string) (string, error)
}
```

2. Add `model` to `subagentInputSchema`:
```json
"model": {
  "type": "string",
  "description": "Optional model ID to use for this subagent (e.g., 'gpt-5.3-codex'). If not provided, uses the parent conversation's model."
}
```

3. Add `Model` field to `subagentInput`:
```go
type subagentInput struct {
	Slug           string `json:"slug"`
	Prompt         string `json:"prompt"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Wait           *bool  `json:"wait,omitempty"`
	Model          string `json:"model,omitempty"`
}
```

4. Pass `req.Model` through in `Run()`:
```go
response, err := s.Runner.RunSubagent(ctx, conversationID, req.Prompt, wait, timeout, req.Model)
```

5. Update the existing `mockSubagentRunner` in the test file to match the new interface:
```go
func (m *mockSubagentRunner) RunSubagent(ctx context.Context, conversationID, prompt string, wait bool, timeout time.Duration, model string) (string, error) {
```

**Step 4: Run test to verify it passes**

Run: `go test ./claudetool -run TestSubagentTool -v`
Expected: All subagent tests PASS (new and existing).

**Step 5: Commit**

```bash
git add claudetool/subagent.go claudetool/subagent_test.go
git commit -m "feat: add model parameter to subagent tool"
```

---

### Task 2: Wire model override through server SubagentRunner

**Files:**
- Modify: `server/subagent.go:27-84` (RunSubagent implementation)
- Modify: `server/subagent_test.go` (if exists, update mock calls)

**Step 1: Write the failing test**

The server test likely already calls `RunSubagent` with the old signature. First check if `server/subagent_test.go` has tests that need updating. If so, add a test that passes a model override:

```go
// In server/subagent_test.go (or add to existing tests)
// Test that when model is provided, it's used instead of defaultModel.
// This is primarily a compilation check + integration test.
```

If no direct unit test exists for `RunSubagent` in server (it's tested via integration tests in `test/subagent_stream_test.go`), proceed to implementation.

**Step 2: Run existing tests to verify current state**

Run: `go test ./server -run Subagent -v`
Expected: Compilation failure — `RunSubagent` signature changed in Task 1.

**Step 3: Implement the changes**

In `server/subagent.go`, update `RunSubagent`:

```go
func (r *SubagentRunner) RunSubagent(ctx context.Context, conversationID, prompt string, wait bool, timeout time.Duration, model string) (string, error) {
	s := r.server

	go r.notifySubagentConversation(ctx, conversationID)

	manager, err := s.getOrCreateSubagentConversationManager(ctx, conversationID)
	if err != nil {
		return "", fmt.Errorf("failed to get conversation manager: %w", err)
	}

	// Use override model if provided, otherwise use server default
	modelID := model
	if modelID == "" {
		modelID = s.defaultModel
	}
	if modelID == "" && s.predictableOnly {
		modelID = "predictable"
	}

	llmService, err := s.llmManager.GetService(modelID)
	if err != nil {
		return "", fmt.Errorf("failed to get LLM service for model %q: %w", modelID, err)
	}

	// ... rest unchanged
```

Also check `test/subagent_stream_test.go` — if it has a mock `SubagentRunner`, update its signature too.

**Step 4: Run tests to verify everything compiles and passes**

Run: `go test ./server ./claudetool ./test -v -count=1 2>&1 | head -100`
Expected: All tests PASS. (Some integration tests may be skipped without a running server, that's fine.)

**Step 5: Commit**

```bash
git add server/subagent.go server/subagent_test.go test/subagent_stream_test.go
git commit -m "feat: wire model override through server SubagentRunner"
```

---

### Task 3: Create co-brainstorm bundled skill

**Files:**
- Create: `bundled_skills/co-brainstorm/SKILL.md`

**Step 1: Write the skill file**

```markdown
---
name: co-brainstorm
description: Use when brainstorming ideas and you want a second independent perspective from a different model
---

# Co-Brainstorm

Get independent ideas from a different model, then compare against your own to surface missed perspectives.

## Process

1. **Spawn background subagent** with a different model:
   ```
   subagent tool call:
     slug: "co-brainstorm"
     model: "gpt-5.3-codex"
     wait: false
     timeout_seconds: 300
     prompt: |
       You are an independent brainstorming partner. Your job is to brainstorm
       on the topic below. Think creatively about approaches, trade-offs, edge
       cases, and alternatives. Do NOT share your ideas until asked.

       Topic: [INSERT TOPIC HERE]

       Think through this thoroughly. When you're done thinking, say exactly:
       "My brainstorming is complete and I'm ready to present."
   ```

2. **While the subagent thinks, brainstorm independently.** Think through:
   - Different approaches and architectures
   - Trade-offs and constraints
   - Edge cases and failure modes
   - Creative alternatives
   - Simpler solutions that might be overlooked

3. **Retrieve the subagent's ideas** by sending a follow-up message to the same slug:
   ```
   subagent tool call:
     slug: "co-brainstorm"
     wait: true
     timeout_seconds: 300
     prompt: "Please share all your brainstorming ideas now."
   ```

4. **Compare and synthesize:**
   - What did the subagent think of that you missed?
   - What did you think of that the subagent missed?
   - Are there simpler alternatives either of you overlooked?
   - Present the combined best ideas to the user

## Error Handling

If the subagent model is not available (e.g., no OpenAI API key configured), tell the user:
"The co-brainstorm skill requires the gpt-5.3-codex model. Please configure your OpenAI API key to use this skill."

Do NOT fall back to the same model — the value comes from independent perspectives.
```

**Step 2: Verify skill parses correctly**

Run: `go test ./bundled_skills -v`
If no test exists for parsing, run the build: `go build ./...`
Expected: Build succeeds (skill is embedded at compile time).

**Step 3: Commit**

```bash
git add bundled_skills/co-brainstorm/SKILL.md
git commit -m "feat: add co-brainstorm bundled skill"
```

---

### Task 4: Create co-plan bundled skill

**Files:**
- Create: `bundled_skills/co-plan/SKILL.md`

**Step 1: Write the skill file**

```markdown
---
name: co-plan
description: Use when creating implementation plans and you want a parallel independent plan from a different model to compare
---

# Co-Plan

Generate an independent implementation plan from a different model, then compare against your own to catch missed approaches.

## Process

1. **Spawn background subagent** with a different model:
   ```
   subagent tool call:
     slug: "co-plan"
     model: "gpt-5.3-codex"
     wait: false
     timeout_seconds: 300
     prompt: |
       You are an independent planning partner. Create a detailed implementation
       plan for the task below. Consider architecture, components, testing strategy,
       edge cases, and risks. Do NOT share your plan until asked.

       Task: [INSERT TASK DESCRIPTION HERE]

       Context: [INSERT RELEVANT CONTEXT — codebase info, constraints, etc.]

       Think through this thoroughly. When your plan is complete, say exactly:
       "My plan is complete and I'm ready to present."
   ```

2. **While the subagent plans, create your own plan independently.** Cover:
   - Architecture and component design
   - Implementation steps
   - Testing strategy
   - Edge cases and error handling
   - Risks and mitigations

3. **Retrieve the subagent's plan** by sending a follow-up:
   ```
   subagent tool call:
     slug: "co-plan"
     wait: true
     timeout_seconds: 300
     prompt: "Please share your complete implementation plan now."
   ```

4. **Compare plans and synthesize:**
   - What approaches did the subagent consider that you didn't?
   - Are there simpler alternatives in either plan?
   - What risks did either plan miss?
   - Present the best combined plan to the user

## Error Handling

If the subagent model is not available, tell the user:
"The co-plan skill requires the gpt-5.3-codex model. Please configure your OpenAI API key to use this skill."

Do NOT fall back to the same model.
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: Build succeeds.

**Step 3: Commit**

```bash
git add bundled_skills/co-plan/SKILL.md
git commit -m "feat: add co-plan bundled skill"
```

---

### Task 5: Create co-validate bundled skill

**Files:**
- Create: `bundled_skills/co-validate/SKILL.md`

**Step 1: Write the skill file**

```markdown
---
name: co-validate
description: Use when you have a plan or design document that needs critical review from a different model acting as a staff engineer
---

# Co-Validate

Get a staff engineer review of a plan from a different model, compare against your own review, then reconcile.

## Process

1. **Read the plan file** that the user wants validated.

2. **Spawn background subagent** with a different model:
   ```
   subagent tool call:
     slug: "co-validate"
     model: "gpt-5.3-codex"
     wait: false
     timeout_seconds: 300
     prompt: |
       You are a staff engineer reviewing a plan. Be critical and thorough.
       Look for:
       - Missing edge cases or error handling
       - Over-engineering or unnecessary complexity
       - Security or performance concerns
       - Simpler alternatives to proposed approaches
       - Missing testing strategy
       - Unclear or ambiguous requirements

       Do NOT share your review until asked.

       Plan to review:
       [INSERT FULL PLAN CONTENT HERE]
   ```

3. **While the subagent reviews, conduct your own independent review.** Look for:
   - Critical issues that would block implementation
   - Simplification opportunities
   - Missing edge cases or failure modes
   - Alternative approaches worth considering
   - Gaps in testing strategy

4. **Retrieve the subagent's review**:
   ```
   subagent tool call:
     slug: "co-validate"
     wait: true
     timeout_seconds: 300
     prompt: "Please share your complete review now. List all issues found, categorized by severity (critical, important, minor)."
   ```

5. **Reconcile the two reviews:**
   - For each issue found by either reviewer:
     - If both agree: accept and update the plan
     - If only one found it: evaluate independently, accept or override with explanation
   - Present the reconciled review to the user with recommended changes

## Error Handling

If the subagent model is not available, tell the user:
"The co-validate skill requires the gpt-5.3-codex model. Please configure your OpenAI API key to use this skill."

Do NOT fall back to the same model.
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: Build succeeds.

**Step 3: Commit**

```bash
git add bundled_skills/co-validate/SKILL.md
git commit -m "feat: add co-validate bundled skill"
```

---

### Task 6: Run full test suite and verify

**Files:** None (verification only)

**Step 1: Build everything**

Run: `make build`
Expected: UI builds, templates build, Go binary builds successfully.

**Step 2: Run Go tests**

Run: `make test-go`
Expected: All tests pass.

**Step 3: Verify skills appear in discovery**

Run: `./bin/percy --model predictable --db test.db serve --port 8002`
Then in another terminal: `curl -s http://localhost:8002/api/skills | jq '.[] | select(.name | startswith("co-"))'`
Expected: Three skills returned: co-brainstorm, co-plan, co-validate.

**Step 4: Clean up test artifacts**

Run: `rm -f test.db`

**Step 5: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix: address test/build issues from co-command skills"
```
