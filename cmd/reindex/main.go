package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/document"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/sqlite"
)

func main() {
	path := flag.String("db", "./data/open-ima.db", "path to open-ima SQLite database")
	flag.Parse()
	database, err := sqlite.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	count, err := reindex(context.Background(), database)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("enqueued %d documents for reindex\n", count)
}

func reindex(ctx context.Context, database *sql.DB) (int, error) {
	rows, err := database.QueryContext(ctx, `SELECT id FROM documents WHERE status NOT IN ('deleting')`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	jobs := queue.New(database)
	for _, id := range ids {
		if _, err := database.ExecContext(ctx,
			`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, document.StatusPending, id); err != nil {
			return 0, err
		}
		if _, err := jobs.Enqueue(ctx, ingest.JobParseDocument, map[string]string{"document_id": id}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
