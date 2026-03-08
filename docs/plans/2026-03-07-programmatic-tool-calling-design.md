# Programmatic Tool Calling Design

**Goal:** Reduce token consumption by letting the LLM write Python scripts that call Percy's tools programmatically. Intermediate tool results stay in the script — only the final printed output enters the conversation context. Works with any LLM provider.

**Inspiration:** [Anthropic's programmatic tool calling](https://platform.claude.com/docs/en/agents-and-tools/tool-use/programmatic-tool-calling), generalized to be provider-independent.

---

## Architecture

A new `scripted_tools` tool in `claudetool/`. The LLM writes a Python script; Percy spawns a subprocess with tool stubs injected. When the script calls a tool function, execution blocks, Percy fulfills the tool call via IPC, and execution resumes. Only `print()` output enters the LLM's context.

```
LLM                    Percy                     Python subprocess
 |                       |                            |
 |-- scripted_tools ---->|                            |
 |   (python script)     |-- spawn uv run python ---->|
 |                       |                            |-- runs script
 |                       |<-- {"tool":"read_file"} ---|   (hits tool stub)
 |                       |-- execute read_file        |
 |                       |-- {"result":"..."} ------->|   (script continues)
 |                       |<-- {"tool":"bash"} --------|   (hits another stub)
 |                       |-- execute bash             |
 |                       |-- {"result":"..."} ------->|   (script continues)
 |                       |                            |-- print("summary")
 |                       |<-- process exits ----------|
 |<-- "summary" ---------|                            
 |   (single tool result)|
```

**Which tools are exposed:** All tools in the ToolSet except `scripted_tools` itself (no recursion) and `subagent` (too complex). Stubs are generated dynamically from the tool list.

**Not deferred:** The schema is tiny (one `script` string field), so it's always available as a core tool.

---

## IPC Protocol

stdin/stdout carry JSON-lines IPC. The LLM's `print()` output goes to stderr (which Percy captures as the final result).

### Python harness (generated, prepended to LLM's script)

```python
import sys, json, asyncio

def _call_tool(name, input_dict):
    """Synchronous tool call via JSON-lines IPC over stdin/stdout."""
    req = json.dumps({"tool": name, "input": input_dict})
    sys.stdout.write(req + "\n")
    sys.stdout.flush()
    line = sys.stdin.readline()
    resp = json.loads(line)
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]

# Async wrappers — LLM can use await naturally
async def read_file(input): return _call_tool("read_file", input)
async def bash(input): return _call_tool("bash", input)
async def patch(input): return _call_tool("patch", input)
# ... one per exposed tool, generated dynamically

# Redirect print() to stderr so it becomes the tool result
_original_stdout = sys.stdout
sys.stdout = sys.stderr

async def _main():
    # === LLM's script inserted here ===
    pass

asyncio.run(_main())
```

- `sys.stdout` reassigned to `sys.stderr` after harness setup. `print()` → stderr → tool result.
- `_original_stdout` used exclusively for IPC.
- Tool stubs are synchronous under the hood (`_call_tool`) but wrapped as `async def` for ergonomics.

### Tool call request (Python → Percy, on real stdout)

```json
{"tool": "read_file", "input": {"path": "server/convo.go"}}
```

### Tool call response (Percy → Python, on stdin)

```json
{"result": "package server\nimport ...", "error": null}
```

Or on error:

```json
{"result": null, "error": "file not found: server/convo.go"}
```

---

## Go Implementation

### `claudetool/scripted_tools.go`

```go
type ScriptedToolsTool struct {
    Tools []*llm.Tool  // tools to expose (filtered)
}
```

The `Run` method:
1. Receives the `script` string from the LLM
2. Generates the Python harness with stubs for each tool in `Tools`
3. Writes harness + script to a temp file
4. Starts `uv run python tempfile` with pipes for stdin/stdout/stderr
5. Reads JSON-lines from subprocess stdout in a loop
6. For each tool call: finds the tool by name, calls `tool.Run(ctx, input)`, writes JSON response to subprocess stdin
7. When subprocess exits: reads stderr as the result text
8. Returns `ToolOut{LLMContent: TextContent(stderr_output)}`

---

## Tool Schema

```
Name: "scripted_tools"
Description: (dynamically generated, lists available tool names and usage example)
InputSchema: {
  "type": "object",
  "required": ["script"],
  "properties": {
    "script": {
      "type": "string",
      "description": "Python script to execute. Tool functions are pre-defined. Use print() for output."
    }
  }
}
```

The description dynamically lists available tool names, generated from the filtered tool set.

---

## Error Handling

| Error | Behavior |
|-------|----------|
| Tool call error | Serialized as `{"error": "..."}` in IPC. Python gets `RuntimeError`. If uncaught, traceback in stderr returned as result. |
| Script syntax/runtime error | Python exits non-zero. stderr (traceback + prior print output) returned as result. |
| `uv` not found | `exec.LookPath("uv")` fails. `ErrorfToolOut("uv not found in PATH")`. |
| Timeout | Subprocess killed. Return stderr so far + timeout note. Same timeout as bash tool. |
| Malformed IPC | Kill process, return stderr + parse error. No retry. |
| Unknown tool name | Return `{"error": "unknown tool: foo"}` on IPC. Script gets RuntimeError. |

Every error path either propagates to the LLM or crashes visibly. No fallbacks.

---

## Testing

### Unit tests (`claudetool/scripted_tools_test.go`)

1. Basic script execution — print only, no tool calls
2. Single tool call — IPC round-trip, only print output returned
3. Multiple tool calls in a loop — all dispatched correctly
4. Tool error handling — RuntimeError raised, traceback returned
5. Unknown tool — error propagated
6. Script syntax error — traceback returned
7. Timeout — killed, error returned
8. No uv in PATH — clean error message

All tests use real `Run` functions with mock tool implementations. No sleeps.

### Integration test (`loop/scripted_tools_test.go`)

Full flow: loop processes a turn with a `scripted_tools` call, script executes with tool calls, only final print output appears in conversation history. Intermediate tool results absent from recorded messages.

---

## Token Budget Impact

Consider a workflow reading 10 files to find relevant ones:

- **Without scripted_tools:** 10 `read_file` calls × ~2K tokens each = ~20K tokens in context, plus 10 LLM round-trips for reasoning between calls.
- **With scripted_tools:** 1 tool result with a summary = ~200 tokens in context, 0 intermediate LLM round-trips.

Savings scale linearly with the number of intermediate tool calls.
