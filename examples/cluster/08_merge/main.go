// Example 08_merge demonstrates the git merge pipeline provided by
// cluster.MergeWorktree. It runs two scenarios: a clean fast-forward-style
// merge and a conflicting merge resolved by a mock LLM resolver.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tgruben-circuit/percy/cluster"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// --- Create a temporary bare-ish git repo --------------------------------
	repoDir, err := os.MkdirTemp("", "percy-merge-example-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(repoDir)
	fmt.Println("📁 Created temp repo dir:", repoDir)

	if err := git(repoDir, "init", "-b", "main"); err != nil {
		return err
	}
	if err := git(repoDir, "config", "user.email", "test@test.com"); err != nil {
		return err
	}
	if err := git(repoDir, "config", "user.name", "Test"); err != nil {
		return err
	}

	// Initial commit on main.
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Merge Example\n"), 0o644); err != nil {
		return err
	}
	if err := git(repoDir, "add", "."); err != nil {
		return err
	}
	if err := git(repoDir, "commit", "-m", "initial commit"); err != nil {
		return err
	}
	fmt.Println("✅ Initial commit on main")

	// =========================================================================
	// Scenario 1: Clean Merge
	// =========================================================================
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("🧪 Scenario 1: Clean Merge")
	fmt.Println(strings.Repeat("=", 60))

	// Create branch worker-1 and add auth.go.
	if err := git(repoDir, "checkout", "-b", "worker-1"); err != nil {
		return err
	}
	authContent := "package auth\n\nfunc Authenticate(token string) bool {\n\treturn token != \"\"\n}\n"
	if err := os.WriteFile(filepath.Join(repoDir, "auth.go"), []byte(authContent), 0o644); err != nil {
		return err
	}
	if err := git(repoDir, "add", "auth.go"); err != nil {
		return err
	}
	if err := git(repoDir, "commit", "-m", "add auth.go"); err != nil {
		return err
	}
	fmt.Println("🌿 Created branch worker-1 with auth.go")

	// Switch back to main.
	if err := git(repoDir, "checkout", "main"); err != nil {
		return err
	}
	fmt.Println("🔀 Switched back to main")

	// Create MergeWorktree on main.
	fmt.Println("📦 Creating MergeWorktree on main...")
	mw, err := cluster.NewMergeWorktree(repoDir, "example-clean", "main")
	if err != nil {
		return fmt.Errorf("new merge worktree: %w", err)
	}
	defer mw.Cleanup()
	fmt.Println("📂 Worktree dir:", mw.Dir())

	// Merge worker-1 — should succeed cleanly.
	fmt.Println("🔄 Merging worker-1 into main...")
	result, err := mw.Merge(ctx, "worker-1", "Add authentication", nil)
	if err != nil {
		return fmt.Errorf("clean merge: %w", err)
	}
	fmt.Printf("✅ Merge result: status=%q  commit=%s\n", result.MergeStatus, result.MergeCommit[:12])

	// Verify auth.go exists in worktree.
	authPath := filepath.Join(mw.Dir(), "auth.go")
	if _, err := os.Stat(authPath); err != nil {
		return fmt.Errorf("auth.go not found in worktree: %w", err)
	}
	data, _ := os.ReadFile(authPath)
	fmt.Printf("📄 auth.go present in worktree (%d bytes)\n", len(data))

	mw.Cleanup()
	fmt.Println("🧹 Cleaned up worktree")

	// =========================================================================
	// Scenario 2: Conflict + Mock LLM Resolution
	// =========================================================================
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("🧪 Scenario 2: Conflict + Mock LLM Resolution")
	fmt.Println(strings.Repeat("=", 60))

	// On main, write main.go with "hello".
	mainGoContent := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(mainGoContent), 0o644); err != nil {
		return err
	}
	if err := git(repoDir, "add", "main.go"); err != nil {
		return err
	}
	if err := git(repoDir, "commit", "-m", "add main.go with hello"); err != nil {
		return err
	}
	fmt.Println(`📝 main: committed main.go with println("hello")`)

	// Create branch worker-2 and change "hello" → "goodbye".
	if err := git(repoDir, "checkout", "-b", "worker-2"); err != nil {
		return err
	}
	goodbyeContent := "package main\n\nfunc main() {\n\tprintln(\"goodbye\")\n}\n"
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(goodbyeContent), 0o644); err != nil {
		return err
	}
	if err := git(repoDir, "add", "main.go"); err != nil {
		return err
	}
	if err := git(repoDir, "commit", "-m", "change hello to goodbye"); err != nil {
		return err
	}
	fmt.Println(`🌿 worker-2: changed main.go to println("goodbye")`)

	// Back on main, change "hello" → "hi".
	if err := git(repoDir, "checkout", "main"); err != nil {
		return err
	}
	hiContent := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(hiContent), 0o644); err != nil {
		return err
	}
	if err := git(repoDir, "add", "main.go"); err != nil {
		return err
	}
	if err := git(repoDir, "commit", "-m", "change hello to hi"); err != nil {
		return err
	}
	fmt.Println(`📝 main: changed main.go to println("hi")`)

	// Create MergeWorktree on main for the conflict scenario.
	fmt.Println("📦 Creating MergeWorktree on main...")
	mw2, err := cluster.NewMergeWorktree(repoDir, "example-conflict", "main")
	if err != nil {
		return fmt.Errorf("new merge worktree: %w", err)
	}
	defer mw2.Cleanup()
	fmt.Println("📂 Worktree dir:", mw2.Dir())

	// Merge worker-2 — will conflict!
	fmt.Println("🔄 Merging worker-2 into main (expect conflict)...")
	result2, err := mw2.Merge(ctx, "worker-2", "Update greeting", mockResolver)
	if err != nil {
		return fmt.Errorf("conflict merge: %w", err)
	}
	fmt.Printf("✅ Merge result: status=%q  commit=%s\n", result2.MergeStatus, result2.MergeCommit[:12])

	// Verify the resolved content.
	resolvedData, err := os.ReadFile(filepath.Join(mw2.Dir(), "main.go"))
	if err != nil {
		return fmt.Errorf("read resolved main.go: %w", err)
	}
	fmt.Println("📄 Resolved main.go content:")
	for _, line := range strings.Split(strings.TrimRight(string(resolvedData), "\n"), "\n") {
		fmt.Printf("   │ %s\n", line)
	}

	mw2.Cleanup()
	fmt.Println("\n🧹 Cleaned up worktree")
	fmt.Println("\n🎉 Both scenarios completed successfully!")
	return nil
}

// mockResolver simulates an LLM-based conflict resolver. It prints what it
// received and returns a combined version that includes both changes.
func mockResolver(ctx context.Context, path, ours, theirs, base, taskTitle, taskDesc string) (string, error) {
	fmt.Println("\n🤖 Mock LLM Conflict Resolver invoked:")
	fmt.Printf("   📄 Path:       %s\n", path)
	fmt.Printf("   📋 Task:       %s\n", taskTitle)
	fmt.Printf("   🔵 Base:       %s\n", summarize(base))
	fmt.Printf("   🟢 Ours:       %s\n", summarize(ours))
	fmt.Printf("   🟡 Theirs:     %s\n", summarize(theirs))

	// Return a resolved version that combines both changes.
	resolved := "package main\n\nfunc main() {\n\tprintln(\"hi\")    // from main\n\tprintln(\"goodbye\") // from worker-2\n}\n"
	fmt.Printf("   ✨ Resolved:   %s\n", summarize(resolved))
	return resolved, nil
}

// summarize returns a one-line summary of file content for display.
func summarize(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) <= 1 {
		return strings.TrimSpace(content)
	}
	return fmt.Sprintf("%d lines — %s ... %s",
		len(lines),
		strings.TrimSpace(lines[0]),
		strings.TrimSpace(lines[len(lines)-1]))
}

// git runs a git command in the given directory.
func git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2024-01-01T00:00:00+00:00",
		"GIT_COMMITTER_DATE=2024-01-01T00:00:00+00:00",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), out, err)
	}
	return nil
}
