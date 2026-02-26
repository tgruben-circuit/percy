package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is an HTTP client for the Percy API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Percy API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// ListConversations fetches the conversation list.
func (c *Client) ListConversations() ([]ConversationWithState, error) {
	var result []ConversationWithState
	err := c.getJSON("/api/conversations", &result)
	return result, err
}

// GetConversation fetches a single conversation's full history.
func (c *Client) GetConversation(id string) (StreamResponse, error) {
	var result StreamResponse
	err := c.getJSON("/api/conversation/"+id, &result)
	return result, err
}

// NewConversation creates a new conversation with an initial message.
// Returns the new conversation ID.
func (c *Client) NewConversation(message, model string) (string, error) {
	req := ChatRequest{Message: message, Model: model}
	var resp struct {
		ConversationID string `json:"conversation_id"`
	}
	err := c.postJSON("/api/conversations/new", req, &resp)
	return resp.ConversationID, err
}

// SendMessage sends a message to an existing conversation.
func (c *Client) SendMessage(conversationID, message string) error {
	req := ChatRequest{Message: message}
	return c.postJSON("/api/conversation/"+conversationID+"/chat", req, nil)
}

// CancelConversation cancels an active conversation.
func (c *Client) CancelConversation(id string) error {
	return c.postJSON("/api/conversation/"+id+"/cancel", nil, nil)
}

// ArchiveConversation archives a conversation.
func (c *Client) ArchiveConversation(id string) error {
	return c.postJSON("/api/conversation/"+id+"/archive", nil, nil)
}

// DeleteConversation deletes a conversation.
func (c *Client) DeleteConversation(id string) error {
	return c.postJSON("/api/conversation/"+id+"/delete", nil, nil)
}

// ListModels fetches available models.
func (c *Client) ListModels() ([]ModelInfo, error) {
	var result []ModelInfo
	err := c.getJSON("/api/models", &result)
	return result, err
}

func (c *Client) getJSON(path string, result any) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, body)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) postJSON(path string, body, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", reqBody)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: status %d: %s", path, resp.StatusCode, respBody)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}
