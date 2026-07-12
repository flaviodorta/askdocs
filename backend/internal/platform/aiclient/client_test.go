package aiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func embedServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestEmbedReturnsOneVectorPerText(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" || r.Method != http.MethodPost {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{Model: "m", Dim: 3}
		for range req.Texts {
			resp.Embeddings = append(resp.Embeddings, []float32{1, 2, 3})
		}
		json.NewEncoder(w).Encode(resp)
	})

	got, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(got) != 2 || len(got[0]) != 3 {
		t.Errorf("embeddings = %v, want 2 vectors of dim 3", got)
	}
}

func TestEmbedSplitsIntoBatches(t *testing.T) {
	var mu sync.Mutex
	var batchSizes []int
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		batchSizes = append(batchSizes, len(req.Texts))
		mu.Unlock()
		resp := embedResponse{}
		for range req.Texts {
			resp.Embeddings = append(resp.Embeddings, []float32{0})
		}
		json.NewEncoder(w).Encode(resp)
	})

	texts := make([]string, 300)
	for i := range texts {
		texts[i] = "t"
	}
	got, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(got) != 300 {
		t.Errorf("len = %d, want 300 vectors across batches", len(got))
	}
	want := []int{128, 128, 44}
	if len(batchSizes) != 3 || batchSizes[0] != want[0] || batchSizes[1] != want[1] || batchSizes[2] != want[2] {
		t.Errorf("batch sizes = %v, want %v", batchSizes, want)
	}
}

func TestEmbedSurfacesServiceError(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"model exploded"}`, http.StatusInternalServerError)
	})

	_, err := client.Embed(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("Embed() error = %v, want status and detail surfaced", err)
	}
}

func TestEmbedRejectsCountMismatch(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{1}}})
	})

	_, err := client.Embed(context.Background(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "1 embeddings for 2 texts") {
		t.Fatalf("Embed() error = %v, want count mismatch", err)
	}
}

func TestEmbedEmptyInputSkipsNetwork(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("network call for empty input")
	})

	got, err := client.Embed(context.Background(), nil)
	if err != nil || got != nil {
		t.Fatalf("Embed(nil) = %v, %v, want nil, nil", got, err)
	}
}
