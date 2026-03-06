package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/activate" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		resp := ActivateResponse{
			Activations: []Activation{
				{ID: "a1", Concept: "auth", Content: "JWT tokens", Score: 0.9, Tags: []string{"security"}},
				{ID: "a2", Concept: "api", Content: "REST endpoints", Score: 0.8, Tags: []string{"http"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	src := NewSource(NewClient(srv.URL, "percy", ""))
	results, err := src.Search(context.Background(), "authentication", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	r := results[0]
	if r.ResultType != "muninn" {
		t.Errorf("ResultType = %q, want %q", r.ResultType, "muninn")
	}
	if r.TopicName != "auth" {
		t.Errorf("TopicName = %q, want %q", r.TopicName, "auth")
	}
	if r.Content != "JWT tokens" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Score != 0.9 {
		t.Errorf("Score = %f, want 0.9", r.Score)
	}
}

func TestSourceSearchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := NewSource(NewClient(srv.URL, "percy", ""))
	_, err := src.Search(context.Background(), "query", 5)
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestSourceSearchEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ActivateResponse{})
	}))
	defer srv.Close()

	src := NewSource(NewClient(srv.URL, "percy", ""))
	results, err := src.Search(context.Background(), "query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
