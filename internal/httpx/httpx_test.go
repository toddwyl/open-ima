package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, 400, "bad input")
	if rec.Code != 400 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"error\":\"bad input\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
