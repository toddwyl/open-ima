package rag

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"open-ima/internal/httpx"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kbs/{id}/search", s.handleSearch)
	mux.HandleFunc("POST /api/kbs/{id}/chat", s.handleChat)
	mux.HandleFunc("GET /api/kbs/{id}/conversations", s.handleConversations)
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.handleMessages)
}

func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) {
	results, err := s.Search(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("mode"))
	if errors.Is(err, ErrKnowledgeBaseNotFound) {
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, results)
}

func (s *Service) handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ConversationID string `json:"conversation_id"`
		Query          string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" {
		httpx.Error(w, http.StatusBadRequest, "query is required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "streaming is not supported")
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
	conversationID, citations, err := s.Chat(r.Context(), r.PathValue("id"), request.ConversationID, request.Query, func(token string) error {
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

func (s *Service) handleConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := s.ListConversations(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, conversations)
}

func (s *Service) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.ListMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, messages)
}
