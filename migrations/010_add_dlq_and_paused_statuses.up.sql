-- Add DLQ workflow instance status and paused step status

-- Extend workflow_instances.status to include 'dlq'
ALTER TABLE workflows.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_status_check;
ALTER TABLE workflows.workflow_instances ADD CONSTRAINT workflow_instances_status_check
    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'rolling_back', 'cancelled', 'cancelling', 'aborted', 'dlq'));

-- Extend workflow_steps.status to include 'paused'
ALTER TABLE workflows.workflow_steps DROP CONSTRAINT IF EXISTS workflow_steps_status_check;
ALTER TABLE workflows.workflow_steps ADD CONSTRAINT workflow_steps_status_check
    CHECK (status IN (
        'pending', 'running', 'completed', 'failed', 'skipped',
        'compensation', 'rolled_back', 'waiting_decision', 'confirmed', 'rejected', 'paused'
    ));

-- Update active_workflows view to include 'dlq' as active
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
WHERE wi.status IN ('pending', 'running', 'dlq')
GROUP BY wi.id, wi.workflow_id, wi.status, wi.created_at, wi.updated_at;

-- Update comments (optional)
COMMENT ON COLUMN workflows.workflow_instances.status IS 'pending | running | completed | failed | rolling_back | cancelled | cancelling | aborted | dlq';
COMMENT ON COLUMN workflows.workflow_steps.status IS 'pending | running | completed | failed | skipped | compensation | rolled_back | waiting_decision | confirmed | rejected | paused';