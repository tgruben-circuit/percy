package memory

// MemoryResult is a unified search result from the two-tier search.
type MemoryResult struct {
	ResultType string  // "topic_summary" or "cell"
	TopicID    string
	TopicName  string
	CellID     string
	CellType   string
	SourceType string
	SourceID   string
	SourceName string
	Salience   float64
	Content    string // summary text for topics, cell content for cells
	Score      float64
	UpdatedAt  string
}

// TwoTierSearch performs a two-tier search: topic summaries first, then individual cells.
// queryVec can be nil for FTS-only search.
func (d *DB) TwoTierSearch(query string, queryVec []float32, sourceType string, limit int) ([]MemoryResult, error) {
	topicLimit := 3
	cellLimit := limit - topicLimit
	if cellLimit < 1 {
		cellLimit = 1
	}

	var results []MemoryResult

	// Tier 1: Topic summaries via FTS.
	topicResults, topicErr := d.SearchTopicsFTS(query, topicLimit)
	if topicErr == nil {
		for _, tr := range topicResults {
			results = append(results, MemoryResult{
				ResultType: "topic_summary",
				TopicID:    tr.TopicID,
				TopicName:  tr.Name,
				Content:    tr.Summary,
				Score:      tr.Score,
				UpdatedAt:  tr.UpdatedAt,
			})
		}
	}

	// Tier 2: Individual cells via FTS.
	cellResults, cellErr := d.SearchCellsFTS(query, sourceType, cellLimit)
	if cellErr == nil {
		for _, cr := range cellResults {
			results = append(results, MemoryResult{
				ResultType: "cell",
				TopicID:    cr.TopicID,
				CellID:     cr.CellID,
				CellType:   cr.CellType,
				SourceType: cr.SourceType,
				SourceID:   cr.SourceID,
				SourceName: cr.SourceName,
				Salience:   cr.Salience,
				Content:    cr.Content,
				Score:      cr.Score,
			})
		}
	}

	// If both tiers failed, return the cell error (or topic error).
	if topicErr != nil && cellErr != nil {
		return nil, cellErr
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
