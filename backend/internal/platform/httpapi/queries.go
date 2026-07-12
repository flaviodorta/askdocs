package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"askdocs/backend/internal/query"
)

const maxQuestionBytes = 8 << 10 // 8 KiB is plenty for a question

type askRequest struct {
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id,omitempty"`
}

type messageResponse struct {
	ID        string           `json:"id"`
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Citations []query.Citation `json:"citations"`
	CreatedAt time.Time        `json:"created_at"`
}

type askResponse struct {
	ConversationID string           `json:"conversation_id"`
	Answer         string           `json:"answer"`
	Citations      []query.Citation `json:"citations"`
	MessageID      string           `json:"message_id"`
	CreatedAt      time.Time        `json:"created_at"`
}

type conversationResponse struct {
	ID       string            `json:"id"`
	Messages []messageResponse `json:"messages"`
}

func toMessageResponse(m query.Message) messageResponse {
	citations := m.Citations
	if citations == nil {
		citations = []query.Citation{}
	}
	return messageResponse{
		ID:        m.ID,
		Role:      m.Role,
		Content:   m.Content,
		Citations: citations,
		CreatedAt: m.CreatedAt,
	}
}

func (a *api) handleAsk() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxQuestionBytes)

		var req askRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		result, err := a.queries.Ask(r.Context(), req.ConversationID, req.Question)
		if err != nil {
			switch {
			case errors.Is(err, query.ErrEmptyQuestion):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, query.ErrConversationNotFound):
				writeError(w, http.StatusNotFound, "conversation not found")
			default:
				a.logger.Error("ask", "error", err)
				writeError(w, http.StatusBadGateway, "failed to answer the question — check that the AI service is running")
			}
			return
		}

		citations := result.Answer.Citations
		if citations == nil {
			citations = []query.Citation{}
		}
		writeJSON(w, http.StatusOK, askResponse{
			ConversationID: result.ConversationID,
			Answer:         result.Answer.Content,
			Citations:      citations,
			MessageID:      result.Answer.ID,
			CreatedAt:      result.Answer.CreatedAt,
		})
	}
}

func (a *api) handleGetConversation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		messages, err := a.queries.Messages(r.Context(), id)
		if err != nil {
			if errors.Is(err, query.ErrConversationNotFound) {
				writeError(w, http.StatusNotFound, "conversation not found")
				return
			}
			a.logger.Error("get conversation", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		out := conversationResponse{ID: id, Messages: make([]messageResponse, 0, len(messages))}
		for _, m := range messages {
			out.Messages = append(out.Messages, toMessageResponse(m))
		}
		writeJSON(w, http.StatusOK, out)
	}
}
