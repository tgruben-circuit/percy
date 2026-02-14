package bundled_skills

import (
	"testing"
)

func TestEmbeddedSkillsReturnsAllThree(t *testing.T) {
	skills, err := EmbeddedSkills()
	if err != nil {
		t.Fatalf("EmbeddedSkills() error: %v", err)
	}
	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(skills))
	}

	want := map[string]bool{
		"claude-code": false,
		"opencode":    false,
		"gemini-cli":  false,
	}
	for _, s := range skills {
		if _, ok := want[s.Name]; !ok {
			t.Errorf("unexpected skill name: %q", s.Name)
			continue
		}
		want[s.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing expected skill: %q", name)
		}
	}
}

func TestEmbeddedSkillsHaveDescriptions(t *testing.T) {
	skills, err := EmbeddedSkills()
	if err != nil {
		t.Fatalf("EmbeddedSkills() error: %v", err)
	}
	for _, s := range skills {
		if s.Description == "" {
			t.Errorf("skill %q has empty Description", s.Name)
		}
		if s.Path == "" {
			t.Errorf("skill %q has empty Path", s.Name)
		}
	}
}

func TestEmbeddedSkillsIdempotent(t *testing.T) {
	first, err := EmbeddedSkills()
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := EmbeddedSkills()
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("first call returned %d skills, second returned %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Errorf("skill %d: first=%q, second=%q", i, first[i].Name, second[i].Name)
		}
		if first[i].Description != second[i].Description {
			t.Errorf("skill %d description mismatch", i)
		}
		if first[i].Path != second[i].Path {
			t.Errorf("skill %d path mismatch: first=%q, second=%q", i, first[i].Path, second[i].Path)
		}
	}
}
