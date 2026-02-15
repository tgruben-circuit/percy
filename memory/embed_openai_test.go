package memory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder(t *testing.T) {
	wantEmbeddings := [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected path /embeddings, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", auth)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req openAIEmbedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("expected model %q, got %q", "text-embedding-3-small", req.Model)
		}
		if len(req.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(req.Input))
		}

		resp := openAIEmbedResponse{
			Data: make([]openAIEmbedding, len(wantEmbeddings)),
		}
		for i, emb := range wantEmbeddings {
			resp.Data[i] = openAIEmbedding{Embedding: emb}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key")
	e.url = srv.URL

	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != len(wantEmbeddings) {
		t.Fatalf("expected %d embeddings, got %d", len(wantEmbeddings), len(vecs))
	}
	for i := range wantEmbeddings {
		if len(vecs[i]) != len(wantEmbeddings[i]) {
			t.Fatalf("embedding %d: expected dimension %d, got %d", i, len(wantEmbeddings[i]), len(vecs[i]))
		}
		for j := range wantEmbeddings[i] {
			if vecs[i][j] != wantEmbeddings[i][j] {
				t.Fatalf("embedding %d[%d]: expected %f, got %f", i, j, wantEmbeddings[i][j], vecs[i][j])
			}
		}
	}
}

func TestOpenAIEmbedderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("bad-key")
	e.url = srv.URL
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
}

func TestOpenAIEmbedderDimension(t *testing.T) {
	e := NewOpenAIEmbedder("test-key")
	if d := e.Dimension(); d != 1536 {
		t.Fatalf("expected dimension 1536, got %d", d)
	}
}
