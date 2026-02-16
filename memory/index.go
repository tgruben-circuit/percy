package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/tgruben-circuit/percy/llm"
)

// hashMessages returns a SHA-256 hex digest of concatenated role+text.
func hashMessages(messages []MessageText) string {
	h := sha256.New()
	for _, m := range messages {
		h.Write([]byte(m.Role))
		h.Write([]byte(m.Text))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// hashString returns a SHA-256 hex digest of s.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// IsIndexed returns true if the stored hash for (sourceType, sourceID)
// matches the provided hash and the hash is non-empty.
func (d *DB) IsIndexed(sourceType, sourceID, hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	var stored string
	err := d.db.QueryRow(
		`SELECT hash FROM index_state WHERE source_type = ? AND source_id = ?`,
		sourceType, sourceID,
	).Scan(&stored)
	if err != nil {
		return false, nil // not found is not an error
	}
	return stored == hash, nil
}

// SetIndexState records (or updates) the content hash for a source.
func (d *DB) SetIndexState(sourceType, sourceID, hash string) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO index_state (source_type, source_id, indexed_at, hash)
		 VALUES (?, ?, datetime('now'), ?)`,
		sourceType, sourceID, hash,
	)
	if err != nil {
		return fmt.Errorf("memory: set index state: %w", err)
	}
	return nil
}

// IndexConversation indexes a conversation's messages into the chunks table.
// It skips re-indexing when the content hash has not changed.
// If embedder is non-nil, embeddings are generated (errors are ignored for
// graceful degradation).
func (d *DB) IndexConversation(ctx context.Context, conversationID, slug string, messages []MessageText, embedder Embedder) error {
	hash := hashMessages(messages)

	indexed, err := d.IsIndexed("conversation", conversationID, hash)
	if err != nil {
		return err
	}
	if indexed {
		return nil
	}

	chunks := ChunkMessages(messages, 1024)

	if err := d.DeleteChunksBySource("conversation", conversationID); err != nil {
		return err
	}

	// Batch-embed all chunk texts if an embedder is provided.
	var embeddings [][]float32
	if embedder != nil {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}
		embeddings, _ = embedder.Embed(ctx, texts) // ignore errors — graceful degradation
	}

	for i, c := range chunks {
		chunkID := fmt.Sprintf("conv_%s_%d", conversationID, i)
		var embBlob []byte
		if i < len(embeddings) && embeddings[i] != nil {
			embBlob = SerializeEmbedding(embeddings[i])
		}
		if err := d.InsertChunk(chunkID, "conversation", conversationID, slug, c.Index, c.Text, embBlob); err != nil {
			return err
		}
	}

	return d.SetIndexState("conversation", conversationID, hash)
}

// IndexConversationV2 indexes a conversation using LLM-powered extraction.
// Falls back to chunk-based indexing if svc is nil.
func (d *DB) IndexConversationV2(ctx context.Context, conversationID, slug string, messages []MessageText, embedder Embedder, svc llm.Service) error {
	hash := hashMessages(messages)

	indexed, err := d.IsIndexed("conversation", conversationID, hash)
	if err != nil {
		return err
	}
	if indexed {
		return nil
	}

	// Extract cells using LLM or fallback.
	var extracted []ExtractedCell
	if svc != nil {
		extracted, err = ExtractCells(ctx, svc, messages)
		if err != nil || len(extracted) == 0 {
			// Fall back to chunk-based extraction.
			extracted = FallbackChunkToCells(conversationID, slug, messages)
		}
	} else {
		extracted = FallbackChunkToCells(conversationID, slug, messages)
	}

	// If no cells extracted at all, just record index state and return.
	if len(extracted) == 0 {
		return d.SetIndexState("conversation", conversationID, hash)
	}

	// Delete old cells for this conversation.
	if err := d.DeleteCellsBySource("conversation", conversationID); err != nil {
		return err
	}

	// Assign cells to topics.
	assigned, err := AssignCellsToTopics(ctx, d, extracted, embedder)
	if err != nil {
		return fmt.Errorf("memory: index v2 assign topics: %w", err)
	}

	// Insert each cell.
	affectedTopics := make(map[string]bool)
	for i, ac := range assigned {
		cellID := fmt.Sprintf("conv_%s_%d", conversationID, i)

		var embBlob []byte
		if embedder != nil {
			vecs, embedErr := embedder.Embed(ctx, []string{ac.Content})
			if embedErr == nil && len(vecs) > 0 && vecs[0] != nil {
				embBlob = SerializeEmbedding(vecs[0])
			}
		}

		cell := Cell{
			CellID:     cellID,
			TopicID:    ac.TopicID,
			SourceType: "conversation",
			SourceID:   conversationID,
			SourceName: slug,
			CellType:   ac.CellType,
			Salience:   ac.Salience,
			Content:    ac.Content,
			Embedding:  embBlob,
		}
		if err := d.InsertCell(cell); err != nil {
			return err
		}

		if ac.TopicID != "" {
			affectedTopics[ac.TopicID] = true
		}
	}

	// Check consolidation for each affected topic (best-effort).
	if svc != nil {
		for topicID := range affectedTopics {
			needs, err := NeedsConsolidation(d, topicID)
			if err != nil {
				slog.Warn("memory: consolidation check failed", "topic_id", topicID, "error", err)
				continue
			}
			if needs {
				if err := ConsolidateTopic(ctx, d, svc, embedder, topicID); err != nil {
					slog.Warn("memory: consolidation failed", "topic_id", topicID, "error", err)
				}
			}
		}
	}

	return d.SetIndexState("conversation", conversationID, hash)
}

// IndexFile indexes a file's content into the chunks table.
// It skips re-indexing when the content hash has not changed.
func (d *DB) IndexFile(ctx context.Context, filePath, fileName, content string, embedder Embedder) error {
	hash := hashString(content)

	indexed, err := d.IsIndexed("file", filePath, hash)
	if err != nil {
		return err
	}
	if indexed {
		return nil
	}

	chunks := ChunkMarkdown(content, 1024)

	if err := d.DeleteChunksBySource("file", filePath); err != nil {
		return err
	}

	var embeddings [][]float32
	if embedder != nil {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}
		embeddings, _ = embedder.Embed(ctx, texts)
	}

	// Use first 8 chars of the hash for chunk IDs.
	short := hash
	if len(short) > 8 {
		short = short[:8]
	}

	for i, c := range chunks {
		chunkID := fmt.Sprintf("file_%s_%d", short, i)
		var embBlob []byte
		if i < len(embeddings) && embeddings[i] != nil {
			embBlob = SerializeEmbedding(embeddings[i])
		}
		if err := d.InsertChunk(chunkID, "file", filePath, fileName, c.Index, c.Text, embBlob); err != nil {
			return err
		}
	}

	return d.SetIndexState("file", filePath, hash)
}
