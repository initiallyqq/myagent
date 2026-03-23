-- 004_create_memories.sql
-- mem0-style persistent memory store
CREATE TABLE IF NOT EXISTS memories (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT       REFERENCES users(id) ON DELETE CASCADE,
    content      TEXT         NOT NULL,                 -- "用户偏好西藏风格的户外徒步"
    source       VARCHAR(32)  NOT NULL DEFAULT 'chat',  -- chat | profile | feedback
    embedding    vector(1024),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memories_user_id ON memories(user_id);
CREATE INDEX IF NOT EXISTS idx_memories_embedding ON memories
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);
