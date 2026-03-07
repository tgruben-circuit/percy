package claudetool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tgruben-circuit/percy/llm"
)

const (
	requestToolsName        = "request_tools"
	requestToolsInputSchema = `{
	"type": "object",
	"required": ["category"],
	"properties": {
		"category": {
			"type": "string",
			"description": "The tool category to activate (e.g., 'browser', 'lsp')"
		}
	}
}`
)

type requestToolsInput struct {
	Category string `json:"category"`
}

// RequestToolsTool is a meta-tool that lets the LLM activate deferred tool categories on demand.
type RequestToolsTool struct {
	sync.RWMutex
	deferredTools []*llm.Tool
	activated     map[string]bool
}

// NewRequestToolsTool creates a new RequestToolsTool with the given deferred tools.
func NewRequestToolsTool(deferred []*llm.Tool) *RequestToolsTool {
	return &RequestToolsTool{
		deferredTools: deferred,
		activated:     make(map[string]bool),
	}
}

// HasDeferredTools reports whether there are any deferred tools to manage.
func (r *RequestToolsTool) HasDeferredTools() bool {
	return len(r.deferredTools) > 0
}

// Tool returns the llm.Tool for request_tools with a dynamically generated description.
func (r *RequestToolsTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        requestToolsName,
		Description: r.description(),
		InputSchema: llm.MustSchema(requestToolsInputSchema),
		Run:         r.Run,
	}
}

func (r *RequestToolsTool) description() string {
	r.RLock()
	defer r.RUnlock()

	var b strings.Builder
	b.WriteString("Activate a deferred tool category. Available categories:\n")
	for _, cat := range r.availableCategories() {
		var names []string
		for _, t := range r.deferredTools {
			if t.Category == cat {
				names = append(names, t.Name)
			}
		}
		fmt.Fprintf(&b, "- %s: %s\n", cat, strings.Join(names, ", "))
	}
	return strings.TrimSpace(b.String())
}

// Run activates a deferred tool category.
func (r *RequestToolsTool) Run(_ context.Context, input json.RawMessage) llm.ToolOut {
	var req requestToolsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return llm.ErrorfToolOut("failed to parse input: %v", err)
	}

	r.Lock()
	defer r.Unlock()

	// Validate category exists.
	found := false
	for _, t := range r.deferredTools {
		if t.Category == req.Category {
			found = true
			break
		}
	}
	if !found {
		return llm.ErrorfToolOut("unknown category %q; available: %s", req.Category, strings.Join(r.availableCategories(), ", "))
	}

	if r.activated[req.Category] {
		var names []string
		for _, t := range r.deferredTools {
			if t.Category == req.Category {
				names = append(names, t.Name)
			}
		}
		return llm.ToolOut{LLMContent: llm.TextContent(fmt.Sprintf("Category %q is already active. Tools: %s", req.Category, strings.Join(names, ", ")))}
	}

	r.activated[req.Category] = true

	var names []string
	for _, t := range r.deferredTools {
		if t.Category == req.Category {
			names = append(names, t.Name)
		}
	}
	return llm.ToolOut{LLMContent: llm.TextContent(fmt.Sprintf("Activated category %q. New tools available: %s", req.Category, strings.Join(names, ", ")))}
}

// IsCategoryActive reports whether a category has been activated.
func (r *RequestToolsTool) IsCategoryActive(category string) bool {
	r.RLock()
	defer r.RUnlock()
	return r.activated[category]
}

// AllActivated reports whether all deferred tool categories have been activated.
func (r *RequestToolsTool) AllActivated() bool {
	r.RLock()
	defer r.RUnlock()
	for _, t := range r.deferredTools {
		if !r.activated[t.Category] {
			return false
		}
	}
	return true
}

// FilterActiveTools returns non-deferred tools plus activated deferred tools.
// It excludes request_tools itself when all categories are activated.
func (r *RequestToolsTool) FilterActiveTools(tools []*llm.Tool) []*llm.Tool {
	r.RLock()
	defer r.RUnlock()

	allActive := true
	for _, t := range r.deferredTools {
		if !r.activated[t.Category] {
			allActive = false
			break
		}
	}

	var result []*llm.Tool
	for _, t := range tools {
		if t.Name == requestToolsName {
			if !allActive {
				result = append(result, t)
			}
			continue
		}
		if !t.Deferred {
			result = append(result, t)
			continue
		}
		if r.activated[t.Category] {
			result = append(result, t)
		}
	}
	return result
}

// availableCategories returns sorted unique categories that haven't been activated yet.
// Must be called with lock held.
func (r *RequestToolsTool) availableCategories() []string {
	seen := make(map[string]bool)
	var cats []string
	for _, t := range r.deferredTools {
		if r.activated[t.Category] || seen[t.Category] {
			continue
		}
		seen[t.Category] = true
		cats = append(cats, t.Category)
	}
	sort.Strings(cats)
	return cats
}
