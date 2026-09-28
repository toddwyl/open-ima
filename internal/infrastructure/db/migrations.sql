-- Schema v4:每表以 id INTEGER PRIMARY KEY AUTOINCREMENT 为物理主键,
-- 业务表另设 <实体>_biz_id TEXT NOT NULL UNIQUE 业务键;外键列引用业务键
-- 并与其同名列对应(如 medias.kb_biz_id 引用 knowledge_bases.kb_biz_id)。
-- 版本由 db.Open 通过 PRAGMA user_version 守卫;v2/v3 库经 ALTER TABLE 升级,见 db.go。

CREATE TABLE IF NOT EXISTS knowledge_bases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kb_biz_id   TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS medias (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    media_biz_id TEXT NOT NULL UNIQUE,
    kb_biz_id       TEXT NOT NULL REFERENCES knowledge_bases(kb_biz_id),
    title           TEXT NOT NULL,
    source_type     TEXT NOT NULL,
    source_uri      TEXT NOT NULL,
    file_type       TEXT NOT NULL,
    file_hash       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
                    -- pending|parsing|chunking|indexing|ready|failed|deleting
    error           TEXT NOT NULL DEFAULT '',
    chunk_count     INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_medias_kb ON medias(kb_biz_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_medias_hash ON medias(kb_biz_id, file_hash)
    WHERE file_hash != '';

CREATE TABLE IF NOT EXISTS chunks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    chunk_biz_id    TEXT NOT NULL UNIQUE,
    media_biz_id TEXT NOT NULL REFERENCES medias(media_biz_id),
    kb_biz_id       TEXT NOT NULL,
    seq             INTEGER NOT NULL,
    token_count     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_chunks_doc ON chunks(media_biz_id);

CREATE TABLE IF NOT EXISTS jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,
    payload     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    dedupe_key  TEXT NOT NULL DEFAULT '',
    run_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_jobs_poll ON jobs(status, run_at);
CREATE INDEX IF NOT EXISTS idx_jobs_dedupe ON jobs(type, dedupe_key, status);

CREATE TABLE IF NOT EXISTS conversations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_biz_id TEXT NOT NULL UNIQUE,
    kb_biz_id           TEXT NOT NULL REFERENCES knowledge_bases(kb_biz_id),
    title               TEXT NOT NULL DEFAULT '',
    mode                TEXT NOT NULL DEFAULT 'agent',
                    -- quick|agent
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    message_biz_id      TEXT NOT NULL UNIQUE,
    conversation_biz_id TEXT NOT NULL REFERENCES conversations(conversation_biz_id),
    role                TEXT NOT NULL,
    content             TEXT NOT NULL,
    citations           TEXT NOT NULL DEFAULT '[]',
    agent_steps         TEXT,       -- []AgentStep JSON;NULL 表示 RAG 旧消息
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS app_settings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
