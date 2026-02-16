package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// ConflictResolver resolves a merge conflict for a single file.
// It receives the file path, ours/theirs/base content, and task metadata.
// It returns the resolved file content.
type ConflictResolver func(ctx context.Context, path, ours, theirs, base, taskTitle, taskDesc string) (string, error)

// MergeResult holds the outcome of a merge operation.
type MergeResult struct {
	MergeStatus string // "merged", "conflict_resolved", "merge_failed"
	MergeCommit string
}

// MergeWorktree manages a dedicated git worktree for merge operations.
type MergeWorktree struct {
	repoDir string
	dir     string
	branch  string
}

// NewMergeWorktree creates a worktree at /tmp/percy-merge-{agentID}.
// It cleans up any stale worktree first, then creates a fresh one.
func NewMergeWorktree(repoDir, agentID, branch string) (*MergeWorktree, error) {
	dir := fmt.Sprintf("/tmp/percy-merge-%s", agentID)

	// Clean up stale worktree if it exists.
	if _, err := os.Stat(dir); err == nil {
		cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", dir)
		_ = cmd.Run() // best-effort
		os.RemoveAll(dir)
	}

	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", dir, branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("worktree add: %s: %w", out, err)
	}

	return &MergeWorktree{
		repoDir: repoDir,
		dir:     dir,
		branch:  branch,
	}, nil
}

// Dir returns the worktree directory path.
func (mw *MergeWorktree) Dir() string {
	return mw.dir
}

// Cleanup removes the worktree.
func (mw *MergeWorktree) Cleanup() {
	cmd := exec.Command("git", "-C", mw.repoDir, "worktree", "remove", "--force", mw.dir)
	if err := cmd.Run(); err != nil {
		slog.Warn("worktree remove failed, falling back to os.RemoveAll", "dir", mw.dir, "error", err)
	}
	os.RemoveAll(mw.dir)
}

// Merge merges branchName into the worktree's branch.
// If there are conflicts and a resolver is provided, it resolves them file by file.
func (mw *MergeWorktree) Merge(ctx context.Context, branchName, taskTitle string, resolver ConflictResolver) (MergeResult, error) {
	msg := fmt.Sprintf("Merge %s: %s", branchName, taskTitle)
	cmd := exec.CommandContext(ctx, "git", "merge", branchName, "--no-ff", "-m", msg)
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()

	if err == nil {
		// Clean merge.
		commit, err := mw.headCommit(ctx)
		if err != nil {
			return MergeResult{}, fmt.Errorf("head commit after merge: %w", err)
		}
		return MergeResult{MergeStatus: "merged", MergeCommit: commit}, nil
	}

	// Check if it's a conflict.
	if !strings.Contains(string(out), "CONFLICT") {
		return MergeResult{}, fmt.Errorf("merge failed: %s: %w", out, err)
	}

	if resolver == nil {
		mw.abortMerge(ctx)
		return MergeResult{}, fmt.Errorf("merge conflict with no resolver")
	}

	// Get conflicted files.
	files, err := mw.conflictedFiles(ctx)
	if err != nil {
		mw.abortMerge(ctx)
		return MergeResult{}, fmt.Errorf("list conflicted files: %w", err)
	}

	for _, path := range files {
		binary, err := mw.isBinary(ctx, path)
		if err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("check binary %q: %w", path, err)
		}
		if binary {
			slog.Warn("skipping binary conflict", "path", path)
			continue
		}

		ours, err := mw.gitShow(ctx, "HEAD", path)
		if err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("git show ours %q: %w", path, err)
		}

		theirs, err := mw.gitShow(ctx, branchName, path)
		if err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("git show theirs %q: %w", path, err)
		}

		base, err := mw.gitShowBase(ctx, branchName, path)
		if err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("git show base %q: %w", path, err)
		}

		resolved, err := resolver(ctx, path, ours, theirs, base, taskTitle, "")
		if err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("resolve conflict %q: %w", path, err)
		}

		fullPath := fmt.Sprintf("%s/%s", mw.dir, path)
		if err := os.WriteFile(fullPath, []byte(resolved), 0o644); err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("write resolved %q: %w", path, err)
		}

		addCmd := exec.CommandContext(ctx, "git", "add", path)
		addCmd.Dir = mw.dir
		if addOut, err := addCmd.CombinedOutput(); err != nil {
			mw.abortMerge(ctx)
			return MergeResult{}, fmt.Errorf("git add %q: %s: %w", path, addOut, err)
		}
	}

	// Commit the resolved merge.
	commitCmd := exec.CommandContext(ctx, "git", "commit", "--no-edit")
	commitCmd.Dir = mw.dir
	if commitOut, err := commitCmd.CombinedOutput(); err != nil {
		mw.abortMerge(ctx)
		return MergeResult{}, fmt.Errorf("commit after resolve: %s: %w", commitOut, err)
	}

	commit, err := mw.headCommit(ctx)
	if err != nil {
		return MergeResult{}, fmt.Errorf("head commit after resolve: %w", err)
	}

	return MergeResult{MergeStatus: "conflict_resolved", MergeCommit: commit}, nil
}

// DeleteBranch deletes the given branch from the repo (not the worktree).
func (mw *MergeWorktree) DeleteBranch(ctx context.Context, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", mw.repoDir, "branch", "-D", branchName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete branch %q: %s: %w", branchName, out, err)
	}
	return nil
}

func (mw *MergeWorktree) headCommit(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %s: %w", out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (mw *MergeWorktree) abortMerge(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "git", "merge", "--abort")
	cmd.Dir = mw.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("merge abort failed", "output", string(out), "error", err)
	}
}

func (mw *MergeWorktree) conflictedFiles(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("diff --name-only: %s: %w", out, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func (mw *MergeWorktree) gitShow(ctx context.Context, ref, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "show", fmt.Sprintf("%s:%s", ref, path))
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %s: %w", ref, path, out, err)
	}
	return string(out), nil
}

func (mw *MergeWorktree) gitShowBase(ctx context.Context, branchName, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "HEAD", branchName)
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("merge-base: %s: %w", out, err)
	}
	base := strings.TrimSpace(string(out))
	return mw.gitShow(ctx, base, path)
}

func (mw *MergeWorktree) isBinary(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "--diff-filter=U", "--", path)
	cmd.Dir = mw.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("diff --numstat %q: %s: %w", path, out, err)
	}
	return strings.HasPrefix(string(out), "-\t-\t"), nil
}
