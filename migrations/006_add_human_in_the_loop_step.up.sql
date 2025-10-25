-- Add aborted state
ALTER TABLE workflows.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_status_check;
ALTER TABLE workflows.workflow_instances ADD CHECK (status IN ('pending', 'running', 'completed', 'failed', 'rolling_back', 'cancelled', 'cancelling', 'aborted'));

-- Add human-in-the-loop step type
ALTER TABLE workflows.workflow_steps DROP CONSTRAINT IF EXISTS workflow_steps_step_type_check;

ALTER TABLE workflows.workflow_steps ADD CONSTRAINT workflow_steps_step_type_check
    CHECK (step_type IN ('task', 'parallel', 'condition', 'fork', 'join', 'save_point', 'human'));

-- Add waiting_decision, confirmed, rejected statuses
ALTER TABLE workflows.workflow_steps
    DROP CONSTRAINT workflow_steps_status_check,
    ADD CONSTRAINT workflow_steps_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped', 'compensation', 'rolled_back', 'waiting_decision', 'confirmed', 'rejected'));

-- Update comments
COMMENT ON COLUMN workflows.workflow_steps.step_type IS 'task | parallel | condition | fork | join | save_point | human';
COMMENT ON COLUMN workflows.workflow_steps.status IS 'pending | running | completed | failed | skipped | compensation | rolled_back | waiting_decision | confirmed | rejected';

-- New table with human decisions
CREATE TABLE IF NOT EXISTS workflows.workflow_human_decisions
(
    id             bigserial primary key,
    instance_id    bigint not null references workflows.workflow_instances on delete cascade,
    step_id        bigint not null references workflows.workflow_steps on delete cascade,
    decided_by     text not null,
    decision       text not null constraint workflow_human_decisions_decision_check check (decision in ('confirmed', 'rejected')),
    comment        text,
    decided_at     timestamp with time zone default now() not null,
    created_at     timestamp with time zone default now() not null,

    UNIQUE (step_id, decided_by)
);

comment on table workflows.workflow_human_decisions IS
    'Stores user decisions for human-in-the-loop workflow steps (confirm/reject).';

COMMENT ON COLUMN workflows.workflow_human_decisions.decided_by IS
    'Arbitrary user identifier (e.g. username, email, external ID).';

COMMENT ON COLUMN workflows.workflow_human_decisions.decision IS
    'confirmed | rejected';

CREATE INDEX IF NOT EXISTS idx_workflow_human_decisions_instance_id
    ON workflows.workflow_human_decisions (instance_id);

CREATE INDEX IF NOT EXISTS idx_workflow_human_decisions_step_id
    ON workflows.workflow_human_decisions (step_id);
