package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"myagent/internal/model"
)

// DemandRepo handles all PostgreSQL operations on the demand_pool table.
type DemandRepo struct {
	db *sql.DB
}

func NewDemandRepo(db *sql.DB) *DemandRepo {
	return &DemandRepo{db: db}
}

// Insert persists an unsatisfied intent to the demand pool.
func (r *DemandRepo) Insert(ctx context.Context, requesterID int64, intent *model.Intent, embedding []float32) (int64, error) {
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return 0, fmt.Errorf("marshal intent: %w", err)
	}
	embStr := VectorLiteral(embedding)
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO demand_pool (requester_id, intent_json, intent_vector, status)
		VALUES ($1, $2, $3::vector, 'pending')
		RETURNING id`,
		requesterID, intentJSON, embStr,
	).Scan(&id)
	return id, err
}

// PendingAll returns all pending demands (for cron reverse-matching).
func (r *DemandRepo) PendingAll(ctx context.Context) ([]*model.DemandPool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, requester_id, intent_json, intent_vector::text, status, created_at, expires_at
		FROM demand_pool
		WHERE status = 'pending' AND expires_at > now()
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var demands []*model.DemandPool
	for rows.Next() {
		d := &model.DemandPool{}
		var vectorStr string
		err := rows.Scan(
			&d.ID, &d.RequesterID, &d.IntentJSON, &vectorStr,
			&d.Status, &d.CreatedAt, &d.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		d.IntentVector = parseVectorLiteral(vectorStr)
		demands = append(demands, d)
	}
	return demands, rows.Err()
}

// MarkMatched sets the status of a demand to "matched".
func (r *DemandRepo) MarkMatched(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE demand_pool SET status = 'matched' WHERE id = $1`, id)
	return err
}

// GetRequesterOpenID returns the openid of the demand requester.
func (r *DemandRepo) GetRequesterOpenID(ctx context.Context, demandID int64) (string, error) {
	var openid string
	err := r.db.QueryRowContext(ctx, `
		SELECT u.openid FROM users u
		JOIN demand_pool d ON d.requester_id = u.id
		WHERE d.id = $1`, demandID).Scan(&openid)
	return openid, err
}

// CosineSimilarity computes cosine similarity between a user embedding and a demand vector using PG.
func (r *DemandRepo) CosineSimilarityBatch(ctx context.Context, userID int64, demandIDs []int64) (map[int64]float64, error) {
	if len(demandIDs) == 0 {
		return nil, nil
	}
	// Build query: join user embedding with each demand vector
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, 1 - (u.embedding <=> d.intent_vector) AS similarity
		FROM demand_pool d
		JOIN users u ON u.id = $1
		WHERE d.id = ANY($2) AND d.status = 'pending'`,
		userID, int64SliceToArray(demandIDs),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var sim float64
		if err := rows.Scan(&id, &sim); err != nil {
			return nil, err
		}
		result[id] = sim
	}
	return result, rows.Err()
}

func int64SliceToArray(ids []int64) interface{} {
	return fmt.Sprintf("{%s}", joinInt64s(ids))
}

func joinInt64s(ids []int64) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%d", id)
	}
	return s
}

// GetOpenIDByUserID returns the openid for a user id.
func (r *DemandRepo) GetOpenIDByUserID(ctx context.Context, userID int64) (string, error) {
	var openid string
	err := r.db.QueryRowContext(ctx,
		`SELECT openid FROM users WHERE id = $1`, userID).Scan(&openid)
	return openid, err
}

// GetDemandsBySQL fetches demands where intent_vector is not null.
func (r *DemandRepo) GetDemandEmbeddings(ctx context.Context) (map[int64][]float32, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, intent_vector::text FROM demand_pool
		WHERE status = 'pending' AND expires_at > now() AND intent_vector IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]float32)
	for rows.Next() {
		var id int64
		var vecStr string
		if err := rows.Scan(&id, &vecStr); err != nil {
			return nil, err
		}
		result[id] = parseVectorLiteral(vecStr)
	}
	return result, rows.Err()
}

// GetRequesterIDByDemandID returns the requester_id for a demand.
func (r *DemandRepo) GetRequesterIDByDemandID(ctx context.Context, demandID int64) (int64, error) {
	var requesterID int64
	err := r.db.QueryRowContext(ctx,
		`SELECT requester_id FROM demand_pool WHERE id = $1`, demandID).Scan(&requesterID)
	return requesterID, err
}

// ExpireOld sets status='expired' for demands past their expires_at.
func (r *DemandRepo) ExpireOld(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE demand_pool SET status = 'expired' WHERE status = 'pending' AND expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetDemandByID returns a single demand.
func (r *DemandRepo) GetDemandByID(ctx context.Context, id int64) (*model.DemandPool, error) {
	d := &model.DemandPool{}
	var vecStr sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, requester_id, intent_json, intent_vector::text, status, created_at, expires_at
		FROM demand_pool WHERE id = $1`, id).Scan(
		&d.ID, &d.RequesterID, &d.IntentJSON, &vecStr,
		&d.Status, &d.CreatedAt, &d.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	if vecStr.Valid {
		d.IntentVector = parseVectorLiteral(vecStr.String)
	}
	return d, nil
}
