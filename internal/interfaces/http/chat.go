package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"open-ima/internal/application/chat"
	kbdom "open-ima/internal/domain/knowledgebase"
)

type chatHandler struct{ service *chat.Service }

func (h *chatHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kbs/{id}/search", h.handleSearch)
	mux.HandleFunc("POST /api/kbs/{id}/chat", h.handleChat)
	mux.HandleFunc("GET /api/kbs/{id}/conversations", h.handleConversations)
	mux.HandleFunc("GET /api/conversations/{id}/messages", h.handleMessages)
}

func (h *chatHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	results, err := h.service.Search(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("mode"))
	if errors.Is(err, kbdom.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *chatHandler) handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ConversationID string `json:"conversation_id"`
		Query          string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	writeEvent := func(event string, data any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	conversationID, citations, err := h.service.Chat(r.Context(), r.PathValue("id"), request.ConversationID, request.Query, func(token string) error {
		return writeEvent("token", map[string]string{"token": token})
	})
	if err != nil {
		_ = writeEvent("error", map[string]string{"error": err.Error(), "conversation_id": conversationID})
		return
	}
	if err := writeEvent("citations", citations); err != nil {
		return
	}
	_ = writeEvent("done", map[string]string{"conversation_id": conversationID})
}

func (h *chatHandler) handleConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := h.service.ListConversations(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (h *chatHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := h.service.ListMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messages)
}
