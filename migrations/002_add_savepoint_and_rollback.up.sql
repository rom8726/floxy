-- Add save_point step type
ALTER TABLE workflows.workflow_steps DROP CONSTRAINT IF EXISTS workflow_steps_step_type_check;

ALTER TABLE workflows.workflow_steps ADD CONSTRAINT workflow_steps_step_type_check 
    CHECK (step_type IN ('task', 'parallel', 'condition', 'fork', 'join', 'save_point', 'human'));

-- Add rolled_back status
ALTER TABLE workflows.workflow_steps DROP CONSTRAINT IF EXISTS workflow_steps_status_check;

ALTER TABLE workflows.workflow_steps ADD CONSTRAINT workflow_steps_status_check 
    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped', 'rolled_back', 'waiting_decision', 'confirmed', 'rejected'));

-- Update comments
COMMENT ON COLUMN workflows.workflow_steps.step_type IS 'task | parallel | condition | fork | join | save_point | human';
COMMENT ON COLUMN workflows.workflow_steps.status IS 'pending | running | completed | failed | skipped | rolled_back | waiting_decision | confirmed | rejected';
