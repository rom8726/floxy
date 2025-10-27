CREATE TABLE IF NOT EXISTS workflows.dead_letter_queue (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflows.workflow_instances(id) ON DELETE CASCADE,
    workflow_id TEXT NOT NULL REFERENCES workflows.workflow_definitions(id),
    step_id BIGINT NOT NULL REFERENCES workflows.workflow_steps(id) ON DELETE CASCADE,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,
    input JSONB,
    error TEXT,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dead_letter_instance_id ON workflows.dead_letter_queue(instance_id);
CREATE INDEX IF NOT EXISTS idx_dead_letter_workflow_id ON workflows.dead_letter_queue(workflow_id);
CREATE INDEX IF NOT EXISTS idx_dead_letter_created_at ON workflows.dead_letter_queue(created_at DESC);
