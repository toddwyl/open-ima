// Package server wires HTTP routes and background components.
package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"open-ima/internal/domain/document"
	"open-ima/internal/httpx"
	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/storage"
	"open-ima/internal/kb"
	"open-ima/internal/media"
	"open-ima/internal/rag"
	"open-ima/internal/settings"
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
	if err := settings.LoadIntoConfig(context.Background(), database, cfg); err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
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
	if err := meiliClient.EnsureIndex(context.Background(), cfg.Meili.Index, meili.EmbedderConfig{
		URL: cfg.Meili.EmbedderURL, Model: cfg.Meili.EmbedderModel, Dimensions: cfg.Meili.EmbedderDimensions,
	}); err != nil {
		return nil, fmt.Errorf("meilisearch ensure index: %w", err)
	}

	jobQueue := queue.New(database)
	mediaService := media.NewService(media.Deps{
		DB: database, Store: store, Queue: jobQueue,
		Parser: parser.New(cfg.Parser.URL),
		Meili:  meiliClient, Chunker: document.NewChunker(512, 80), MeiliIndex: cfg.Meili.Index,
	})
	worker := queue.NewWorker(jobQueue)
	mediaService.RegisterHandlers(worker)
	kbService := kb.NewService(database, mediaService, store)
	uploadHandler := upload.NewHandler(mediaService, store)
	ragService := rag.NewService(rag.Deps{
		DB: database, Meili: meiliClient,
		Chat: llm.NewChatClientWithProtocol(cfg.LLM.Protocol, cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model), MeiliIndex: cfg.Meili.Index,
	})
	settingsService := settings.New(database, cfg, meiliClient, ragService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	kbService.RegisterRoutes(mux)
	uploadHandler.RegisterRoutes(mux)
	ragService.RegisterRoutes(mux)
	settingsService.RegisterRoutes(mux)
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
			if errors.Is(err, media.ErrDocumentNotFound) {
				httpx.Error(w, http.StatusNotFound, err.Error())
				return
			}
			if errors.Is(err, media.ErrDocumentDeleting) {
				httpx.Error(w, http.StatusConflict, err.Error())
				return
			}
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/documents/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := mediaService.Retry(r.Context(), r.PathValue("id")); err != nil {
			if errors.Is(err, media.ErrDocumentNotFound) {
				httpx.Error(w, http.StatusNotFound, err.Error())
				return
			}
			if !errors.Is(err, media.ErrDocumentNotFailed) {
				httpx.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
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
