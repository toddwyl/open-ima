package httpapi

import (
	"errors"
	"net/http"

	"open-ima/internal/application/reading"
	"open-ima/internal/domain/media"
)

type readingHandler struct{ reading *reading.Service }

func (h *readingHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/medias/{id}/content", h.handleContent)
	mux.HandleFunc("POST /api/medias/{id}/open", h.handleOpen)
}

func (h *readingHandler) handleContent(w http.ResponseWriter, r *http.Request) {
	result, err := h.reading.Content(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *readingHandler) handleOpen(w http.ResponseWriter, r *http.Request) {
	if err := h.reading.Open(r.Context(), r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *readingHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, reading.ErrNotIndexed):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, reading.ErrIsURL):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
