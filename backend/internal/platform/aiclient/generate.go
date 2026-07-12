package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"askdocs/backend/internal/query"
)

// The JSON bodies mirror ai-service's Pydantic schemas for /generate.

type generateRequest struct {
	Question string          `json:"question"`
	Chunks   []generateChunk `json:"chunks"`
}

type generateChunk struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Text       string `json:"text"`
}

type generateResponse struct {
	Answer    string             `json:"answer"`
	Citations []generateCitation `json:"citations"`
}

type generateCitation struct {
	ChunkID    string `json:"chunk_id"`
	DocumentID string `json:"document_id"`
}

// Generate implements query.LLMService against the Python /generate endpoint.
func (c *Client) Generate(ctx context.Context, question string, chunks []query.RetrievedChunk) (query.Answer, error) {
	reqChunks := make([]generateChunk, len(chunks))
	for i, chunk := range chunks {
		reqChunks[i] = generateChunk{ID: chunk.ChunkID, DocumentID: chunk.DocumentID, Text: chunk.Text}
	}

	body, err := json.Marshal(generateRequest{Question: question, Chunks: reqChunks})
	if err != nil {
		return query.Answer{}, fmt.Errorf("encode generate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/generate", bytes.NewReader(body))
	if err != nil {
		return query.Answer{}, fmt.Errorf("build generate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return query.Answer{}, fmt.Errorf("call ai service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		// The service's error contract is {"detail": "..."} with sanitized
		// messages; surface that as a typed error the handler can show.
		var errBody struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &errBody) == nil && errBody.Detail != "" {
			return query.Answer{}, fmt.Errorf("ai service /generate returned %d: %w",
				resp.StatusCode, &query.AIUnavailableError{Detail: errBody.Detail})
		}
		return query.Answer{}, fmt.Errorf("ai service /generate returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var decoded generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return query.Answer{}, fmt.Errorf("decode generate response: %w", err)
	}

	answer := query.Answer{Text: decoded.Answer, ChunkIDs: make([]string, 0, len(decoded.Citations))}
	for _, citation := range decoded.Citations {
		answer.ChunkIDs = append(answer.ChunkIDs, citation.ChunkID)
	}
	return answer, nil
}
