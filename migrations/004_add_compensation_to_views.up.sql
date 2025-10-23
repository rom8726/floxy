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
    COUNT(ws.id) FILTER (WHERE ws.status = 'running') as running_steps,
    COUNT(ws.id) FILTER (WHERE ws.status = 'compensation') as compensation_steps,
    COUNT(ws.id) FILTER (WHERE ws.status = 'rolled_back') as rolled_back_steps
FROM workflows.workflow_instances wi
         LEFT JOIN workflows.workflow_steps ws ON wi.id = ws.instance_id
WHERE wi.status IN ('pending', 'running')
GROUP BY wi.id, wi.workflow_id, wi.status, wi.created_at, wi.updated_at;
