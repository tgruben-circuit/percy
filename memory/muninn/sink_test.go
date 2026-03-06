package muninn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPush_TagConstruction(t *testing.T) {
	var gotBatches [][]struct {
		Vault   string   `json:"vault"`
		Concept string   `json:"concept"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Engrams []struct {
				Vault   string   `json:"vault"`
				Concept string   `json:"concept"`
				Content string   `json:"content"`
				Tags    []string `json:"tags"`
			} `json:"engrams"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotBatches = append(gotBatches, body.Engrams)
		ids := make([]string, len(body.Engrams))
		for i := range ids {
			ids[i] = "id-" + body.Engrams[i].Content
		}
		json.NewEncoder(w).Encode(map[string][]string{"ids": ids})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "testvault", "")
	sink := NewSink(client)

	cells := []CellEngram{
		{Concept: "topic-a", Content: "hello", Tags: []string{"user"}},
		{Concept: "topic-a", Content: "world", Tags: []string{"assistant"}},
	}

	err := sink.Push(context.Background(), "conv-123", "my-slug", cells)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(gotBatches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(gotBatches))
	}

	batch := gotBatches[0]
	if len(batch) != 2 {
		t.Fatalf("expected 2 engrams, got %d", len(batch))
	}

	// Check first engram
	e := batch[0]
	if e.Vault != "testvault" {
		t.Errorf("vault = %q, want %q", e.Vault, "testvault")
	}
	if e.Concept != "topic-a" {
		t.Errorf("concept = %q, want %q", e.Concept, "topic-a")
	}
	if e.Content != "hello" {
		t.Errorf("content = %q, want %q", e.Content, "hello")
	}
	wantTags := []string{"user", "percy", "conv:conv-123", "slug:my-slug"}
	if len(e.Tags) != len(wantTags) {
		t.Fatalf("tags = %v, want %v", e.Tags, wantTags)
	}
	for i, tag := range wantTags {
		if e.Tags[i] != tag {
			t.Errorf("tag[%d] = %q, want %q", i, e.Tags[i], tag)
		}
	}
}

func TestPush_EmptySlugOmitsTag(t *testing.T) {
	var gotTags []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Engrams []struct {
				Tags []string `json:"tags"`
			} `json:"engrams"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Engrams) > 0 {
			gotTags = body.Engrams[0].Tags
		}
		json.NewEncoder(w).Encode(map[string][]string{"ids": {"id-1"}})
	}))
	defer ts.Close()

	sink := NewSink(NewClient(ts.URL, "v", ""))
	err := sink.Push(context.Background(), "c1", "", []CellEngram{
		{Concept: "x", Content: "y", Tags: []string{"user"}},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	for _, tag := range gotTags {
		if tag == "slug:" {
			t.Error("empty slug tag should not be present")
		}
	}
	want := []string{"user", "percy", "conv:c1"}
	if len(gotTags) != len(want) {
		t.Fatalf("tags = %v, want %v", gotTags, want)
	}
}

func TestPush_BatchesAt50(t *testing.T) {
	batchCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Engrams []json.RawMessage `json:"engrams"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if batchCount == 0 && len(body.Engrams) != 50 {
			t.Errorf("first batch: got %d engrams, want 50", len(body.Engrams))
		}
		if batchCount == 1 && len(body.Engrams) != 10 {
			t.Errorf("second batch: got %d engrams, want 10", len(body.Engrams))
		}
		batchCount++
		ids := make([]string, len(body.Engrams))
		json.NewEncoder(w).Encode(map[string][]string{"ids": ids})
	}))
	defer ts.Close()

	sink := NewSink(NewClient(ts.URL, "v", ""))
	cells := make([]CellEngram, 60)
	for i := range cells {
		cells[i] = CellEngram{Concept: "c", Content: "x"}
	}

	if err := sink.Push(context.Background(), "conv", "s", cells); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if batchCount != 2 {
		t.Errorf("expected 2 batches, got %d", batchCount)
	}
}

func TestPush_NoCells(t *testing.T) {
	sink := NewSink(NewClient("http://unused", "v", ""))
	if err := sink.Push(context.Background(), "c", "s", nil); err != nil {
		t.Fatalf("Push with nil cells: %v", err)
	}
}
