package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestErrorShape(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, 400, "bad input")
	if recorder.Code != 400 {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Body.String(); got != "{\"error\":\"bad input\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
