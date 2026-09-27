package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"open-ima/internal/application/chat"
	"open-ima/internal/application/copilot"
	kbdom "open-ima/internal/domain/knowledgebase"
)

// toolResultDisplayRunes 是 tool_result 事件 output 的展示截断长度(全文已在注册表限长)。
const toolResultDisplayRunes = 2000

type chatHandler struct {
	chat    *chat.Service
	copilot *copilot.Service
}

func (h *chatHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kbs/{id}/search", h.handleSearch)
	mux.HandleFunc("POST /api/kbs/{id}/chat", h.handleChat)
	mux.HandleFunc("GET /api/kbs/{id}/conversations", h.handleConversations)
	mux.HandleFunc("GET /api/conversations/{id}/messages", h.handleMessages)
}

func (h *chatHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	results, err := h.chat.Search(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("mode"))
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

// handleChat 是统一对话端点:快问/agent 共用同一 SSE 事件契约
// (thought / tool_call / tool_result / references / token / done / error)。
func (h *chatHandler) handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ConversationBizID string `json:"conversation_biz_id"`
		ModelBizID        string `json:"model_biz_id"`
		Mode              string `json:"mode"`
		Query             string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	if request.Mode != "" && request.Mode != "quick" && request.Mode != "agent" {
		writeError(w, http.StatusBadRequest, "mode must be quick or agent")
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
	emit := func(event copilot.Event) error {
		switch event.Type {
		case copilot.EventThought:
			return writeEvent("thought", map[string]any{"round": event.Round, "content": event.Content})
		case copilot.EventToolCall:
			return writeEvent("tool_call", map[string]any{
				"round": event.Round, "id": event.CallID, "name": event.ToolName, "args": event.Args,
			})
		case copilot.EventToolResult:
			return writeEvent("tool_result", map[string]any{
				"round": event.Round, "id": event.CallID, "name": event.ToolName,
				"success": event.Success, "output": truncateRunes(event.Output, toolResultDisplayRunes),
				"duration_ms": event.DurationMs,
			})
		case copilot.EventReferences:
			return writeEvent("references", map[string]any{"items": event.References})
		case copilot.EventToken:
			return writeEvent("token", map[string]string{"token": event.Content})
		}
		return nil
	}
	conversationBizID, result, err := h.copilot.Chat(
		r.Context(), r.PathValue("id"), request.ConversationBizID, request.ModelBizID, request.Mode, request.Query, emit)
	if err != nil {
		_ = writeEvent("error", map[string]string{"error": err.Error(), "conversation_biz_id": conversationBizID})
		return
	}
	_ = writeEvent("done", map[string]any{
		"conversation_biz_id": conversationBizID,
		"rounds":              result.Rounds,
		"truncated":           result.Truncated,
		"citations":           result.Citations,
		"mode":                result.Mode,
		"degraded":            result.Degraded,
	})
}

func (h *chatHandler) handleConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := h.copilot.ListConversations(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (h *chatHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := h.copilot.ListMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}
