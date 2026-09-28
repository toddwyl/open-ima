package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentsEndpointFiltersByMedia(t *testing.T) {
	handler := newHandler(&state{
		created: true,
		docs: []map[string]any{
			{"id": "c1", "media_biz_id": "m1", "kb_biz_id": "kb1"},
			{"id": "c2", "media_biz_id": "m2", "kb_biz_id": "kb1"},
		},
	})
	req := httptest.NewRequest(http.MethodGet,
		"/indexes/chunks/documents?filter=media_biz_id+%3D+%27m1%27&fields=id,media_biz_id,kb_biz_id&limit=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0]["id"] != "c1" {
		t.Fatalf("response = %+v", response.Results)
	}
}
