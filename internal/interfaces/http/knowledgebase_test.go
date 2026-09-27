package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKnowledgeBaseHandlers(t *testing.T) {
	mux := newTestServices(t).router()
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs", strings.NewReader(`{"name":"API库","description":"d"}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &created)
	knowledgeBaseID := created["id"].(string)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/kbs", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "API库") {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/kbs/"+knowledgeBaseID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", recorder.Code)
	}
}

func TestKnowledgeBaseCreateValidatesName(t *testing.T) {
	mux := newTestServices(t).router()
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs", strings.NewReader(`{"name":" "}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestKnowledgeBaseDeleteMissing(t *testing.T) {
	mux := newTestServices(t).router()
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/kbs/missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d", recorder.Code)
	}
}
