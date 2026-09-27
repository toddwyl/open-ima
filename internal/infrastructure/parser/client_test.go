package parser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/parse" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["file_url"] != "http://x/f.md" || request["file_type"] != "md" {
			t.Errorf("request = %v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"doc","blocks":[{"type":"heading","text":"H","level":1},{"type":"paragraph","text":"P","level":0}]}`))
	}))
	defer server.Close()

	result, err := New(server.URL).Parse(context.Background(), "http://x/f.md", "md")
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "doc" || len(result.Blocks) != 2 || result.Blocks[0].Level != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestParse422IsFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"pdf: encrypted file is not supported"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).Parse(context.Background(), "http://x/f.pdf", "pdf")
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %T %v", err, err)
	}
	if fatal.Message != "pdf: encrypted file is not supported" {
		t.Fatalf("message = %q", fatal.Message)
	}
}

func TestParse502IsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"fetch failed"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).Parse(context.Background(), "http://x/f.pdf", "pdf")
	var fatal *FatalError
	if err == nil || errors.As(err, &fatal) {
		t.Fatalf("err = %T %v", err, err)
	}
}
