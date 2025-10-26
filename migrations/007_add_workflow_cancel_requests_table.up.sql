CREATE TABLE IF NOT EXISTS workflows.workflow_cancel_requests
(
    id           BIGSERIAL PRIMARY KEY,
    instance_id  BIGINT                   NOT NULL
        REFERENCES workflows.workflow_instances ON DELETE CASCADE,
    requested_by TEXT                     NOT NULL,
    cancel_type  TEXT                     NOT NULL
        CONSTRAINT workflow_cancel_requests_type_check CHECK (cancel_type IN ('cancel', 'abort')),
    reason       TEXT,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id)
);

CREATE INDEX IF NOT EXISTS idx_workflow_cancel_requests_instance_id
    ON workflows.workflow_cancel_requests(instance_id);
