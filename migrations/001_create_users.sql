-- 001_create_users.sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS users (
    id              BIGSERIAL PRIMARY KEY,
    openid          VARCHAR(64)  UNIQUE NOT NULL,
    nickname        VARCHAR(64)  NOT NULL DEFAULT '',
    gender          CHAR(1)      NOT NULL DEFAULT 'X',       -- M / F / X
    tags            JSONB        NOT NULL DEFAULT '{}',      -- {"mbti":"ENFP","hobby":["摄影","徒步"]}
    destinations    TEXT[]       NOT NULL DEFAULT '{}',
    budget_min      INT          NOT NULL DEFAULT 0,
    budget_max      INT          NOT NULL DEFAULT 999999,
    available_start DATE,
    available_end   DATE,
    embedding       vector(1024),                            -- personality / bio embedding
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
