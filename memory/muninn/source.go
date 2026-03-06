package muninn

import (
	"context"

	memdb "github.com/tgruben-circuit/percy/memory"
)

// Source reads from MuninnDB and returns results as MemoryResults.
type Source struct {
	client *Client
}

// NewSource creates a new MuninnDB read source.
func NewSource(client *Client) *Source {
	return &Source{client: client}
}

// Search activates MuninnDB and returns results as MemoryResults.
func (s *Source) Search(ctx context.Context, query string, limit int) ([]memdb.MemoryResult, error) {
	resp, err := s.client.Activate(ctx, []string{query}, limit)
	if err != nil {
		return nil, err
	}
	results := make([]memdb.MemoryResult, len(resp.Activations))
	for i, a := range resp.Activations {
		results[i] = memdb.MemoryResult{
			ResultType: "muninn",
			TopicName:  a.Concept,
			Content:    a.Content,
			Score:      a.Score,
		}
	}
	return results, nil
}
