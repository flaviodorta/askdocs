package query

import (
	"context"
	"fmt"
	"strings"
)

// DefaultTopK is how many chunks are retrieved per question.
const DefaultTopK = 5

// NoContextAnswer is returned without calling the LLM when retrieval finds
// nothing (no ready documents yet).
const NoContextAnswer = "I couldn't find anything in your documents to answer that. Upload a document and wait for it to be processed, then try again."

const snippetMaxRunes = 200

type Service struct {
	repo     Repository
	embedder EmbeddingService
	vectors  VectorStore
	llm      LLMService
	topK     int
}

func NewService(repo Repository, embedder EmbeddingService, vectors VectorStore, llm LLMService) *Service {
	return &Service{repo: repo, embedder: embedder, vectors: vectors, llm: llm, topK: DefaultTopK}
}

type AskResult struct {
	ConversationID string
	Answer         Message
}

// Ask runs the RAG flow: embed question → retrieve top-k chunks (the user's
// own only) → generate → persist both messages. An empty conversationID
// starts a new conversation.
func (s *Service) Ask(ctx context.Context, userID, conversationID, question string) (AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return AskResult{}, ErrEmptyQuestion
	}

	conv, err := s.conversation(ctx, userID, conversationID)
	if err != nil {
		return AskResult{}, err
	}

	embeddings, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return AskResult{}, fmt.Errorf("embed question: %w", err)
	}
	if len(embeddings) != 1 {
		return AskResult{}, fmt.Errorf("embed question: got %d embeddings, want 1", len(embeddings))
	}

	chunks, err := s.vectors.Search(ctx, userID, embeddings[0], s.topK)
	if err != nil {
		return AskResult{}, fmt.Errorf("search chunks: %w", err)
	}

	userMsg := Message{ConversationID: conv.ID, Role: "user", Content: question}
	if err := s.repo.AppendMessage(ctx, &userMsg); err != nil {
		return AskResult{}, fmt.Errorf("save question: %w", err)
	}

	assistant := Message{ConversationID: conv.ID, Role: "assistant"}
	if len(chunks) == 0 {
		assistant.Content = NoContextAnswer
	} else {
		answer, err := s.llm.Generate(ctx, question, chunks)
		if err != nil {
			return AskResult{}, fmt.Errorf("generate answer: %w", err)
		}
		assistant.Content = answer.Text
		assistant.Citations = buildCitations(answer.ChunkIDs, chunks)
	}

	if err := s.repo.AppendMessage(ctx, &assistant); err != nil {
		return AskResult{}, fmt.Errorf("save answer: %w", err)
	}
	return AskResult{ConversationID: conv.ID, Answer: assistant}, nil
}

// Messages returns a conversation's history, oldest first. The scoped
// GetConversation is the ownership check.
func (s *Service) Messages(ctx context.Context, userID, conversationID string) ([]Message, error) {
	if _, err := s.repo.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, conversationID)
}

func (s *Service) conversation(ctx context.Context, userID, id string) (Conversation, error) {
	if id == "" {
		conv, err := s.repo.CreateConversation(ctx, userID)
		if err != nil {
			return Conversation{}, fmt.Errorf("create conversation: %w", err)
		}
		return conv, nil
	}
	return s.repo.GetConversation(ctx, userID, id)
}

// buildCitations keeps only cited ids that were actually retrieved (an LLM
// can hallucinate ids — those are dropped), deduped, in retrieval order.
func buildCitations(citedIDs []string, chunks []RetrievedChunk) []Citation {
	cited := make(map[string]bool, len(citedIDs))
	for _, id := range citedIDs {
		cited[id] = true
	}

	var out []Citation
	for _, chunk := range chunks {
		if !cited[chunk.ChunkID] {
			continue
		}
		out = append(out, Citation{
			ChunkID:    chunk.ChunkID,
			DocumentID: chunk.DocumentID,
			Filename:   chunk.Filename,
			Snippet:    snippet(chunk.Text),
		})
	}
	return out
}

func snippet(text string) string {
	runes := []rune(text)
	if len(runes) <= snippetMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:snippetMaxRunes])) + "…"
}
