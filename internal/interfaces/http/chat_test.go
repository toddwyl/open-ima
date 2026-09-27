package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatSSEAndHistory(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{service: services.chat}).register(mux)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":"original query"}`))
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, event := range []string{"event: token", "event: citations", "event: done", "Answer ", "[1]"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in %s", event, body)
		}
	}
	if strings.Index(body, "event: token") > strings.Index(body, "event: citations") {
		t.Fatalf("citations arrived before tokens: %s", body)
	}
	var conversationID string
	for _, block := range strings.Split(body, "\n\n") {
		if strings.HasPrefix(block, "event: done") {
			var done map[string]string
			_ = json.Unmarshal([]byte(strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")), &done)
			conversationID = done["conversation_id"]
		}
	}
	if conversationID == "" {
		t.Fatal("missing conversation id")
	}
	messages, err := services.chat.ListMessages(context.Background(), conversationID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Content != "Answer [1]" || len(messages[1].Citations) != 3 {
		t.Fatalf("messages=%+v", messages)
	}
	if messages[1].Citations[0].ChunkID != "c2" {
		t.Fatalf("RRF order = %+v", messages[1].Citations)
	}
	services.mu.Lock()
	defer services.mu.Unlock()
	if len(services.searchQueries) < 2 || services.searchQueries[len(services.searchQueries)-2] != "original query" || services.searchQueries[len(services.searchQueries)-1] != "expanded query" {
		t.Fatalf("queries = %v", services.searchQueries)
	}
}

func TestChatRejectsEmptyQuery(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{service: services.chat}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":" "}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestSearchRejectsUnknownKnowledgeBase(t *testing.T) {
	services := newTestServices(t)
	mux := http.NewServeMux()
	(&chatHandler{service: services.chat}).register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/kbs/missing/search?q=alpha", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
