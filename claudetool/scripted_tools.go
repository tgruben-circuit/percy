package claudetool

import (
	"fmt"
	"strings"

	"github.com/tgruben-circuit/percy/llm"
)

const scriptedToolsName = "scripted_tools"

// excludedFromScripting lists tools that should not be exposed to scripted tools.
var excludedFromScripting = map[string]bool{
	scriptedToolsName: true,
	"subagent":        true,
	"request_tools":   true,
}

// filterScriptableTools returns tools that are allowed in scripted_tools.
func filterScriptableTools(tools []*llm.Tool) []*llm.Tool {
	var out []*llm.Tool
	for _, t := range tools {
		if !excludedFromScripting[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// generateHarness creates a Python script that wraps the user's script with
// IPC plumbing and tool stubs.
func generateHarness(tools []*llm.Tool, userScript string) string {
	var b strings.Builder

	// Imports and IPC setup
	b.WriteString(`import sys, json, asyncio

_ipc_out = sys.stdout

def _call_tool(name, input_dict):
    req = json.dumps({"tool": name, "input": input_dict})
    _ipc_out.write(req + "\n")
    _ipc_out.flush()
    line = sys.stdin.readline()
    if not line:
        raise EOFError("IPC channel closed")
    resp = json.loads(line)
    if "error" in resp and resp["error"]:
        raise RuntimeError(resp["error"])
    return resp["result"]

`)

	// Generate async stub for each tool
	for _, t := range tools {
		fmt.Fprintf(&b, "async def %s(**kwargs):\n", t.Name)
		fmt.Fprintf(&b, "    return _call_tool(%q, kwargs)\n\n", t.Name)
	}

	// Redirect stdout to stderr so print() becomes tool output
	b.WriteString("sys.stdout = sys.stderr\n\n")

	// Wrap user script in async def _main()
	b.WriteString("async def _main():\n")
	for _, line := range strings.Split(userScript, "\n") {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("\nasyncio.run(_main())\n")

	return b.String()
}
