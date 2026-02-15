// Package bundled_skills provides embedded skill definitions that ship with
// Percy. Each subdirectory contains a SKILL.md file following the Agent
// Skills specification (https://agentskills.io).
package bundled_skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/tgruben-circuit/percy/skills"
)

//go:embed *
var skillsFS embed.FS

var (
	once      sync.Once
	cached    []skills.Skill
	cachedErr error

	tmpDirOnce sync.Once
	tmpDir     string
	tmpDirErr  error
)

// EmbeddedSkills returns all bundled skills parsed from the embedded SKILL.md
// files. Results are cached after the first successful call. The embedded
// files are written to a temporary directory so that skills.Parse (which reads
// from disk) can process them.
func EmbeddedSkills() ([]skills.Skill, error) {
	once.Do(func() {
		cached, cachedErr = loadEmbeddedSkills()
	})
	return cached, cachedErr
}

func loadEmbeddedSkills() ([]skills.Skill, error) {
	entries, err := skillsFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded skills: %w", err)
	}

	dir, err := ensureTmpDir()
	if err != nil {
		return nil, err
	}

	var result []skills.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Extract all files in this skill directory to temp dir.
		if err := fs.WalkDir(skillsFS, name, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := skillsFS.ReadFile(path)
			if err != nil {
				return err
			}
			dest := filepath.Join(dir, path)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0o644)
		}); err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}

		skillPath := filepath.Join(dir, name, "SKILL.md")
		skill, err := skills.Parse(skillPath)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		result = append(result, skill)
	}

	return result, nil
}

func ensureTmpDir() (string, error) {
	tmpDirOnce.Do(func() {
		tmpDir, tmpDirErr = os.MkdirTemp("", "percy-bundled-skills-*")
	})
	return tmpDir, tmpDirErr
}
