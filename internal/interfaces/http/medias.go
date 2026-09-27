package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"open-ima/internal/application/ingest"
	"open-ima/internal/application/port"
	kbdom "open-ima/internal/domain/knowledgebase"
	"open-ima/internal/domain/media"
)

var allowedExtensions = map[string]string{
	".pdf": "pdf", ".docx": "docx", ".pptx": "pptx", ".md": "md",
	".txt": "txt", ".html": "html", ".htm": "html",
}

type mediasHandler struct {
	ingest   *ingest.Service
	store    port.FileStore
	maxBytes int64
}

func (h *mediasHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs/{id}/medias", h.handleUpload)
	mux.HandleFunc("GET /api/kbs/{id}/medias", h.handleList)
	mux.HandleFunc("DELETE /api/medias/{id}", h.handleDelete)
	mux.HandleFunc("POST /api/medias/{id}/retry", h.handleRetry)
	mux.HandleFunc("POST /api/reindex", h.handleReindex)
}

func (h *mediasHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes)
	if err := r.ParseMultipartForm(h.maxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	extension := strings.ToLower(filepath.Ext(header.Filename))
	fileType, ok := allowedExtensions[extension]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported file type: "+extension)
		return
	}

	temporary, err := os.CreateTemp("", "open-ima-upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hasher), file); err != nil {
		_ = temporary.Close()
		writeError(w, http.StatusBadRequest, "file too large or read failed")
		return
	}
	if err := temporary.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := hex.EncodeToString(hasher.Sum(nil))
	storedFile, err := os.Open(temporaryPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer storedFile.Close()
	if err := h.store.Put(r.Context(), key, storedFile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	title := strings.TrimSuffix(filepath.Base(header.Filename), extension)
	mediaBizID, duplicate, err := h.ingest.CreateMedia(
		r.Context(), r.PathValue("id"), title, "file", key, fileType, key,
	)
	if errors.Is(err, kbdom.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"media_biz_id": mediaBizID, "duplicate": duplicate})
}

func (h *mediasHandler) handleList(w http.ResponseWriter, r *http.Request) {
	documents, err := h.ingest.List(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (h *mediasHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.ingest.DeleteMedia(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, media.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, media.ErrDeleting) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *mediasHandler) handleRetry(w http.ResponseWriter, r *http.Request) {
	if err := h.ingest.RetryMedia(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, media.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if !errors.Is(err, media.ErrNotFailed) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "requeued"})
}

// handleReindex 触发全量重建索引:存量文档重置后重新走解析→分块→索引流水线。
func (h *mediasHandler) handleReindex(w http.ResponseWriter, r *http.Request) {
	enqueued, err := h.ingest.EnqueueReindex(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"enqueued": enqueued})
}
