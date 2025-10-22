-- Add compensation_retry_count column to workflow_steps table
ALTER TABLE workflows.workflow_steps ADD COLUMN IF NOT EXISTS compensation_retry_count INTEGER NOT NULL DEFAULT 0;

-- Update the status check constraint to include compensation status
ALTER TABLE workflows.workflow_steps
    DROP CONSTRAINT workflow_steps_status_check,
    ADD CONSTRAINT workflow_steps_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped', 'compensation', 'rolled_back'));

-- Update comments
COMMENT ON COLUMN workflows.workflow_steps.compensation_retry_count IS 'Number of compensation retries for this step';
COMMENT ON COLUMN workflows.workflow_steps.status IS 'pending | running | completed | failed | skipped | compensation | rolled_back';
