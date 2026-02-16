package memory_test

import (
	"path/filepath"
	"testing"

	"github.com/tgruben-circuit/percy/memory"
)

func openTestDB(t *testing.T) *memory.DB {
	t.Helper()
	dir := t.TempDir()
	mdb, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mdb.Close() })
	return mdb
}

func TestInsertAndSearchCells(t *testing.T) {
	mdb := openTestDB(t)

	// Create a topic first so the cell has a parent.
	err := mdb.UpsertTopic(memory.Topic{
		TopicID: "topic-1",
		Name:    "Authentication",
		Summary: "JWT token authentication patterns",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert a cell.
	err = mdb.InsertCell(memory.Cell{
		CellID:     "cell-1",
		TopicID:    "topic-1",
		SourceType: "conversation",
		SourceID:   "conv-1",
		SourceName: "Auth Discussion",
		CellType:   "fact",
		Salience:   0.9,
		Content:    "The API uses JWT tokens for authentication with RS256 signing",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for the cell via FTS.
	results, err := mdb.SearchCellsFTS("JWT authentication", "", 10)
	if err != nil {
		t.Fatalf("SearchCellsFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result, got 0")
	}
	if results[0].CellID != "cell-1" {
		t.Errorf("expected cell-1, got %s", results[0].CellID)
	}
	if results[0].TopicID != "topic-1" {
		t.Errorf("expected topic-1, got %s", results[0].TopicID)
	}

	// Verify topic cell_count was updated.
	topic, err := mdb.GetTopic("topic-1")
	if err != nil {
		t.Fatal(err)
	}
	if topic == nil {
		t.Fatal("expected topic, got nil")
	}
	if topic.CellCount != 1 {
		t.Errorf("expected cell_count=1, got %d", topic.CellCount)
	}
}

func TestSupersedeCells(t *testing.T) {
	mdb := openTestDB(t)

	err := mdb.UpsertTopic(memory.Topic{
		TopicID: "topic-1",
		Name:    "Database Config",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert two cells.
	for _, c := range []memory.Cell{
		{
			CellID:     "cell-1",
			TopicID:    "topic-1",
			SourceType: "conversation",
			SourceID:   "conv-1",
			SourceName: "DB Setup",
			CellType:   "fact",
			Salience:   0.8,
			Content:    "Database connection pool uses max 20 connections",
		},
		{
			CellID:     "cell-2",
			TopicID:    "topic-1",
			SourceType: "conversation",
			SourceID:   "conv-1",
			SourceName: "DB Setup",
			CellType:   "decision",
			Salience:   0.7,
			Content:    "We decided to use PostgreSQL for the main database",
		},
	} {
		if err := mdb.InsertCell(c); err != nil {
			t.Fatal(err)
		}
	}

	// Supersede cell-1.
	if err := mdb.SupersedeCells([]string{"cell-1"}); err != nil {
		t.Fatal(err)
	}

	// FTS search should exclude superseded cells.
	results, err := mdb.SearchCellsFTS("connection pool", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.CellID == "cell-1" {
			t.Error("superseded cell-1 should not appear in search results")
		}
	}

	// Non-superseded cell should still be found.
	results, err = mdb.SearchCellsFTS("PostgreSQL", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected cell-2 in results")
	}
	if results[0].CellID != "cell-2" {
		t.Errorf("expected cell-2, got %s", results[0].CellID)
	}

	// GetCellsByTopic with includeSuperseded=false should exclude cell-1.
	cells, err := mdb.GetCellsByTopic("topic-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 non-superseded cell, got %d", len(cells))
	}
	if cells[0].CellID != "cell-2" {
		t.Errorf("expected cell-2, got %s", cells[0].CellID)
	}

	// GetCellsByTopic with includeSuperseded=true should include both.
	cells, err = mdb.GetCellsByTopic("topic-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells (including superseded), got %d", len(cells))
	}
}

func TestUpsertAndSearchTopic(t *testing.T) {
	mdb := openTestDB(t)

	// Create a topic with a summary.
	err := mdb.UpsertTopic(memory.Topic{
		TopicID: "topic-1",
		Name:    "Deployment Pipeline",
		Summary: "CI/CD pipeline configuration using GitHub Actions with Docker containers",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for the topic.
	results, err := mdb.SearchTopicsFTS("GitHub Actions", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 topic result")
	}
	if results[0].TopicID != "topic-1" {
		t.Errorf("expected topic-1, got %s", results[0].TopicID)
	}

	// Update the topic.
	err = mdb.UpsertTopic(memory.Topic{
		TopicID:   "topic-1",
		Name:      "Deployment Pipeline (updated)",
		Summary:   "Kubernetes deployment using ArgoCD for continuous delivery",
		CellCount: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for old summary should not match.
	oldResults, err := mdb.SearchTopicsFTS("GitHub Actions", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldResults) != 0 {
		t.Errorf("expected 0 results for old summary, got %d", len(oldResults))
	}

	// Search for new summary should match.
	newResults, err := mdb.SearchTopicsFTS("Kubernetes ArgoCD", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(newResults) == 0 {
		t.Fatal("expected results for updated summary")
	}
	if newResults[0].TopicID != "topic-1" {
		t.Errorf("expected topic-1, got %s", newResults[0].TopicID)
	}

	// Verify GetTopic returns updated data.
	topic, err := mdb.GetTopic("topic-1")
	if err != nil {
		t.Fatal(err)
	}
	if topic == nil {
		t.Fatal("expected topic, got nil")
	}
	if topic.Name != "Deployment Pipeline (updated)" {
		t.Errorf("expected updated name, got %s", topic.Name)
	}
	if topic.CellCount != 5 {
		t.Errorf("expected cell_count=5, got %d", topic.CellCount)
	}

	// AllTopics should return the topic.
	all, err := mdb.AllTopics()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 topic, got %d", len(all))
	}
	if all[0].TopicID != "topic-1" {
		t.Errorf("expected topic-1, got %s", all[0].TopicID)
	}

	// GetTopic for non-existent should return nil, nil.
	missing, err := mdb.GetTopic("does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing topic, got %+v", missing)
	}
}

func TestGetCellsByTopic(t *testing.T) {
	mdb := openTestDB(t)

	// Create two topics.
	for _, tp := range []memory.Topic{
		{TopicID: "topic-a", Name: "Frontend"},
		{TopicID: "topic-b", Name: "Backend"},
	} {
		if err := mdb.UpsertTopic(tp); err != nil {
			t.Fatal(err)
		}
	}

	// Insert cells into different topics with varying salience.
	cells := []memory.Cell{
		{CellID: "c1", TopicID: "topic-a", SourceType: "conversation", SourceID: "conv-1", SourceName: "FE Work", CellType: "fact", Salience: 0.5, Content: "React component architecture"},
		{CellID: "c2", TopicID: "topic-a", SourceType: "conversation", SourceID: "conv-1", SourceName: "FE Work", CellType: "decision", Salience: 0.9, Content: "Use TypeScript strict mode"},
		{CellID: "c3", TopicID: "topic-b", SourceType: "conversation", SourceID: "conv-2", SourceName: "BE Work", CellType: "fact", Salience: 0.7, Content: "Go backend with SQLite storage"},
	}
	for _, c := range cells {
		if err := mdb.InsertCell(c); err != nil {
			t.Fatal(err)
		}
	}

	// Get cells for topic-a, should be ordered by salience DESC.
	topicACells, err := mdb.GetCellsByTopic("topic-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(topicACells) != 2 {
		t.Fatalf("expected 2 cells for topic-a, got %d", len(topicACells))
	}
	// Higher salience first.
	if topicACells[0].CellID != "c2" {
		t.Errorf("expected c2 (salience 0.9) first, got %s", topicACells[0].CellID)
	}
	if topicACells[1].CellID != "c1" {
		t.Errorf("expected c1 (salience 0.5) second, got %s", topicACells[1].CellID)
	}

	// Get cells for topic-b.
	topicBCells, err := mdb.GetCellsByTopic("topic-b", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(topicBCells) != 1 {
		t.Fatalf("expected 1 cell for topic-b, got %d", len(topicBCells))
	}
	if topicBCells[0].CellID != "c3" {
		t.Errorf("expected c3, got %s", topicBCells[0].CellID)
	}

	// SearchCellsFTS with sourceType filter.
	results, err := mdb.SearchCellsFTS("TypeScript", "conversation", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for TypeScript search with conversation filter")
	}

	// DeleteCellsBySource should remove cells for that source.
	if err := mdb.DeleteCellsBySource("conversation", "conv-1"); err != nil {
		t.Fatal(err)
	}
	topicACells, err = mdb.GetCellsByTopic("topic-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(topicACells) != 0 {
		t.Errorf("expected 0 cells after delete, got %d", len(topicACells))
	}
}

func TestUnsummarizedCellCount(t *testing.T) {
	mdb := openTestDB(t)

	// Create a topic.
	err := mdb.UpsertTopic(memory.Topic{
		TopicID: "topic-1",
		Name:    "Testing",
		Summary: "Initial summary",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert cells after the topic was created/updated.
	for _, c := range []memory.Cell{
		{CellID: "c1", TopicID: "topic-1", SourceType: "conversation", SourceID: "conv-1", SourceName: "Test", CellType: "fact", Salience: 0.5, Content: "First unsummarized fact"},
		{CellID: "c2", TopicID: "topic-1", SourceType: "conversation", SourceID: "conv-1", SourceName: "Test", CellType: "fact", Salience: 0.5, Content: "Second unsummarized fact"},
	} {
		if err := mdb.InsertCell(c); err != nil {
			t.Fatal(err)
		}
	}

	count, err := mdb.UnsummarizedCellCount("topic-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 unsummarized cells, got %d", count)
	}
}
