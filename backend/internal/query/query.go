// Package query is the question-answering domain: retrieval, generation and
// conversation history. Retrieval ownership (recorded decision): Go embeds the
// question and searches pgvector itself; the Python service stays stateless
// and only generates from the chunks Go hands it.
package query

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmptyQuestion        = errors.New("question must not be empty")
	ErrConversationNotFound = errors.New("conversation not found")
)

type Conversation struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

type Message struct {
	ID             string
	ConversationID string
	Role           string // "user" | "assistant"
	Content        string
	Citations      []Citation
	CreatedAt      time.Time
}

// Citation is display-ready: everything the frontend needs to show a source.
type Citation struct {
	ChunkID    string `json:"chunk_id"`
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	Snippet    string `json:"snippet"`
}

// RetrievedChunk is a chunk returned by similarity search, joined with its
// document for display.
type RetrievedChunk struct {
	ChunkID    string
	DocumentID string
	Filename   string
	Index      int
	Text       string
}

// Answer is what the LLM produces: text plus the ids of the chunks it used.
type Answer struct {
	Text     string
	ChunkIDs []string
}

// Repository persists conversations and messages. GetConversation is scoped
// by owner — accessing another user's conversation looks identical to it not
// existing. Implemented by platform/postgres.
type Repository interface {
	CreateConversation(ctx context.Context, userID string) (Conversation, error)
	GetConversation(ctx context.Context, userID, id string) (Conversation, error)
	AppendMessage(ctx context.Context, msg *Message) error
	ListMessages(ctx context.Context, conversationID string) ([]Message, error)
}

// EmbeddingService turns the question into a vector. Implemented by platform/aiclient.
type EmbeddingService interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// VectorStore finds the chunks most similar to an embedding, restricted to
// the user's own ready documents — retrieval is where cross-user leaks would
// happen, so the scope lives in the port itself. Implemented by
// platform/postgres.
type VectorStore interface {
	Search(ctx context.Context, userID string, embedding []float32, limit int) ([]RetrievedChunk, error)
}

// LLMService generates a grounded answer from the retrieved chunks.
// Implemented by platform/aiclient (calls the Python service).
type LLMService interface {
	Generate(ctx context.Context, question string, chunks []RetrievedChunk) (Answer, error)
}
