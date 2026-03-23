package model

import "time"

// User represents the user profile stored in PostgreSQL.
type User struct {
	ID             int64          `db:"id"              json:"id"`
	OpenID         string         `db:"openid"          json:"openid"`
	Nickname       string         `db:"nickname"        json:"nickname"`
	Gender         string         `db:"gender"          json:"gender"`   // M / F / X
	Tags           map[string]any `db:"tags"            json:"tags"`    // JSONB: {"mbti":"ENFP","hobby":["摄影"]}
	Destinations   []string       `db:"destinations"    json:"destinations"`
	BudgetMin      int            `db:"budget_min"      json:"budget_min"`
	BudgetMax      int            `db:"budget_max"      json:"budget_max"`
	AvailableStart *time.Time     `db:"available_start" json:"available_start,omitempty"`
	AvailableEnd   *time.Time     `db:"available_end"   json:"available_end,omitempty"`
	Embedding      []float32      `db:"embedding"       json:"-"`
	Similarity     float64        `db:"similarity"      json:"similarity,omitempty"`
	CreatedAt      time.Time      `db:"created_at"      json:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"      json:"updated_at"`
}

// UserRegisterReq is the HTTP request body for user registration.
type UserRegisterReq struct {
	OpenID         string         `json:"openid"           binding:"required"`
	Nickname       string         `json:"nickname"`
	Gender         string         `json:"gender"           binding:"oneof=M F X"`
	Tags           map[string]any `json:"tags"`
	Destinations   []string       `json:"destinations"`
	BudgetMin      int            `json:"budget_min"`
	BudgetMax      int            `json:"budget_max"`
	AvailableStart *time.Time     `json:"available_start"`
	AvailableEnd   *time.Time     `json:"available_end"`
	Bio            string         `json:"bio"` // free-text for embedding generation
}
