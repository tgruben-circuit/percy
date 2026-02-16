package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tgruben-circuit/percy/llm"
	memdb "github.com/tgruben-circuit/percy/memory"
)

const (
	memorySearchName        = "memory_search"
	memorySearchDescription = "Search past conversations and workspace files for relevant context.\nUse this when you need to recall previous discussions, decisions, or information from earlier sessions."
	memorySearchInputSchema = `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query to find relevant memories"
    },
    "source_type": {
      "type": "string",
      "enum": ["conversation", "file", "all"],
      "description": "Filter results by source type. Defaults to all."
    },
    "detail_level": {
      "type": "string",
      "enum": ["summary", "full"],
      "description": "summary: topic summaries only. full: summaries + individual cells. Defaults to full."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of results to return (default 10, max 25)"
    }
  }
}`
)

type searchInput struct {
	Query       string `json:"query"`
	SourceType  string `json:"source_type"`
	DetailLevel string `json:"detail_level"`
	Limit       int    `json:"limit"`
}

// MemorySearchTool provides semantic search over past conversations and files.
type MemorySearchTool struct {
	db       *memdb.DB
	embedder memdb.Embedder
}

// NewMemorySearchTool creates a new memory search tool.
func NewMemorySearchTool(db *memdb.DB, embedder memdb.Embedder) *MemorySearchTool {
	return &MemorySearchTool{db: db, embedder: embedder}
}

// Tool returns the llm.Tool definition for memory search.
func (t *MemorySearchTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        memorySearchName,
		Description: memorySearchDescription,
		InputSchema: llm.MustSchema(memorySearchInputSchema),
		Run:         t.Run,
	}
}

// Run executes the memory search tool.
func (t *MemorySearchTool) Run(ctx context.Context, input json.RawMessage) llm.ToolOut {
	var in searchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return llm.ErrorfToolOut("failed to parse input: %w", err)
	}

	if t.db == nil {
		return llm.ToolOut{LLMContent: llm.TextContent("No memory index found. Memory search is not available in this session.")}
	}

	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.Limit > 25 {
		in.Limit = 25
	}

	sourceType := in.SourceType
	if sourceType == "all" {
		sourceType = ""
	}

	var queryVec []float32
	if t.embedder != nil {
		vecs, err := t.embedder.Embed(ctx, []string{in.Query})
		if err == nil && len(vecs) > 0 {
			queryVec = vecs[0]
		}
	}

	results, err := t.db.TwoTierSearch(in.Query, queryVec, sourceType, in.Limit)
	if err != nil {
		return llm.ErrorfToolOut("memory search failed: %w", err)
	}

	// Filter by detail level.
	if in.DetailLevel == "summary" {
		var summaryOnly []memdb.MemoryResult
		for _, r := range results {
			if r.ResultType == "topic_summary" {
				summaryOnly = append(summaryOnly, r)
			}
		}
		results = summaryOnly
	}

	if len(results) == 0 {
		return llm.ToolOut{LLMContent: llm.TextContent(fmt.Sprintf("No relevant memories found for: %s", in.Query))}
	}

	return llm.ToolOut{
		LLMContent: llm.TextContent(formatMemoryResults(results)),
		Display:    results,
	}
}

// formatMemoryResults formats two-tier search results as human-readable text for the LLM.
func formatMemoryResults(results []memdb.MemoryResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d relevant memories:\n\n", len(results))
	for i, r := range results {
		switch r.ResultType {
		case "topic_summary":
			fmt.Fprintf(&b, "--- Topic Summary: %q (updated %s) ---\n%s\n\n", r.TopicName, r.UpdatedAt, r.Content)
		case "cell":
			fmt.Fprintf(&b, "--- Result %d [%s] (score: %.2f, salience: %.1f) ---\n%s\n\n", i+1, r.CellType, r.Score, r.Salience, r.Content)
		}
	}
	return b.String()
}
