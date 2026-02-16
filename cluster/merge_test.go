package cluster

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepo creates a temporary git repo with one commit on the given branch.
func setupGitRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "-b", branch},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("setup %v: %s: %v", args, out, err)
		}
	}

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("setup %v: %s: %v", args, out, err)
		}
	}
	return dir
}

func TestMergeWorktreeCreateAndCleanup(t *testing.T) {
	repoDir := setupGitRepo(t, "main")

	mw, err := NewMergeWorktree(repoDir, "agent1", "main")
	if err != nil {
		t.Fatal(err)
	}

	dir := mw.Dir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}

	mw.Cleanup()

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone after cleanup, err=%v", err)
	}
}

func TestMergeClean(t *testing.T) {
	repoDir := setupGitRepo(t, "main")

	// Create a worker branch with a new file.
	cmds := [][]string{
		{"git", "checkout", "-b", "worker-1"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	newFile := filepath.Join(repoDir, "feature.txt")
	if err := os.WriteFile(newFile, []byte("new feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add feature"},
		{"git", "checkout", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	mw, err := NewMergeWorktree(repoDir, "agent-merge", "main")
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Cleanup()

	ctx := context.Background()
	result, err := mw.Merge(ctx, "worker-1", "add feature", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.MergeStatus != "merged" {
		t.Fatalf("expected status 'merged', got %q", result.MergeStatus)
	}
	if result.MergeCommit == "" {
		t.Fatal("expected non-empty merge commit")
	}

	// Verify the file exists in the worktree.
	feat := filepath.Join(mw.Dir(), "feature.txt")
	if _, err := os.Stat(feat); err != nil {
		t.Fatalf("feature.txt should exist in worktree: %v", err)
	}
}

func TestMergeWithConflict(t *testing.T) {
	repoDir := setupGitRepo(t, "main")

	// Modify README on a worker branch.
	cmds := [][]string{
		{"git", "checkout", "-b", "worker-conflict"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Worker Change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "worker changes README"},
		{"git", "checkout", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	// Modify README on main so it conflicts.
	if err := os.WriteFile(readme, []byte("# Main Change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "main changes README"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	mw, err := NewMergeWorktree(repoDir, "agent-conflict", "main")
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Cleanup()

	mockResolver := func(ctx context.Context, path, ours, theirs, base, taskTitle, taskDesc string) (string, error) {
		return "# Merged\n", nil
	}

	ctx := context.Background()
	result, err := mw.Merge(ctx, "worker-conflict", "fix readme", mockResolver)
	if err != nil {
		t.Fatal(err)
	}
	if result.MergeStatus != "conflict_resolved" {
		t.Fatalf("expected status 'conflict_resolved', got %q", result.MergeStatus)
	}

	// Verify resolved content in worktree.
	data, err := os.ReadFile(filepath.Join(mw.Dir(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Merged\n" {
		t.Fatalf("expected resolved content '# Merged\\n', got %q", string(data))
	}
}

func TestMergeConflictNoResolver(t *testing.T) {
	repoDir := setupGitRepo(t, "main")

	// Create conflicting branches.
	cmds := [][]string{
		{"git", "checkout", "-b", "worker-noreso"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "worker readme"},
		{"git", "checkout", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	if err := os.WriteFile(readme, []byte("# Main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "main readme"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	mw, err := NewMergeWorktree(repoDir, "agent-noreso", "main")
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Cleanup()

	ctx := context.Background()
	_, err = mw.Merge(ctx, "worker-noreso", "test", nil)
	if err == nil {
		t.Fatal("expected error for conflict with nil resolver")
	}

	// Verify the repo is not left in a conflicted state by running git status.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = mw.Dir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status after abort: %s: %v", out, err)
	}
	outStr := strings.TrimSpace(string(out))
	if outStr != "" {
		t.Fatalf("repo should be clean after merge abort, got: %q", outStr)
	}
}

func TestDeleteBranch(t *testing.T) {
	repoDir := setupGitRepo(t, "main")

	// Create a branch, add a commit, merge it, then delete.
	cmds := [][]string{
		{"git", "checkout", "-b", "to-delete"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	f := filepath.Join(repoDir, "delete-me.txt")
	if err := os.WriteFile(f, []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "branch commit"},
		{"git", "checkout", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	mw, err := NewMergeWorktree(repoDir, "agent-del", "main")
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Cleanup()

	ctx := context.Background()
	result, err := mw.Merge(ctx, "to-delete", "test delete", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Update main to point to the merge commit so git considers to-delete fully merged.
	updateCmd := exec.Command("git", "-C", repoDir, "update-ref", "refs/heads/main", result.MergeCommit)
	if out, err := updateCmd.CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %s: %v", out, err)
	}

	if err := mw.DeleteBranch(ctx, "to-delete"); err != nil {
		t.Fatal(err)
	}

	// Verify branch is gone.
	cmd := exec.Command("git", "branch", "--list", "to-delete")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git branch list: %s: %v", out, err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("branch should be deleted, got: %q", string(out))
	}
}
