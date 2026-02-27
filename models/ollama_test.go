package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverOllamaModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "llama3.1:latest"},
				{"name": "codellama:13b"},
			},
		})
	}))
	defer server.Close()

	models, err := DiscoverOllamaModels(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "ollama/llama3.1:latest" {
		t.Fatalf("expected ollama/llama3.1:latest, got %s", models[0].ID)
	}
	if models[1].ID != "ollama/codellama:13b" {
		t.Fatalf("expected ollama/codellama:13b, got %s", models[1].ID)
	}
	if models[0].Provider != ProviderOllama {
		t.Fatalf("expected provider ollama, got %s", models[0].Provider)
	}
	// Test that factory works
	svc, err := models[0].Factory(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("factory returned nil service")
	}
}

func TestDiscoverOllamaModels_Unreachable(t *testing.T) {
	_, err := DiscoverOllamaModels("http://localhost:1", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
