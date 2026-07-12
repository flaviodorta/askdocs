package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"askdocs/backend/internal/query"
)

// QueryRepository implements query.Repository on Postgres.
type QueryRepository struct {
	pool *pgxpool.Pool
}

func NewQueryRepository(pool *pgxpool.Pool) *QueryRepository {
	return &QueryRepository{pool: pool}
}

func (r *QueryRepository) CreateConversation(ctx context.Context, userID string) (query.Conversation, error) {
	var conv query.Conversation
	err := r.pool.QueryRow(ctx,
		`INSERT INTO conversations (user_id) VALUES ($1) RETURNING id, user_id, created_at`,
		userID,
	).Scan(&conv.ID, &conv.UserID, &conv.CreatedAt)
	if err != nil {
		return query.Conversation{}, fmt.Errorf("insert conversation: %w", err)
	}
	return conv, nil
}

func (r *QueryRepository) GetConversation(ctx context.Context, userID, id string) (query.Conversation, error) {
	var conv query.Conversation
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, created_at FROM conversations WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&conv.ID, &conv.UserID, &conv.CreatedAt)
	if err != nil {
		if notFound(err) {
			return query.Conversation{}, query.ErrConversationNotFound
		}
		return query.Conversation{}, fmt.Errorf("select conversation: %w", err)
	}
	return conv, nil
}

func (r *QueryRepository) AppendMessage(ctx context.Context, msg *query.Message) error {
	citations := msg.Citations
	if citations == nil {
		citations = []query.Citation{}
	}
	encoded, err := json.Marshal(citations)
	if err != nil {
		return fmt.Errorf("encode citations: %w", err)
	}

	err = r.pool.QueryRow(ctx,
		`INSERT INTO messages (conversation_id, role, content, citations)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		msg.ConversationID, msg.Role, msg.Content, encoded,
	).Scan(&msg.ID, &msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (r *QueryRepository) ListMessages(ctx context.Context, conversationID string) ([]query.Message, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, conversation_id, role, content, citations, created_at
		 FROM messages
		 WHERE conversation_id = $1
		 ORDER BY created_at`,
		conversationID)
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}
	defer rows.Close()

	messages := []query.Message{}
	for rows.Next() {
		var msg query.Message
		var citations []byte
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &citations, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if err := json.Unmarshal(citations, &msg.Citations); err != nil {
			return nil, fmt.Errorf("decode citations: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}
