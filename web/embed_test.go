package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAndSPAFallback(t *testing.T) {
	handler := Handler()
	for _, requestPath := range []string{"/", "/knowledge/kb1"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("path=%s code=%d body=%s", requestPath, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerDoesNotMaskAPIOrMissingAssets(t *testing.T) {
	handler := Handler()
	for _, requestPath := range []string{"/api/missing", "/internal/missing", "/assets/missing.js"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%s code=%d", requestPath, recorder.Code)
		}
	}
}
