CREATE SCHEMA IF NOT EXISTS workflows;

CREATE TABLE IF NOT EXISTS workflows.workflow_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version INT NOT NULL,
    definition JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(name, version)
);

CREATE INDEX IF NOT EXISTS idx_workflow_definitions_name ON workflows.workflow_definitions(name);

CREATE TABLE IF NOT EXISTS workflows.workflow_instances (
    id BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows.workflow_definitions(id),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    input JSONB,
    output JSONB,
    error TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_instances_workflow_id ON workflows.workflow_instances(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_status ON workflows.workflow_instances(status);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_created_at ON workflows.workflow_instances(created_at DESC);

CREATE TABLE IF NOT EXISTS workflows.workflow_steps (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflows.workflow_instances(id) ON DELETE CASCADE,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL CHECK (step_type IN ('task', 'parallel', 'condition', 'fork', 'join')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped')),
    input JSONB,
    output JSONB,
    error TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_steps_instance_id ON workflows.workflow_steps(instance_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_status ON workflows.workflow_steps(status);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_step_name ON workflows.workflow_steps(step_name);

CREATE TABLE IF NOT EXISTS workflows.workflow_join_state (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflows.workflow_instances(id) ON DELETE CASCADE,
    join_step_name TEXT NOT NULL,
    waiting_for JSONB NOT NULL,  -- list of step names we are waiting for
    completed JSONB NOT NULL DEFAULT '[]',  -- list of completed steps
    failed JSONB NOT NULL DEFAULT '[]',     -- list of failed steps
    join_strategy TEXT NOT NULL DEFAULT 'all',  -- 'all' or 'any'
    is_ready BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(instance_id, join_step_name)
);

CREATE INDEX IF NOT EXISTS idx_workflow_join_state_instance ON workflows.workflow_join_state(instance_id);

CREATE TABLE IF NOT EXISTS workflows.workflow_queue (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflows.workflow_instances(id) ON DELETE CASCADE,
    step_id BIGINT REFERENCES workflows.workflow_steps(id) ON DELETE CASCADE,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempted_at TIMESTAMPTZ,
    attempted_by TEXT,
    priority INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_workflow_queue_scheduled ON workflows.workflow_queue(scheduled_at, priority DESC)
    WHERE attempted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_workflow_queue_instance_id ON workflows.workflow_queue(instance_id);

CREATE TABLE IF NOT EXISTS workflows.workflow_events (
    id BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflows.workflow_instances(id) ON DELETE CASCADE,
    step_id BIGINT REFERENCES workflows.workflow_steps(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_events_instance_id ON workflows.workflow_events(instance_id);
CREATE INDEX IF NOT EXISTS idx_workflow_events_created_at ON workflows.workflow_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_events_event_type ON workflows.workflow_events(event_type);

CREATE OR REPLACE FUNCTION workflows.update_updated_at_column()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

DROP TRIGGER IF EXISTS update_workflow_instances_updated_at ON workflows.workflow_instances;
CREATE TRIGGER update_workflow_instances_updated_at
    BEFORE UPDATE ON workflows.workflow_instances
    FOR EACH ROW
EXECUTE FUNCTION workflows.update_updated_at_column();


COMMENT ON TABLE workflows.workflow_definitions IS 'Workflow templates with definition of the execution graph';
COMMENT ON TABLE workflows.workflow_instances IS 'Instances of running workflows';
COMMENT ON TABLE workflows.workflow_steps IS 'Separate workflow execution steps';
COMMENT ON TABLE workflows.workflow_queue IS 'Queue of steps for workers to complete';
COMMENT ON TABLE workflows.workflow_events IS 'Event log for auditing and debugging';

COMMENT ON COLUMN workflows.workflow_definitions.definition IS 'JSONB graph with adjacency list structure';
COMMENT ON COLUMN workflows.workflow_instances.status IS 'pending | running | completed | failed | cancelled';
COMMENT ON COLUMN workflows.workflow_steps.status IS 'pending | running | completed | failed | skipped';
COMMENT ON COLUMN workflows.workflow_steps.step_type IS 'task | parallel | condition';

DROP VIEW IF EXISTS workflows.active_workflows;
CREATE OR REPLACE VIEW workflows.active_workflows AS
SELECT
    wi.id,
    wi.workflow_id,
    wi.status,
    wi.created_at,
    wi.updated_at,
    EXTRACT(EPOCH FROM (NOW() - wi.created_at)) as duration_seconds,
    COUNT(ws.id) as total_steps,
    COUNT(ws.id) FILTER (WHERE ws.status = 'completed') as completed_steps,
    COUNT(ws.id) FILTER (WHERE ws.status = 'failed') as failed_steps,
    COUNT(ws.id) FILTER (WHERE ws.status = 'running') as running_steps
FROM workflows.workflow_instances wi
         LEFT JOIN workflows.workflow_steps ws ON wi.id = ws.instance_id
WHERE wi.status IN ('pending', 'running')
GROUP BY wi.id, wi.workflow_id, wi.status, wi.created_at, wi.updated_at;

DROP VIEW IF EXISTS workflows.workflow_stats;
CREATE OR REPLACE VIEW workflows.workflow_stats AS
SELECT
    wd.name,
    wd.version,
    COUNT(wi.id) as total_instances,
    COUNT(wi.id) FILTER (WHERE wi.status = 'completed') as completed,
    COUNT(wi.id) FILTER (WHERE wi.status = 'failed') as failed,
    COUNT(wi.id) FILTER (WHERE wi.status = 'running') as running,
    AVG(EXTRACT(EPOCH FROM (wi.completed_at - wi.created_at)))
    FILTER (WHERE wi.status = 'completed') as avg_duration_seconds
FROM workflows.workflow_definitions wd
         LEFT JOIN workflows.workflow_instances wi ON wd.id = wi.workflow_id
GROUP BY wd.name, wd.version;

CREATE OR REPLACE FUNCTION workflows.cleanup_old_workflows(days_to_keep INT DEFAULT 30)
    RETURNS TABLE(deleted_count BIGINT) AS $$
DECLARE
    result BIGINT;
BEGIN
    DELETE FROM workflows.workflow_instances
    WHERE status IN ('completed', 'failed', 'cancelled')
      AND completed_at < NOW() - INTERVAL '1 day' * days_to_keep;

    GET DIAGNOSTICS result = ROW_COUNT;
    RETURN QUERY SELECT result;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION workflows.cleanup_old_workflows IS 'Deletes completed workflows older than the specified number of days';
