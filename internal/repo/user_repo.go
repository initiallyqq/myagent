package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"myagent/internal/model"

	"github.com/lib/pq"
)

// UserRepo handles all PostgreSQL operations on the users table.
type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

// Upsert inserts or updates a user by openid. Returns the resolved user id.
func (r *UserRepo) Upsert(ctx context.Context, req *model.UserRegisterReq, embedding []float32) (int64, error) {
	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		return 0, fmt.Errorf("marshal tags: %w", err)
	}

	var embeddingArg any
	if emb := VectorLiteral(embedding); emb != "" {
		embeddingArg = emb
	}

	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO users
			(openid, nickname, gender, tags, destinations, budget_min, budget_max,
			 available_start, available_end, embedding)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,
			CASE WHEN $10::text IS NULL THEN NULL ELSE $10::vector END)
		ON CONFLICT (openid) DO UPDATE SET
			nickname        = EXCLUDED.nickname,
			gender          = EXCLUDED.gender,
			tags            = EXCLUDED.tags,
			destinations    = EXCLUDED.destinations,
			budget_min      = EXCLUDED.budget_min,
			budget_max      = EXCLUDED.budget_max,
			available_start = EXCLUDED.available_start,
			available_end   = EXCLUDED.available_end,
			embedding       = EXCLUDED.embedding,
			updated_at      = now()
		RETURNING id`,
		req.OpenID,
		req.Nickname,
		req.Gender,
		tagsJSON,
		pq.Array(req.Destinations),
		req.BudgetMin,
		req.BudgetMax,
		nullableTime(req.AvailableStart),
		nullableTime(req.AvailableEnd),
		embeddingArg,
	).Scan(&id)
	return id, err
}

// HybridSearch runs the core mixed scalar+vector query.
// strictMode=true applies budget and date filters; false relaxes them.
func (r *UserRepo) HybridSearch(ctx context.Context, q *model.SearchQuery, limit int) ([]*model.User, error) {
	conditions := []string{}
	args := []any{}
	idx := 1

	// vector param is always first
	var embArg any
	if emb := VectorLiteral(q.Embedding); emb != "" {
		embArg = emb
	}
	args = append(args, embArg) // $1
	idx++

	if q.Gender != "" {
		conditions = append(conditions, fmt.Sprintf("gender = $%d", idx))
		args = append(args, q.Gender)
		idx++
	}

	if q.Dest != "" {
		conditions = append(conditions, fmt.Sprintf("destinations @> ARRAY[$%d::text]", idx))
		args = append(args, q.Dest)
		idx++
	}

	if !q.Relaxed && q.Budget > 0 {
		conditions = append(conditions, fmt.Sprintf("budget_max >= $%d", idx))
		args = append(args, q.Budget)
		idx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, openid, nickname, gender, tags, destinations,
		       budget_min, budget_max, created_at, updated_at,
		       1 - (embedding <=> $1::vector) AS similarity
		FROM users
		%s
		ORDER BY embedding <=> $1::vector
		LIMIT %d`, where, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		u := &model.User{}
		var tagsJSON []byte
		var destinations pq.StringArray
		err := rows.Scan(
			&u.ID, &u.OpenID, &u.Nickname, &u.Gender,
			&tagsJSON, &destinations,
			&u.BudgetMin, &u.BudgetMax,
			&u.CreatedAt, &u.UpdatedAt,
			&u.Similarity,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(tagsJSON, &u.Tags)
		u.Destinations = []string(destinations)
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetByOpenID fetches a user by WeChat OpenID.
func (r *UserRepo) GetByOpenID(ctx context.Context, openid string) (*model.User, error) {
	u := &model.User{}
	var tagsJSON []byte
	var destinations pq.StringArray
	err := r.db.QueryRowContext(ctx, `
		SELECT id, openid, nickname, gender, tags, destinations,
		       budget_min, budget_max, created_at, updated_at
		FROM users WHERE openid = $1`, openid).Scan(
		&u.ID, &u.OpenID, &u.Nickname, &u.Gender,
		&tagsJSON, &destinations,
		&u.BudgetMin, &u.BudgetMax,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(tagsJSON, &u.Tags)
	u.Destinations = []string(destinations)
	return u, nil
}

// GetEmbeddingByID returns the embedding vector for a given user id.
func (r *UserRepo) GetEmbeddingByID(ctx context.Context, userID int64) ([]float32, error) {
	var raw string
	err := r.db.QueryRowContext(ctx,
		`SELECT embedding::text FROM users WHERE id = $1`, userID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return parseVectorLiteral(raw), nil
}

// VectorLiteral converts a float32 slice into PostgreSQL vector literal e.g. "[0.1,0.2,...]"
// Returns an empty string when the slice is empty; callers must pass it as a nullable
// parameter when interacting with SQL.
// Exported so the memory package can reuse it without circular dependency.
func VectorLiteral(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	sb := strings.Builder{}
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%g", f))
	}
	sb.WriteByte(']')
	return sb.String()
}

// parseVectorLiteral parses "[0.1,0.2,...]" back to []float32.
func parseVectorLiteral(s string) []float32 {
	s = strings.Trim(s, "[] \t\n")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]float32, 0, len(parts))
	for _, p := range parts {
		var f float64
		fmt.Sscanf(strings.TrimSpace(p), "%f", &f)
		result = append(result, float32(f))
	}
	return result
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
