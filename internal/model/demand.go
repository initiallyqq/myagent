package model

import "time"

// DemandPool stores unsatisfied user intent, waiting for async reverse-matching.
type DemandPool struct {
	ID            int64      `db:"id"             json:"id"`
	RequesterID   int64      `db:"requester_id"   json:"requester_id"`
	IntentJSON    []byte     `db:"intent_json"    json:"intent_json"`
	IntentVector  []float32  `db:"intent_vector"  json:"-"`
	Status        string     `db:"status"         json:"status"` // pending / matched / expired
	CreatedAt     time.Time  `db:"created_at"     json:"created_at"`
	ExpiresAt     time.Time  `db:"expires_at"     json:"expires_at"`
}

// SubscribeReq is the HTTP request body for subscribing to push notifications.
type SubscribeReq struct {
	OpenID     string `json:"openid"      binding:"required"`
	TemplateID string `json:"template_id"`
}
