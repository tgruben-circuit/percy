package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tgruben-circuit/percy/llm"
	"github.com/tgruben-circuit/percy/llm/oai"
)

// DiscoverOllamaModels queries an Ollama instance for locally available models.
// baseURL should be like "http://localhost:11434" (no trailing slash).
func DiscoverOllamaModels(baseURL string, httpc *http.Client) ([]Model, error) {
	if httpc == nil {
		httpc = &http.Client{Timeout: 2 * time.Second}
	}

	resp, err := httpc.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama discovery: status %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama discovery: %w", err)
	}

	models := make([]Model, 0, len(result.Models))
	for _, m := range result.Models {
		modelName := m.Name
		endpoint := baseURL + "/v1"
		models = append(models, Model{
			ID:          "ollama/" + modelName,
			Provider:    ProviderOllama,
			Description: fmt.Sprintf("Ollama local: %s", modelName),
			Factory: func(config *Config, httpc *http.Client) (llm.Service, error) {
				return &oai.Service{
					Model:    oai.Model{ModelName: modelName, URL: endpoint},
					APIKey:   "ollama",
					ModelURL: endpoint,
					HTTPC:    httpc,
				}, nil
			},
		})
	}
	return models, nil
}
