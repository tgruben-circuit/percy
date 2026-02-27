package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tgruben-circuit/percy/db/generated"
)

type usageDateRow struct {
	Date             string  `json:"date"`
	Model            *string `json:"model"`
	MessageCount     int64   `json:"message_count"`
	TotalInputTokens float64 `json:"total_input_tokens"`
	TotalOutputTokens float64 `json:"total_output_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
}

type usageConversationRow struct {
	ConversationID    string  `json:"conversation_id"`
	Slug              *string `json:"slug"`
	Model             *string `json:"model"`
	MessageCount      int64   `json:"message_count"`
	TotalInputTokens  float64 `json:"total_input_tokens"`
	TotalOutputTokens float64 `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
}

type usageResponse struct {
	ByDate          []usageDateRow         `json:"by_date"`
	ByConversation  []usageConversationRow `json:"by_conversation"`
	TotalCostUSD    float64                `json:"total_cost_usd"`
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case nil:
		return 0
	default:
		return 0
	}
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse("2006-01-02", sinceStr)
		if err != nil {
			http.Error(w, "Invalid 'since' date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	} else {
		since = time.Now().AddDate(0, 0, -30) // Default: last 30 days
	}

	ctx := r.Context()

	// Get usage by date
	var byDateRows []generated.GetUsageByDateRow
	if err := s.db.Queries(ctx, func(q *generated.Queries) error {
		var err error
		byDateRows, err = q.GetUsageByDate(ctx, since)
		return err
	}); err != nil {
		s.logger.Error("Failed to get usage by date", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get usage by conversation
	var byConvRows []generated.GetUsageByConversationRow
	if err := s.db.Queries(ctx, func(q *generated.Queries) error {
		var err error
		byConvRows, err = q.GetUsageByConversation(ctx, since)
		return err
	}); err != nil {
		s.logger.Error("Failed to get usage by conversation", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert to response types
	var totalCost float64
	byDate := make([]usageDateRow, len(byDateRows))
	for i, row := range byDateRows {
		cost := toFloat64(row.TotalCostUsd)
		byDate[i] = usageDateRow{
			Date:              toString(row.Date),
			Model:             row.Model,
			MessageCount:      row.MessageCount,
			TotalInputTokens:  toFloat64(row.TotalInputTokens),
			TotalOutputTokens: toFloat64(row.TotalOutputTokens),
			TotalCostUSD:      cost,
		}
		totalCost += cost
	}

	byConv := make([]usageConversationRow, len(byConvRows))
	for i, row := range byConvRows {
		byConv[i] = usageConversationRow{
			ConversationID:    row.ConversationID,
			Slug:              row.Slug,
			Model:             row.Model,
			MessageCount:      row.MessageCount,
			TotalInputTokens:  toFloat64(row.TotalInputTokens),
			TotalOutputTokens: toFloat64(row.TotalOutputTokens),
			TotalCostUSD:      toFloat64(row.TotalCostUsd),
		}
	}

	resp := usageResponse{
		ByDate:         byDate,
		ByConversation: byConv,
		TotalCostUSD:   totalCost,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
