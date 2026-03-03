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
