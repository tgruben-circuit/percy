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
