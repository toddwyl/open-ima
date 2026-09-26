package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedBatchesAndSortsByIndex(t *testing.T) {
	var batches []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Model != "test-emb" {
			t.Errorf("model = %q", request.Model)
		}
		batches = append(batches, len(request.Input))
		data := make([]map[string]any, len(request.Input))
		for index := range request.Input {
			data[index] = map[string]any{
				"index": index, "embedding": []float64{float64(index), float64(len(request.Input))},
			}
		}
		if len(data) == 2 {
			data[0], data[1] = data[1], data[0]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL, "k", "test-emb")
	client.BatchSize = 2
	vectors, err := client.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(batches) != "[2 1]" {
		t.Fatalf("batches = %v", batches)
	}
	if len(vectors) != 3 || vectors[0][0] != 0 || vectors[1][0] != 1 || vectors[2][0] != 0 {
		t.Fatalf("vectors = %v", vectors)
	}
	if vectors[0][1] != 2 || vectors[2][1] != 1 {
		t.Fatalf("batch boundary wrong: %v", vectors)
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	client := NewEmbeddingClient("http://unused", "k", "m")
	vectors, err := client.Embed(context.Background(), nil)
	if err != nil || len(vectors) != 0 {
		t.Fatalf("vectors=%v err=%v", vectors, err)
	}
}

func TestEmbedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := NewEmbeddingClient(server.URL, "k", "m").Embed(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmbedRejectsDuplicateIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`))
	}))
	defer server.Close()
	_, err := NewEmbeddingClient(server.URL, "", "m").Embed(context.Background(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v", err)
	}
}
