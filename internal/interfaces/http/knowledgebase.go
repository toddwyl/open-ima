package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	kbapp "open-ima/internal/application/knowledgebase"
	kbdom "open-ima/internal/domain/knowledgebase"
)

type knowledgeBaseHandler struct{ service *kbapp.Service }

func (h *knowledgeBaseHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs", h.handleCreate)
	mux.HandleFunc("GET /api/kbs", h.handleList)
	mux.HandleFunc("DELETE /api/kbs/{id}", h.handleDelete)
	mux.HandleFunc("POST /api/kbs/{id}/documents:url", h.handleIngestURL)
}

func (h *knowledgeBaseHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	knowledgeBase, err := h.service.Create(r.Context(), request.Name, request.Description)
	if errors.Is(err, kbdom.ErrNameTaken) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, knowledgeBase)
}

func (h *knowledgeBaseHandler) handleList(w http.ResponseWriter, r *http.Request) {
	knowledgeBases, err := h.service.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, knowledgeBases)
}

func (h *knowledgeBaseHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, kbdom.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *knowledgeBaseHandler) handleIngestURL(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	documentID, duplicate, err := h.service.IngestURL(r.Context(), r.PathValue("id"), request.URL)
	if errors.Is(err, kbapp.ErrInvalidURL) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, kbdom.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"document_id": documentID, "duplicate": duplicate})
}
