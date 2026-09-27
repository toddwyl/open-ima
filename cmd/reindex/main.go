package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
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
	fmt.Printf("enqueued %d medias for reindex\n", count)
}

// reindex 与 POST /api/reindex 共用同一套领域语义:
// 全部非 deleting 文档重置为 pending 并重新投递解析任务。
func reindex(ctx context.Context, database *sql.DB) (int, error) {
	medias := media.NewMediaService(db.NewMediaRepository(database))
	ids, err := medias.ReindexableIDs(ctx)
	if err != nil {
		return 0, err
	}
	jobs := queue.New(database)
	for _, id := range ids {
		if err := medias.ResetForReindex(ctx, id); err != nil {
			return 0, err
		}
		if _, err := jobs.Enqueue(ctx, ingest.JobParseMedia, map[string]string{"media_biz_id": id}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
