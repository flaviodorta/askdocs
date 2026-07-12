package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"askdocs/backend/internal/query"
)

// In-memory implementations of the query ports, scoped per user like the
// real Postgres repository.

type memQueryRepo struct {
	conversations map[string]query.Conversation
	messages      map[string][]query.Message
	nextID        int
}

func newMemQueryRepo() *memQueryRepo {
	return &memQueryRepo{conversations: map[string]query.Conversation{}, messages: map[string][]query.Message{}}
}

func (r *memQueryRepo) CreateConversation(_ context.Context, userID string) (query.Conversation, error) {
	r.nextID++
	conv := query.Conversation{ID: fmt.Sprintf("conv-%d", r.nextID), UserID: userID, CreatedAt: time.Now()}
	r.conversations[conv.ID] = conv
	return conv, nil
}

func (r *memQueryRepo) GetConversation(_ context.Context, userID, id string) (query.Conversation, error) {
	conv, ok := r.conversations[id]
	if !ok || conv.UserID != userID {
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

func (s *stubVectorStore) Search(_ context.Context, _ string, _ []float32, _ int) ([]query.RetrievedChunk, error) {
	return s.chunks, nil
}

type stubLLM struct{ answer query.Answer }

func (l *stubLLM) Generate(_ context.Context, _ string, _ []query.RetrievedChunk) (query.Answer, error) {
	return l.answer, nil
}

func askJSON(question, conversationID string) string {
	if conversationID == "" {
		return fmt.Sprintf(`{"question": %q}`, question)
	}
	return fmt.Sprintf(`{"question": %q, "conversation_id": %q}`, question, conversationID)
}

func TestAskReturnsAnswerWithCitations(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")
	env.vector.chunks = []query.RetrievedChunk{{ChunkID: "c1", DocumentID: "d1", Filename: "contrato.pdf", Text: "prazo de 30 dias"}}
	env.llm.answer = query.Answer{Text: "O prazo é de 30 dias.", ChunkIDs: []string{"c1"}}

	rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("Qual o prazo?", "")), "application/json", cookie)

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
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("  ", "")), "application/json", cookie)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAskInvalidJSONIs400(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodPost, "/queries", strings.NewReader("not json"), "application/json", cookie)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAskUnknownConversationIs404(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("oi?", "nope")), "application/json", cookie)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetConversationReturnsHistory(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")
	env.vector.chunks = []query.RetrievedChunk{{ChunkID: "c1", DocumentID: "d1", Filename: "f.pdf", Text: "t"}}
	env.llm.answer = query.Answer{Text: "resposta", ChunkIDs: []string{"c1"}}

	rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("pergunta?", "")), "application/json", cookie)
	var asked askResponse
	json.Unmarshal(rec.Body.Bytes(), &asked)

	rec = env.do(http.MethodGet, "/conversations/"+asked.ConversationID, nil, "", cookie)

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

func TestConversationsAreScopedPerUser(t *testing.T) {
	env := okEnv(t)
	alice := env.register("alice@example.com")
	bob := env.register("bob@example.com")
	env.llm.answer = query.Answer{Text: "resposta"}
	env.vector.chunks = []query.RetrievedChunk{{ChunkID: "c1", DocumentID: "d1", Filename: "f.pdf", Text: "t"}}

	rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("pergunta?", "")), "application/json", alice)
	var asked askResponse
	json.Unmarshal(rec.Body.Bytes(), &asked)

	// Bob cannot read or continue Alice's conversation.
	if rec := env.do(http.MethodGet, "/conversations/"+asked.ConversationID, nil, "", bob); rec.Code != http.StatusNotFound {
		t.Errorf("bob get alice's conversation: status = %d, want 404", rec.Code)
	}
	if rec := env.do(http.MethodPost, "/queries", strings.NewReader(askJSON("e aí?", asked.ConversationID)), "application/json", bob); rec.Code != http.StatusNotFound {
		t.Errorf("bob continue alice's conversation: status = %d, want 404", rec.Code)
	}
}

func TestGetConversationUnknownIs404(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	rec := env.do(http.MethodGet, "/conversations/nope", nil, "", cookie)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
