package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/document"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/db/dao"
	"open-ima/internal/infrastructure/queue"
)

func main() {
	path := flag.String("db", "./data/open-ima.db", "path to open-ima SQLite database")
	flag.Parse()
	database, err := db.Open(*path)
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
	documents := dao.NewDocumentDAO(database)
	ids, err := documents.ReindexableIDs(ctx, document.StatusDeleting)
	if err != nil {
		return 0, err
	}
	jobs := queue.New(database)
	for _, id := range ids {
		if err := documents.ResetForReindex(ctx, id, document.StatusPending); err != nil {
			return 0, err
		}
		if _, err := jobs.Enqueue(ctx, ingest.JobParseDocument, map[string]string{"document_id": id}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
