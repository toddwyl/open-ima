package main

import (
	"log"
	"net/http"
	"os"

	"open-ima/internal/config"
	"open-ima/internal/db"
	"open-ima/internal/httpx"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, 200, map[string]string{"status": "ok"})
	})
	log.Printf("open-ima listening on %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
