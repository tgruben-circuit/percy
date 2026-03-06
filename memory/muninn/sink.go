package muninn

import "context"

const batchLimit = 50

// CellEngram maps a Percy memory cell to a MuninnDB engram.
type CellEngram struct {
	Concept string
	Content string
	Tags    []string
}

// Sink pushes memory cells to MuninnDB after conversation indexing.
type Sink struct{ client *Client }

// NewSink creates a Sink that writes through the given Client.
func NewSink(client *Client) *Sink { return &Sink{client: client} }

// Push converts cells to engrams and writes them in batches of 50.
func (s *Sink) Push(ctx context.Context, conversationID, slug string, cells []CellEngram) error {
	if len(cells) == 0 {
		return nil
	}

	// Build common extra tags.
	extra := []string{"percy", "conv:" + conversationID}
	if slug != "" {
		extra = append(extra, "slug:"+slug)
	}

	reqs := make([]WriteRequest, len(cells))
	for i, c := range cells {
		tags := make([]string, 0, len(c.Tags)+len(extra))
		tags = append(tags, c.Tags...)
		tags = append(tags, extra...)
		reqs[i] = WriteRequest{
			Concept: c.Concept,
			Content: c.Content,
			Tags:    tags,
		}
	}

	for i := 0; i < len(reqs); i += batchLimit {
		end := i + batchLimit
		if end > len(reqs) {
			end = len(reqs)
		}
		if _, err := s.client.WriteBatch(ctx, reqs[i:end]); err != nil {
			return err
		}
	}
	return nil
}
