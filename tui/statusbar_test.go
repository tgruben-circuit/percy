package tui

import (
	"strings"
	"testing"
)

func TestStatusBarWithCwd(t *testing.T) {
	s := StatusBar{
		Connected: true,
		Model:     "claude-sonnet-4",
		Width:     80,
		Cwd:       "/home/user/repos/percy",
	}
	view := s.View()
	if !strings.Contains(view, "/home/user/repos/percy") {
		t.Errorf("expected cwd in status bar, got %q", view)
	}
}

func TestStatusBarWithoutCwd(t *testing.T) {
	s := StatusBar{
		Connected: true,
		Model:     "claude-sonnet-4",
		Width:     80,
	}
	view := s.View()
	// Should not contain stray separators for missing cwd
	if strings.Contains(view, "| |") {
		t.Errorf("unexpected double separator in %q", view)
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		path string
		max  int
		want string
	}{
		{"/home/user/repos/percy", 30, "/home/user/repos/percy"},
		{"/home/user/repos/percy", 10, ".../percy"},
		{"/very/long/deeply/nested/path/to/project", 20, ".../path/to/project"},
		{"", 20, ""},
		{"/short", 10, "/short"},
	}
	for _, tt := range tests {
		got := shortenPath(tt.path, tt.max)
		if len(got) > tt.max && tt.path != "" {
			t.Errorf("shortenPath(%q, %d) = %q (len %d), exceeds max", tt.path, tt.max, got, len(got))
		}
		if tt.path != "" && got == "" {
			t.Errorf("shortenPath(%q, %d) returned empty", tt.path, tt.max)
		}
	}
}
