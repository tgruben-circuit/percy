// Package bundled_skills provides embedded skill definitions that ship with
// Shelley. Each subdirectory contains a SKILL.md file following the Agent
// Skills specification (https://agentskills.io).
package bundled_skills

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"shelley.exe.dev/skills"
)

//go:embed */SKILL.md
var fs embed.FS

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
	entries, err := fs.ReadDir(".")
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
		data, err := fs.ReadFile(filepath.Join(name, "SKILL.md"))
		if err != nil {
			continue
		}

		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", skillDir, err)
		}

		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", skillPath, err)
		}

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
		tmpDir, tmpDirErr = os.MkdirTemp("", "shelley-bundled-skills-*")
	})
	return tmpDir, tmpDirErr
}
