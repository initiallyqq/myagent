-- 003_create_indexes.sql

-- Scalar filter indexes on users
CREATE INDEX IF NOT EXISTS idx_users_gender        ON users(gender);
CREATE INDEX IF NOT EXISTS idx_users_destinations  ON users USING GIN(destinations);
CREATE INDEX IF NOT EXISTS idx_users_tags          ON users USING GIN(tags);

-- HNSW vector index on users.embedding
-- Build this index when user count exceeds 100,000.
-- Cost: one-time background build; queries degrade gracefully without it.
CREATE INDEX IF NOT EXISTS idx_users_embedding ON users
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);

-- demand_pool indexes
CREATE INDEX IF NOT EXISTS idx_demand_pool_status ON demand_pool(status)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_demand_pool_requester ON demand_pool(requester_id);

CREATE INDEX IF NOT EXISTS idx_demand_intent_vector ON demand_pool
    USING hnsw (intent_vector vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);

CREATE INDEX IF NOT EXISTS idx_demand_pool_expires ON demand_pool(expires_at);
