// Package upload validates and stores multipart document uploads.
package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"open-ima/internal/httpx"
	"open-ima/internal/media"
	"open-ima/internal/storage"
)

var allowedExtensions = map[string]string{
	".pdf": "pdf", ".docx": "docx", ".pptx": "pptx", ".md": "md",
	".txt": "txt", ".html": "html", ".htm": "html",
}

type Handler struct {
	media    *media.Service
	store    storage.Storage
	MaxBytes int64
}

func NewHandler(mediaService *media.Service, store storage.Storage) *Handler {
	return &Handler{media: mediaService, store: store, MaxBytes: 50 << 20}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs/{id}/documents", h.handleUpload)
}

func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxBytes)
	if err := r.ParseMultipartForm(h.MaxBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	extension := strings.ToLower(filepath.Ext(header.Filename))
	fileType, ok := allowedExtensions[extension]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "unsupported file type: "+extension)
		return
	}

	temporary, err := os.CreateTemp("", "open-ima-upload-*")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hasher), file); err != nil {
		_ = temporary.Close()
		httpx.Error(w, http.StatusBadRequest, "file too large or read failed")
		return
	}
	if err := temporary.Close(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := hex.EncodeToString(hasher.Sum(nil))
	storedFile, err := os.Open(temporaryPath)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer storedFile.Close()
	if err := h.store.Put(r.Context(), key, storedFile); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	title := strings.TrimSuffix(filepath.Base(header.Filename), extension)
	documentID, duplicate, err := h.media.CreateDocument(
		r.Context(), r.PathValue("id"), title, "file", key, fileType, key,
	)
	if errors.Is(err, media.ErrKnowledgeBaseNotFound) {
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"document_id": documentID, "duplicate": duplicate})
}
