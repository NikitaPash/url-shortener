CREATE DATABASE IF NOT EXISTS shortener;

CREATE TABLE IF NOT EXISTS shortener.clicks (
    timestamp   DateTime64(3, 'UTC'),
    short_id    String,
    user_id     String,
    ip          String,
    user_agent  String,
    referrer    String,
    country     LowCardinality(String),
    device      LowCardinality(String),
    browser     LowCardinality(String),
    is_bot      UInt8
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (short_id, timestamp);

-- Idempotent column addition for existing deployments (no-op if column already exists).
ALTER TABLE shortener.clicks ADD COLUMN IF NOT EXISTS browser LowCardinality(String) DEFAULT '';
