package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeRepo struct {
	conversations map[string]Conversation
	messages      map[string][]Message
	nextID        int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{conversations: map[string]Conversation{}, messages: map[string][]Message{}}
}

func (r *fakeRepo) CreateConversation(_ context.Context, userID string) (Conversation, error) {
	r.nextID++
	conv := Conversation{ID: fmt.Sprintf("conv-%d", r.nextID), UserID: userID}
	r.conversations[conv.ID] = conv
	return conv, nil
}

func (r *fakeRepo) GetConversation(_ context.Context, userID, id string) (Conversation, error) {
	conv, ok := r.conversations[id]
	if !ok || conv.UserID != userID {
		return Conversation{}, ErrConversationNotFound
	}
	return conv, nil
}

func (r *fakeRepo) AppendMessage(_ context.Context, msg *Message) error {
	r.nextID++
	msg.ID = fmt.Sprintf("msg-%d", r.nextID)
	r.messages[msg.ConversationID] = append(r.messages[msg.ConversationID], *msg)
	return nil
}

func (r *fakeRepo) ListMessages(_ context.Context, conversationID string) ([]Message, error) {
	return r.messages[conversationID], nil
}

type fakeEmbedder struct{ err error }

func (e fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

type fakeVectorStore struct {
	chunks []RetrievedChunk
	err    error
}

func (s fakeVectorStore) Search(_ context.Context, _ string, _ []float32, _ int) ([]RetrievedChunk, error) {
	return s.chunks, s.err
}

type fakeLLM struct {
	answer Answer
	err    error
	called bool
}

func (l *fakeLLM) Generate(_ context.Context, _ string, _ []RetrievedChunk) (Answer, error) {
	l.called = true
	return l.answer, l.err
}

func twoChunks() []RetrievedChunk {
	return []RetrievedChunk{
		{ChunkID: "chunk-1", DocumentID: "doc-1", Filename: "contrato.pdf", Index: 0, Text: "O contrato prevê rescisão em 30 dias."},
		{ChunkID: "chunk-2", DocumentID: "doc-1", Filename: "contrato.pdf", Index: 1, Text: strings.Repeat("texto longo ", 50)},
	}
}

func TestAskHappyPath(t *testing.T) {
	repo := newFakeRepo()
	llm := &fakeLLM{answer: Answer{Text: "A rescisão é em 30 dias.", ChunkIDs: []string{"chunk-1"}}}
	svc := NewService(repo, fakeEmbedder{}, fakeVectorStore{chunks: twoChunks()}, llm)

	got, err := svc.Ask(context.Background(), "u1", "", "Qual o prazo de rescisão?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got.ConversationID == "" {
		t.Error("no conversation created")
	}
	if got.Answer.Content != "A rescisão é em 30 dias." {
		t.Errorf("answer = %q", got.Answer.Content)
	}
	if len(got.Answer.Citations) != 1 || got.Answer.Citations[0].ChunkID != "chunk-1" {
		t.Fatalf("citations = %+v, want chunk-1", got.Answer.Citations)
	}
	if got.Answer.Citations[0].Filename != "contrato.pdf" || got.Answer.Citations[0].Snippet == "" {
		t.Errorf("citation not display-ready: %+v", got.Answer.Citations[0])
	}

	msgs := repo.messages[got.ConversationID]
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Errorf("persisted messages = %+v, want user+assistant pair", msgs)
	}
}

func TestAskDropsHallucinatedCitations(t *testing.T) {
	repo := newFakeRepo()
	llm := &fakeLLM{answer: Answer{Text: "resposta", ChunkIDs: []string{"chunk-2", "chunk-999", "chunk-2"}}}
	svc := NewService(repo, fakeEmbedder{}, fakeVectorStore{chunks: twoChunks()}, llm)

	got, err := svc.Ask(context.Background(), "u1", "", "pergunta?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(got.Answer.Citations) != 1 || got.Answer.Citations[0].ChunkID != "chunk-2" {
		t.Fatalf("citations = %+v, want only the real deduped chunk-2", got.Answer.Citations)
	}
	if !strings.HasSuffix(got.Answer.Citations[0].Snippet, "…") {
		t.Errorf("long chunk snippet not truncated: %q", got.Answer.Citations[0].Snippet)
	}
}

func TestAskWithoutChunksSkipsLLM(t *testing.T) {
	repo := newFakeRepo()
	llm := &fakeLLM{}
	svc := NewService(repo, fakeEmbedder{}, fakeVectorStore{}, llm)

	got, err := svc.Ask(context.Background(), "u1", "", "pergunta sem documentos?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if llm.called {
		t.Error("LLM was called with zero retrieved chunks")
	}
	if got.Answer.Content != NoContextAnswer || len(got.Answer.Citations) != 0 {
		t.Errorf("answer = %+v, want canned no-context answer", got.Answer)
	}
}

func TestAskContinuesExistingConversation(t *testing.T) {
	repo := newFakeRepo()
	conv, _ := repo.CreateConversation(context.Background(), "u1")
	llm := &fakeLLM{answer: Answer{Text: "ok"}}
	svc := NewService(repo, fakeEmbedder{}, fakeVectorStore{chunks: twoChunks()}, llm)

	got, err := svc.Ask(context.Background(), "u1", conv.ID, "pergunta?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got.ConversationID != conv.ID {
		t.Errorf("conversation = %q, want %q", got.ConversationID, conv.ID)
	}
}

func TestAskUnknownConversationIs404(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeEmbedder{}, fakeVectorStore{}, &fakeLLM{})

	if _, err := svc.Ask(context.Background(), "u1", "nope", "pergunta?"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("Ask() error = %v, want ErrConversationNotFound", err)
	}
}

func TestAskEmptyQuestionRejected(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeEmbedder{}, fakeVectorStore{}, &fakeLLM{})

	if _, err := svc.Ask(context.Background(), "u1", "", "   "); !errors.Is(err, ErrEmptyQuestion) {
		t.Fatalf("Ask() error = %v, want ErrEmptyQuestion", err)
	}
}

func TestAskLLMFailureSurfaces(t *testing.T) {
	llm := &fakeLLM{err: errors.New("ai down")}
	svc := NewService(newFakeRepo(), fakeEmbedder{}, fakeVectorStore{chunks: twoChunks()}, llm)

	if _, err := svc.Ask(context.Background(), "u1", "", "pergunta?"); err == nil || !strings.Contains(err.Error(), "generate answer") {
		t.Fatalf("Ask() error = %v, want generate failure", err)
	}
}

func TestConversationsAreScopedPerUser(t *testing.T) {
	repo := newFakeRepo()
	conv, _ := repo.CreateConversation(context.Background(), "user-a")
	svc := NewService(repo, fakeEmbedder{}, fakeVectorStore{chunks: twoChunks()}, &fakeLLM{answer: Answer{Text: "ok"}})

	if _, err := svc.Ask(context.Background(), "user-b", conv.ID, "pergunta?"); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("cross-user Ask: error = %v, want ErrConversationNotFound", err)
	}
	if _, err := svc.Messages(context.Background(), "user-b", conv.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("cross-user Messages: error = %v, want ErrConversationNotFound", err)
	}
}

func TestMessagesUnknownConversation(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeEmbedder{}, fakeVectorStore{}, &fakeLLM{})

	if _, err := svc.Messages(context.Background(), "u1", "nope"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("Messages() error = %v, want ErrConversationNotFound", err)
	}
}
