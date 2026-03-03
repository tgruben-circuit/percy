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
