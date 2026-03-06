// Command muninn-backfill exports Percy memory cells and topic summaries to a MuninnDB server.
//
// Usage:
//
//	go run ./cmd/muninn-backfill -db /path/to/memory.db -url http://192.168.1.67:8475 [-vault percy] [-token ...] [-dry-run]
//
// It reads all non-superseded cells and all topic summaries from the SQLite memory
// database and writes them as engrams to MuninnDB, preserving the original created_at
// timestamps so that MuninnDB's temporal scoring reflects the true age of each memory.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type engram struct {
	Vault     string `json:"vault"`
	Concept   string `json:"concept"`
	Content   string `json:"content"`
	Tags      []string `json:"tags"`
	CreatedAt string `json:"created_at,omitempty"`
}

type batchReq struct {
	Engrams []engram `json:"engrams"`
}

type batchResp struct {
	IDs []string `json:"ids"`
}

func main() {
	dbPath := flag.String("db", "", "path to memory.db (required)")
	url := flag.String("url", "http://192.168.1.67:8475", "MuninnDB server URL")
	vault := flag.String("vault", "percy", "MuninnDB vault name")
	token := flag.String("token", "", "MuninnDB API token")
	dryRun := flag.Bool("dry-run", false, "print what would be sent without sending")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "usage: muninn-backfill -db /path/to/memory.db [-url ...] [-vault ...] [-token ...] [-dry-run]")
		os.Exit(1)
	}

	db, err := sql.Open("sqlite", *dbPath+"?mode=ro")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var engrams []engram

	// --- Topic summaries ---
	rows, err := db.Query(`SELECT name, COALESCE(summary, ''), updated_at FROM topics WHERE summary != '' AND summary IS NOT NULL`)
	if err != nil {
		log.Fatalf("query topics: %v", err)
	}
	for rows.Next() {
		var name, summary, updatedAt string
		if err := rows.Scan(&name, &summary, &updatedAt); err != nil {
			log.Fatalf("scan topic: %v", err)
		}
		engrams = append(engrams, engram{
			Vault:     *vault,
			Concept:   name,
			Content:   summary,
			Tags:      []string{"percy", "topic_summary", "backfill"},
			CreatedAt: toISO8601(updatedAt),
		})
	}
	rows.Close()

	// --- Cells ---
	rows, err = db.Query(`SELECT cell_type, source_name, COALESCE(topic_id, ''), content, created_at
		FROM cells WHERE superseded = FALSE ORDER BY created_at`)
	if err != nil {
		log.Fatalf("query cells: %v", err)
	}
	for rows.Next() {
		var cellType, sourceName, topicID, content, createdAt string
		if err := rows.Scan(&cellType, &sourceName, &topicID, &content, &createdAt); err != nil {
			log.Fatalf("scan cell: %v", err)
		}
		concept := sourceName
		if concept == "" {
			concept = topicID
		}
		tags := []string{"percy", cellType, "backfill"}
		if sourceName != "" {
			tags = append(tags, "slug:"+sourceName)
		}
		engrams = append(engrams, engram{
			Vault:     *vault,
			Concept:   concept,
			Content:   content,
			Tags:      tags,
			CreatedAt: toISO8601(createdAt),
		})
	}
	rows.Close()

	if len(engrams) == 0 {
		log.Println("no cells or topics to backfill")
		return
	}

	log.Printf("found %d engrams to backfill", len(engrams))

	if *dryRun {
		for i, e := range engrams {
			fmt.Printf("[%d] concept=%q created=%s tags=%v content=%.80s\n", i+1, e.Concept, e.CreatedAt, e.Tags, e.Content)
		}
		return
	}

	// Send in batches of 50
	client := &http.Client{Timeout: 30 * time.Second}
	total := 0
	for i := 0; i < len(engrams); i += 50 {
		end := i + 50
		if end > len(engrams) {
			end = len(engrams)
		}
		batch := engrams[i:end]

		body, err := json.Marshal(batchReq{Engrams: batch})
		if err != nil {
			log.Fatalf("marshal batch: %v", err)
		}

		req, err := http.NewRequest("POST", *url+"/api/engrams/batch", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if *token != "" {
			req.Header.Set("Authorization", "Bearer "+*token)
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("POST /api/engrams/batch: %v", err)
		}
		if resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Fatalf("MuninnDB returned %d: %s", resp.StatusCode, respBody)
		}

		var result batchResp
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		total += len(result.IDs)
		log.Printf("batch %d-%d: wrote %d engrams", i+1, end, len(result.IDs))
	}

	log.Printf("backfill complete: %d engrams written to %s vault=%s", total, *url, *vault)
}

// toISO8601 converts SQLite datetime ("2026-02-17 14:10:10") to ISO 8601 ("2026-02-17T14:10:10Z").
func toISO8601(sqliteTime string) string {
	if sqliteTime == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02 15:04:05", sqliteTime)
	if err != nil {
		return sqliteTime // pass through as-is
	}
	return t.UTC().Format(time.RFC3339)
}
