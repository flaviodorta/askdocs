package aiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"askdocs/backend/internal/query"
)

func retrieved() []query.RetrievedChunk {
	return []query.RetrievedChunk{
		{ChunkID: "c1", DocumentID: "d1", Filename: "f.pdf", Text: "trecho um"},
		{ChunkID: "c2", DocumentID: "d1", Filename: "f.pdf", Text: "trecho dois"},
	}
}

func TestGenerateSendsChunksAndReturnsAnswer(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate" {
			t.Errorf("path = %s, want /generate", r.URL.Path)
		}
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Question != "qual o prazo?" || len(req.Chunks) != 2 || req.Chunks[0].ID != "c1" {
			t.Errorf("request = %+v, want question and both chunks", req)
		}
		json.NewEncoder(w).Encode(generateResponse{
			Answer:    "O prazo é 30 dias.",
			Citations: []generateCitation{{ChunkID: "c1", DocumentID: "d1"}},
		})
	})

	got, err := client.Generate(context.Background(), "qual o prazo?", retrieved())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Text != "O prazo é 30 dias." {
		t.Errorf("answer = %q", got.Text)
	}
	if len(got.ChunkIDs) != 1 || got.ChunkIDs[0] != "c1" {
		t.Errorf("cited chunks = %v, want [c1]", got.ChunkIDs)
	}
}

func TestGenerateSurfacesServiceError(t *testing.T) {
	client := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"llm provider error"}`, http.StatusBadGateway)
	})

	_, err := client.Generate(context.Background(), "pergunta?", retrieved())
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "llm provider error") {
		t.Fatalf("Generate() error = %v, want status and detail surfaced", err)
	}
}
