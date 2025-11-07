-- SQLite schema for floxy

CREATE TABLE IF NOT EXISTS workflow_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    definition BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE(name, version)
);

CREATE TABLE IF NOT EXISTS workflow_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    status TEXT NOT NULL,
    input BLOB,
    output BLOB,
    error TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,
    status TEXT NOT NULL,
    input BLOB,
    output BLOB,
    error TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 0,
    compensation_retry_count INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    step_id INTEGER,
    scheduled_at TIMESTAMP NOT NULL,
    attempted_at TIMESTAMP,
    attempted_by TEXT,
    priority INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_queue_sched ON queue(scheduled_at);

CREATE TABLE IF NOT EXISTS workflow_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    step_id INTEGER,
    event_type TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS join_states (
    instance_id INTEGER NOT NULL,
    join_step_name TEXT NOT NULL,
    waiting_for TEXT NOT NULL,
    completed TEXT NOT NULL,
    failed TEXT NOT NULL,
    join_strategy TEXT NOT NULL,
    is_ready INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY(instance_id, join_step_name)
);

CREATE TABLE IF NOT EXISTS cancel_requests (
    instance_id INTEGER PRIMARY KEY,
    requested_by TEXT NOT NULL,
    cancel_type TEXT NOT NULL,
    reason TEXT,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS human_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    step_id INTEGER NOT NULL,
    decided_by TEXT NOT NULL,
    decision TEXT NOT NULL,
    comment TEXT,
    decided_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_dlq (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    workflow_id TEXT NOT NULL,
    step_id INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,
    input BLOB,
    error TEXT,
    reason TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
