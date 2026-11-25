CREATE TABLE  workers (
    id TEXT PRIMARY KEY ,
    hostname TEXT NOT NULL , 
    os TEXT NOT NULL ,
    arch TEXT NOT NULL ,
    cpu_model TEXT,
    cpu_cores INT,
    ram_total_bytes BIGINT NOT NULL ,
    labels JSON NOT NULL DEFAULT '{}', -- NEED MORE HOW THIS WORKS --
    secret_hash  BYTEA NOT NULL ,
    status TEXT NOT NULL DEFAULT 'pending',
    last_seen_at TIMESTAMPTZ ,
    agent_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
CREATE TABLE join_tokens (
 token_hash BIGSERIAL PRIMARY KEY,
 expires_at BYTEA NOT NULL UNIQUE ,
 max_uses INT NOT NULL DEFAULT 1,
 used_count INT NOT NULL DEFAULT 0,
 labels JSONB NOT NULL DEFAULT'{}' --- labels applied to registed workers 
)