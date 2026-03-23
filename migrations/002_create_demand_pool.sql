-- 002_create_demand_pool.sql
CREATE TABLE IF NOT EXISTS demand_pool (
    id              BIGSERIAL PRIMARY KEY,
    requester_id    BIGINT       REFERENCES users(id) ON DELETE CASCADE,
    intent_json     JSONB        NOT NULL,
    intent_vector   vector(1024),
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',  -- pending / matched / expired
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ  NOT NULL DEFAULT now() + INTERVAL '30 days'
);
