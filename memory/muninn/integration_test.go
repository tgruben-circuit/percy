package muninn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// storedEngram is an engram held in the mock server.
type storedEngram struct {
	Vault   string   `json:"vault"`
	Concept string   `json:"concept"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

func newMockMuninn() (*httptest.Server, *mockState) {
	st := &mockState{engrams: make(map[string]storedEngram)}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/engrams/batch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Engrams []storedEngram `json:"engrams"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st.mu.Lock()
		ids := make([]string, len(req.Engrams))
		for i, e := range req.Engrams {
			id := fmt.Sprintf("eng-%d", st.nextID)
			st.nextID++
			st.engrams[id] = e
			ids[i] = id
		}
		st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"ids": ids})
	})

	mux.HandleFunc("POST /api/activate", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		var activations []Activation
		for id, e := range st.engrams {
			activations = append(activations, Activation{
				ID: id, Concept: e.Concept, Content: e.Content,
				Score: 0.8, Tags: e.Tags,
			})
		}
		st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ActivateResponse{Activations: activations})
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux), st
}

type mockState struct {
	mu      sync.Mutex
	nextID  int
	engrams map[string]storedEngram
}

func (m *mockState) snapshot() map[string]storedEngram {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]storedEngram, len(m.engrams))
	for k, v := range m.engrams {
		out[k] = v
	}
	return out
}

func TestFullCycle(t *testing.T) {
	ts, state := newMockMuninn()
	defer ts.Close()
	ctx := context.Background()

	// Step 1: Client + health check.
	client := NewClient(ts.URL, "test-vault", "test-token")
	if !client.Healthy(ctx) {
		t.Fatal("expected Healthy() == true")
	}

	// Step 2: Sink pushes 3 engrams.
	sink := NewSink(client)
	cells := []CellEngram{
		{Concept: "auth", Content: "JWT with refresh tokens", Tags: []string{"security"}},
		{Concept: "database", Content: "SQLite for local storage", Tags: []string{"storage"}},
		{Concept: "api", Content: "REST API on port 8080", Tags: []string{"http"}},
	}
	if err := sink.Push(ctx, "conv-1", "test-chat", cells); err != nil {
		t.Fatalf("Sink.Push: %v", err)
	}

	// Verify batch stored 3 engrams with correct tags.
	snap := state.snapshot()
	if got := len(snap); got != 3 {
		t.Fatalf("expected 3 stored engrams, got %d", got)
	}
	requiredTags := []string{"percy", "conv:conv-1", "slug:test-chat"}
	for id, e := range snap {
		for _, tag := range requiredTags {
			if !containsStr(e.Tags, tag) {
				t.Errorf("engram %s missing tag %q (tags=%v)", id, tag, e.Tags)
			}
		}
	}

	// Step 3: Source search.
	source := NewSource(client)
	results, err := source.Search(ctx, "authentication", 10)
	if err != nil {
		t.Fatalf("Source.Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}

	// All results must be muninn type.
	for _, r := range results {
		if r.ResultType != "muninn" {
			t.Errorf("expected ResultType=muninn, got %q", r.ResultType)
		}
	}

	// At least one result matches written content.
	wantContents := map[string]bool{
		"JWT with refresh tokens":    true,
		"SQLite for local storage":   true,
		"REST API on port 8080":      true,
	}
	found := false
	for _, r := range results {
		if wantContents[r.Content] {
			found = true
			break
		}
	}
	if !found {
		t.Error("no search result matched written content")
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
