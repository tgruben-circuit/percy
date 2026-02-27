package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/tgruben-circuit/percy/db/generated"
	"github.com/tgruben-circuit/percy/llm"
)

// TouchedFile represents a file the agent interacted with.
type TouchedFile struct {
	Path      string `json:"path"`
	Operation string `json:"operation"` // "read", "write", "patch", "navigate"
	Count     int    `json:"count"`     // number of interactions
}

func extractTouchedFiles(messages []generated.Message) []TouchedFile {
	type fileInfo struct {
		operation string
		count     int
	}
	files := make(map[string]*fileInfo)

	track := func(path, op string) {
		if path == "" {
			return
		}
		// Normalize path
		path = filepath.Clean(path)
		existing, ok := files[path]
		if !ok {
			files[path] = &fileInfo{operation: op, count: 1}
		} else {
			existing.count++
			// Escalate: read < write < patch
			if op == "patch" || (op == "write" && existing.operation == "read") {
				existing.operation = op
			}
		}
	}

	for _, msg := range messages {
		if msg.LlmData == nil {
			continue
		}
		var llmMsg llm.Message
		if err := json.Unmarshal([]byte(*msg.LlmData), &llmMsg); err != nil {
			continue
		}
		for _, content := range llmMsg.Content {
			if content.Type != llm.ContentTypeToolUse {
				continue
			}
			var input map[string]interface{}
			if err := json.Unmarshal(content.ToolInput, &input); err != nil {
				continue
			}
			switch content.ToolName {
			case "patch":
				if path, ok := input["path"].(string); ok {
					track(path, "patch")
				}
			case "read_file":
				if path, ok := input["path"].(string); ok {
					track(path, "read")
				}
			case "change_dir":
				if path, ok := input["path"].(string); ok {
					track(path, "navigate")
				}
			case "keyword_search":
				// keyword_search doesn't have explicit file paths
			case "output_iframe":
				if path, ok := input["path"].(string); ok {
					track(path, "read")
				}
			}
		}
	}

	result := make([]TouchedFile, 0, len(files))
	for path, info := range files {
		result = append(result, TouchedFile{
			Path:      path,
			Operation: info.operation,
			Count:     info.count,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result
}

func (s *Server) handleTouchedFiles(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")

	messages, err := s.db.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	files := extractTouchedFiles(messages)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}
