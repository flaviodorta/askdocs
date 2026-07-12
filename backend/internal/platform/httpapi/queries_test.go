package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"askdocs/backend/internal/document"
	"askdocs/backend/internal/query"
)

// In-memory implementations of the query ports.

type memQueryRepo struct {
	conversations map[string]query.Conversation
	messages      map[string][]query.Message
	nextID        int
}

func newMemQueryRepo() *memQueryRepo {
	return &memQueryRepo{conversations: map[string]query.Conversation{}, messages: map[string][]query.Message{}}
}

func (r *memQueryRepo) CreateConversation(_ context.Context) (query.Conversation, error) {
	r.nextID++
	conv := query.Conversation{ID: fmt.Sprintf("conv-%d", r.nextID), CreatedAt: time.Now()}
	r.conversations[conv.ID] = conv
	return conv, nil
}

func (r *memQueryRepo) GetConversation(_ context.Context, id string) (query.Conversation, error) {
	conv, ok := r.conversations[id]
	if !ok {
		return query.Conversation{}, query.ErrConversationNotFound
	}
	return conv, nil
}

func (r *memQueryRepo) AppendMessage(_ context.Context, msg *query.Message) error {
	r.nextID++
	msg.ID = fmt.Sprintf("msg-%d", r.nextID)
	msg.CreatedAt = time.Now()
	r.messages[msg.ConversationID] = append(r.messages[msg.ConversationID], *msg)
	return nil
}

func (r *memQueryRepo) ListMessages(_ context.Context, conversationID string) ([]query.Message, error) {
	return r.messages[conversationID], nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1}
	}
	return out, nil
}

type stubVectorStore struct{ chunks []query.RetrievedChunk }

func (s stubVectorStore) Search(_ context.Context, _ []float32, _ int) ([]query.RetrievedChunk, error) {
	return s.chunks, nil
}

type stubLLM struct{ answer query.Answer }

func (l *stubLLM) Generate(_ context.Context, _ string, _ []query.RetrievedChunk) (query.Answer, error) {
	return l.answer, nil
}

func newQueryTestServer(repo *memQueryRepo, chunks []query.RetrievedChunk, llm *stubLLM) http.Handler {
	docs := document.NewService(newMemRepo(), &memStore{})
	queries := query.NewService(repo, stubEmbedder{}, stubVectorStore{chunks: chunks}, llm)
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), pingFunc(func(context.Context) error { return nil }), docs, queries)
}

func TestAskReturnsAnswerWithCitations(t *testing.T) {
	repo := newMemQueryRepo()
	chunks := []query.RetrievedChunk{{ChunkID: "c1", DocumentID: "d1", Filename: "contrato.pdf", Text: "prazo de 30 dias"}}
	llm := &stubLLM{answer: query.Answer{Text: "O prazo é de 30 dias.", ChunkIDs: []string{"c1"}}}
	srv := newQueryTestServer(repo, chunks, llm)

	body := strings.NewReader(`{"question": "Qual o prazo?"}`)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/queries", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp askResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ConversationID == "" || resp.Answer != "O prazo é de 30 dias." {
		t.Errorf("response = %+v", resp)
	}
	if len(resp.Citations) != 1 || resp.Citations[0].Filename != "contrato.pdf" {
		t.Errorf("citations = %+v, want contrato.pdf source", resp.Citations)
	}
}

func TestAskEmptyQuestionIs400(t *testing.T) {
	srv := newQueryTestServer(newMemQueryRepo(), nil, &stubLLM{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/queries", strings.NewReader(`{"question": "  "}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAskInvalidJSONIs400(t *testing.T) {
	srv := newQueryTestServer(newMemQueryRepo(), nil, &stubLLM{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/queries", strings.NewReader(`not json`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAskUnknownConversationIs404(t *testing.T) {
	srv := newQueryTestServer(newMemQueryRepo(), nil, &stubLLM{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/queries",
		strings.NewReader(`{"question": "oi?", "conversation_id": "nope"}`)))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetConversationReturnsHistory(t *testing.T) {
	repo := newMemQueryRepo()
	chunks := []query.RetrievedChunk{{ChunkID: "c1", DocumentID: "d1", Filename: "f.pdf", Text: "t"}}
	llm := &stubLLM{answer: query.Answer{Text: "resposta", ChunkIDs: []string{"c1"}}}
	srv := newQueryTestServer(repo, chunks, llm)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/queries", strings.NewReader(`{"question": "pergunta?"}`)))
	var asked askResponse
	json.Unmarshal(rec.Body.Bytes(), &asked)

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conversations/"+asked.ConversationID, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var conv conversationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &conv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conv.Messages) != 2 || conv.Messages[0].Role != "user" || conv.Messages[1].Role != "assistant" {
		t.Errorf("messages = %+v, want user+assistant", conv.Messages)
	}
	if conv.Messages[0].Citations == nil {
		t.Error("citations must be [] in JSON, never null")
	}
}

func TestGetConversationUnknownIs404(t *testing.T) {
	srv := newQueryTestServer(newMemQueryRepo(), nil, &stubLLM{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conversations/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
