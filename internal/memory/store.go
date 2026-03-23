package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"myagent/internal/repo"
)

// Memory is a single remembered fact about a user.
type Memory struct {
	ID      int64
	UserID  int64
	Content string
	Source  string
}

// Store handles CRUD for the memories table.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save persists a new memory with its embedding vector.
func (s *Store) Save(ctx context.Context, userID int64, content, source string, embedding []float32) (int64, error) {
	embStr := repo.VectorLiteral(embedding)
	var embArg any
	if embStr != "" {
		embArg = embStr
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO memories (user_id, content, source, embedding)
		VALUES ($1, $2, $3, CASE WHEN $4::text IS NULL THEN NULL ELSE $4::vector END)
		RETURNING id`,
		userID, content, source, embArg,
	).Scan(&id)
	return id, err
}

// RetrieveRelevant returns the top-k memories most semantically similar to the query vector.
func (s *Store) RetrieveRelevant(ctx context.Context, userID int64, queryVec []float32, topK int) ([]*Memory, error) {
	if len(queryVec) == 0 {
		return s.RecentByUser(ctx, userID, topK)
	}
	embStr := repo.VectorLiteral(queryVec)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, user_id, content, source
		FROM memories
		WHERE user_id = $1
		ORDER BY embedding <=> $2::vector
		LIMIT %d`, topK),
		userID, embStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// RecentByUser returns the most recent memories for a user (fallback when no vector).
func (s *Store) RecentByUser(ctx context.Context, userID int64, limit int) ([]*Memory, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, user_id, content, source
		FROM memories
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT %d`, limit),
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// DeleteByUser removes all memories for a user (GDPR convenience).
func (s *Store) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE user_id = $1`, userID)
	return err
}

func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.UserID, &m.Content, &m.Source); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// FormatForPrompt converts a slice of memories into a compact string block
// suitable for injection into an LLM system prompt.
func FormatForPrompt(mems []*Memory) string {
	if len(mems) == 0 {
		return ""
	}
	lines := make([]string, len(mems))
	for i, m := range mems {
		lines[i] = "- " + m.Content
	}
	return "【用户历史偏好记忆】\n" + strings.Join(lines, "\n")
}
