package kb

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"open-ima/internal/httpx"
	"open-ima/internal/media"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs", s.handleCreate)
	mux.HandleFunc("GET /api/kbs", s.handleList)
	mux.HandleFunc("DELETE /api/kbs/{id}", s.handleDelete)
	mux.HandleFunc("POST /api/kbs/{id}/documents:url", s.handleIngestURL)
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	knowledgeBase, err := s.Create(r.Context(), request.Name, request.Description)
	if errors.Is(err, ErrNameTaken) {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, knowledgeBase)
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	knowledgeBases, err := s.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, knowledgeBases)
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Delete(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleIngestURL(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.URL == "" {
		httpx.Error(w, http.StatusBadRequest, "url is required")
		return
	}
	documentID, duplicate, err := s.IngestURL(r.Context(), r.PathValue("id"), request.URL)
	if errors.Is(err, ErrInvalidURL) {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, media.ErrKnowledgeBaseNotFound) {
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"document_id": documentID, "duplicate": duplicate})
}
