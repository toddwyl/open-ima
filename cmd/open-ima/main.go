package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"open-ima/internal/app"
	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/db"
)

func main() {
	cfg, err := config.Load(os.Getenv("IMA_CONFIG"))
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	srv, err := app.New(cfg, database)
	if err != nil {
		log.Fatalf("build app: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Worker.Start(ctx, cfg.Worker.Concurrency)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := srv.Ingest.EnqueueReconcile(ctx); err != nil {
					log.Printf("reconcile enqueue: %v", err)
				}
			}
		}
	}()
	log.Printf("open-ima listening on %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, srv.Handler))
}
