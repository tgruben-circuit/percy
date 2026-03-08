package claudetool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tgruben-circuit/percy/llm"
)

const (
	scriptedToolsName    = "scripted_tools"
	defaultScriptTimeout = 2 * time.Minute

	scriptedToolsInputSchema = `
{
  "type": "object",
  "required": ["script"],
  "properties": {
    "script": {
      "type": "string",
      "description": "Python script to execute. Runs inside async def _main(). Can call tool functions and use print()."
    }
  }
}
`
)

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

// ScriptedToolsTool runs Python scripts that can call other tools via IPC.
type ScriptedToolsTool struct {
	Tools      []*llm.Tool
	WorkingDir *MutableWorkingDir
	Timeout    time.Duration
}

type scriptedToolsInput struct {
	Script string `json:"script"`
}

type ipcRequest struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type ipcResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func (s *ScriptedToolsTool) timeout() time.Duration {
	if s.Timeout == 0 {
		return defaultScriptTimeout
	}
	return s.Timeout
}

// description generates a dynamic description listing available tool names.
func (s *ScriptedToolsTool) description() string {
	scriptable := filterScriptableTools(s.Tools)
	names := make([]string, len(scriptable))
	for i, t := range scriptable {
		names[i] = t.Name
	}
	return fmt.Sprintf(`Execute a Python script that can call other tools programmatically.
Tool results stay in the script — only your print() output enters the conversation.
Use this when you need to call multiple tools and process/filter/aggregate results before reporting.

Available tools: %s

Each tool is an async function. Call with keyword arguments matching the tool's input schema:
  content = await read_file(path="foo.go")
  result = await bash(command="go test ./...")

Use print() for output — only printed text is returned.`, strings.Join(names, ", "))
}

// Tool returns the llm.Tool for scripted_tools.
func (s *ScriptedToolsTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        scriptedToolsName,
		Description: s.description(),
		InputSchema: llm.MustSchema(scriptedToolsInputSchema),
		Run:         s.Run,
	}
}

// Run executes a Python script with IPC-based tool calling.
func (s *ScriptedToolsTool) Run(ctx context.Context, m json.RawMessage) llm.ToolOut {
	var req scriptedToolsInput
	if err := json.Unmarshal(m, &req); err != nil {
		return llm.ErrorfToolOut("failed to unmarshal scripted_tools input: %w", err)
	}
	if strings.TrimSpace(req.Script) == "" {
		return llm.ErrorfToolOut("script must not be empty")
	}

	// Check that uv is available
	if _, err := exec.LookPath("uv"); err != nil {
		return llm.ErrorfToolOut("uv not found in PATH: install it from https://docs.astral.sh/uv/")
	}

	// Filter and generate harness
	scriptableTools := filterScriptableTools(s.Tools)
	harness := generateHarness(scriptableTools, req.Script)

	// Write harness to temp file
	tmpFile, err := os.CreateTemp("", "percy-script-*.py")
	if err != nil {
		return llm.ErrorfToolOut("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(harness); err != nil {
		tmpFile.Close()
		return llm.ErrorfToolOut("failed to write harness: %w", err)
	}
	tmpFile.Close()

	// Set up context with timeout
	execCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	// Spawn subprocess
	var stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, "uv", "run", "python", tmpFile.Name())
	cmd.Dir = s.WorkingDir.Get()
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return llm.ErrorfToolOut("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return llm.ErrorfToolOut("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return llm.ErrorfToolOut("failed to start script: %w", err)
	}

	// IPC loop: read requests from subprocess stdout, execute tools, write responses to stdin
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ipcReq ipcRequest
		if err := json.Unmarshal(line, &ipcReq); err != nil {
			continue
		}

		resp := s.executeTool(ctx, ipcReq)
		respBytes, err := json.Marshal(resp)
		if err != nil {
			errResp, _ := json.Marshal(ipcResponse{Error: "failed to marshal response"})
			stdin.Write(errResp)
			stdin.Write([]byte("\n"))
			continue
		}
		stdin.Write(respBytes)
		stdin.Write([]byte("\n"))
	}

	// Wait for process to exit
	waitErr := cmd.Wait()
	output := stderr.String()

	// Check for timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return llm.ErrorfToolOut("script timed out after %s\n%s", s.timeout(), output)
	}

	// On script error: return stderr as content (not as error) so LLM sees the traceback
	if waitErr != nil && output != "" {
		return llm.ToolOut{LLMContent: llm.TextContent(output)}
	}
	if waitErr != nil {
		return llm.ErrorfToolOut("script failed: %w", waitErr)
	}

	return llm.ToolOut{LLMContent: llm.TextContent(output)}
}

// executeTool finds and runs a tool by name, returning an IPC response.
func (s *ScriptedToolsTool) executeTool(ctx context.Context, req ipcRequest) ipcResponse {
	for _, t := range s.Tools {
		if t.Name == req.Tool {
			out := t.Run(ctx, req.Input)
			if out.Error != nil {
				return ipcResponse{Error: out.Error.Error()}
			}
			var texts []string
			for _, c := range out.LLMContent {
				if c.Text != "" {
					texts = append(texts, c.Text)
				}
			}
			return ipcResponse{Result: strings.Join(texts, "\n")}
		}
	}
	return ipcResponse{Error: fmt.Sprintf("unknown tool: %s", req.Tool)}
}
