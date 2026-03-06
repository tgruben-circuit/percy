package muninn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WriteRequest is a single engram to write.
type WriteRequest struct {
	Concept string   `json:"concept"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// Activation is a single memory activation returned by Activate.
type Activation struct {
	ID      string   `json:"id"`
	Concept string   `json:"concept"`
	Content string   `json:"content"`
	Score   float64  `json:"score"`
	Tags    []string `json:"tags"`
}

// ActivateResponse is the response from the activate endpoint.
type ActivateResponse struct {
	Activations []Activation `json:"activations"`
}

// Client is a thin HTTP client for MuninnDB's REST API.
type Client struct {
	baseURL string
	vault   string
	token   string
	http    *http.Client
}

// NewClient creates a new MuninnDB client.
func NewClient(baseURL, vault, token string) *Client {
	return &Client{
		baseURL: baseURL,
		vault:   vault,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Write stores a single engram and returns its ID.
func (c *Client) Write(ctx context.Context, concept, content string, tags []string) (string, error) {
	payload := struct {
		Vault   string   `json:"vault"`
		Concept string   `json:"concept"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}{c.vault, concept, content, tags}
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/api/engrams", payload, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// WriteBatch stores up to 50 engrams and returns their IDs.
func (c *Client) WriteBatch(ctx context.Context, engrams []WriteRequest) ([]string, error) {
	type engramWithVault struct {
		Vault   string   `json:"vault"`
		Concept string   `json:"concept"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	items := make([]engramWithVault, len(engrams))
	for i, e := range engrams {
		items[i] = engramWithVault{c.vault, e.Concept, e.Content, e.Tags}
	}
	payload := struct {
		Engrams []engramWithVault `json:"engrams"`
	}{items}
	var resp struct {
		IDs []string `json:"ids"`
	}
	if err := c.post(ctx, "/api/engrams/batch", payload, &resp); err != nil {
		return nil, err
	}
	return resp.IDs, nil
}

// Activate performs context-aware memory retrieval.
func (c *Client) Activate(ctx context.Context, contexts []string, maxResults int) (*ActivateResponse, error) {
	payload := struct {
		Vault      string   `json:"vault"`
		Context    []string `json:"context"`
		MaxResults int      `json:"max_results"`
	}{c.vault, contexts, maxResults}
	var resp ActivateResponse
	if err := c.post(ctx, "/api/activate", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Healthy returns true if MuninnDB is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/stats", nil)
	if err != nil {
		return false
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) post(ctx context.Context, path string, payload, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("muninn: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("muninn: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("muninn: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("muninn: %s returned %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("muninn: decode: %w", err)
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
