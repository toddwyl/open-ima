// Package server wires HTTP routes and background components.
package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"

	"open-ima/internal/chunker"
	"open-ima/internal/config"
	"open-ima/internal/httpx"
	"open-ima/internal/kb"
	"open-ima/internal/llm"
	"open-ima/internal/media"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/rag"
	"open-ima/internal/storage"
	"open-ima/internal/upload"
	frontend "open-ima/web"
)

type Server struct {
	Handler http.Handler
	Worker  *queue.Worker
	Media   *media.Service
	Queue   *queue.Queue
	KB      *kb.Service
	RAG     *rag.Service
}

func New(cfg *config.Config, database *sql.DB) (*Server, error) {
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	store, err := storage.NewLocalStorage(
		filepath.Join(cfg.DataDir, "files"), cfg.PublicBaseURL, hex.EncodeToString(secret),
	)
	if err != nil {
		return nil, err
	}
	meiliClient := meili.New(cfg.Meili.URL, cfg.Meili.APIKey)
	if err := meiliClient.EnsureIndex(context.Background(), cfg.Meili.Index, cfg.Embedding.Dimensions); err != nil {
		return nil, fmt.Errorf("meilisearch ensure index: %w", err)
	}

	jobQueue := queue.New(database)
	mediaService := media.NewService(media.Deps{
		DB: database, Store: store, Queue: jobQueue,
		Parser:   parserclient.New(cfg.Parser.URL),
		Embedder: llm.NewEmbeddingClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model),
		Meili:    meiliClient, Chunker: chunker.New(512, 80), MeiliIndex: cfg.Meili.Index,
	})
	worker := queue.NewWorker(jobQueue)
	mediaService.RegisterHandlers(worker)
	kbService := kb.NewService(database, mediaService, store)
	uploadHandler := upload.NewHandler(mediaService, store)
	ragService := rag.NewService(rag.Deps{
		DB: database, Meili: meiliClient,
		Embedder: llm.NewEmbeddingClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model),
		Chat:     llm.NewChatClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model), MeiliIndex: cfg.Meili.Index,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	kbService.RegisterRoutes(mux)
	uploadHandler.RegisterRoutes(mux)
	ragService.RegisterRoutes(mux)
	mux.HandleFunc("GET /api/kbs/{id}/documents", func(w http.ResponseWriter, r *http.Request) {
		documents, err := mediaService.List(r.Context(), r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, documents)
	})
	mux.HandleFunc("DELETE /api/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := mediaService.Delete(r.Context(), r.PathValue("id")); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/documents/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := mediaService.Retry(r.Context(), r.PathValue("id")); err != nil {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "requeued"})
	})
	mux.Handle("GET /internal/files/{key}", store.Handler())
	mux.Handle("/", frontend.Handler())

	return &Server{
		Handler: mux, Worker: worker, Media: mediaService, Queue: jobQueue, KB: kbService, RAG: ragService,
	}, nil
}
