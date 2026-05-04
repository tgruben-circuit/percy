package claudetool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoWriteUpdateDoneWithoutVerifier(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(path, []byte("- [~] implement feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &TodoWriteTool{WorkingDir: NewMutableWorkingDir(dir)}
	input, err := json.Marshal(todoWriteInput{
		Operation: "update_status",
		TaskID:    1,
		Status:    "done",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result := tool.Run(context.Background(), input)
	if result.Error != nil {
		t.Fatalf("Run returned error: %v", result.Error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(data), "- [x] implement feature\n"; got != want {
		t.Fatalf("TODO.md = %q, want %q", got, want)
	}
}

func TestTodoWriteUpdateDoneVerifierPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(path, []byte("- [~] implement feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runner := &mockSubagentRunnerWithModel{response: "VERDICT: PASS"}

	tool := &TodoWriteTool{
		WorkingDir:           NewMutableWorkingDir(dir),
		DB:                   newMockSubagentDB(),
		ParentConversationID: "parent-123",
		Runner:               runner,
		VerifierModel:        "latest:verifier",
		VerifierEnabled:      true,
	}
	input, err := json.Marshal(todoWriteInput{
		Operation: "update_status",
		TaskID:    1,
		Status:    "done",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result := tool.Run(context.Background(), input)
	if result.Error != nil {
		t.Fatalf("Run returned error: %v", result.Error)
	}
	if runner.receivedModel != "latest:verifier" {
		t.Fatalf("verifier model = %q, want latest:verifier", runner.receivedModel)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(data), "- [x] implement feature\n"; got != want {
		t.Fatalf("TODO.md = %q, want %q", got, want)
	}
}

func TestTodoWriteUpdateDoneVerifierRejects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	original := "- [~] implement feature\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &TodoWriteTool{
		WorkingDir:           NewMutableWorkingDir(dir),
		DB:                   newMockSubagentDB(),
		ParentConversationID: "parent-123",
		Runner:               &mockSubagentRunnerWithModel{response: "VERDICT: FAIL - tests are missing"},
		VerifierModel:        "latest:verifier",
		VerifierEnabled:      true,
	}
	input, err := json.Marshal(todoWriteInput{
		Operation: "update_status",
		TaskID:    1,
		Status:    "done",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result := tool.Run(context.Background(), input)
	if result.Error == nil {
		t.Fatal("expected verifier rejection")
	}
	if !strings.Contains(result.Error.Error(), "tests are missing") {
		t.Fatalf("error = %q, want verifier reason", result.Error.Error())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != original {
		t.Fatalf("TODO.md = %q, want unchanged %q", got, original)
	}
}
