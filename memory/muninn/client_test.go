package muninn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteEngram(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/engrams" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing auth header")
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Vault   string   `json:"vault"`
			Concept string   `json:"concept"`
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req.Vault != "percy" {
			t.Fatalf("vault = %q, want %q", req.Vault, "percy")
		}
		if req.Concept != "test-concept" {
			t.Fatalf("concept = %q", req.Concept)
		}
		if req.Content != "test-content" {
			t.Fatalf("content = %q", req.Content)
		}
		if len(req.Tags) != 1 || req.Tags[0] != "tag1" {
			t.Fatalf("tags = %v", req.Tags)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "engram-123"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "percy", "test-token")
	id, err := c.Write(context.Background(), "test-concept", "test-content", []string{"tag1"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "engram-123" {
		t.Fatalf("id = %q, want %q", id, "engram-123")
	}
}

func TestActivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/activate" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Vault      string   `json:"vault"`
			Context    []string `json:"context"`
			MaxResults int      `json:"max_results"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req.MaxResults != 5 {
			t.Fatalf("max_results = %d", req.MaxResults)
		}
		resp := ActivateResponse{
			Activations: []Activation{
				{ID: "a1", Concept: "go", Content: "Go is great", Score: 0.95, Tags: []string{"lang"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "percy", "")
	resp, err := c.Activate(context.Background(), []string{"query"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Activations) != 1 {
		t.Fatalf("got %d activations", len(resp.Activations))
	}
	a := resp.Activations[0]
	if a.ID != "a1" || a.Concept != "go" || a.Score != 0.95 {
		t.Fatalf("unexpected activation: %+v", a)
	}
}

func TestWriteBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/engrams/batch" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Engrams []json.RawMessage `json:"engrams"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if len(req.Engrams) != 2 {
			t.Fatalf("got %d engrams, want 2", len(req.Engrams))
		}
		json.NewEncoder(w).Encode(map[string][]string{"ids": {"id1", "id2"}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "percy", "")
	ids, err := c.WriteBatch(context.Background(), []WriteRequest{
		{Concept: "c1", Content: "content1", Tags: []string{"t1"}},
		{Concept: "c2", Content: "content2", Tags: []string{"t2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d ids, want 2", len(ids))
	}
}

func TestClientReturnsErrorOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "percy", "")
	_, err := c.Write(context.Background(), "c", "content", nil)
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stats" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "percy", "")
	if !c.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}

	// Unreachable server.
	c2 := NewClient("http://127.0.0.1:1", "percy", "")
	if c2.Healthy(context.Background()) {
		t.Fatal("expected unhealthy")
	}
}
