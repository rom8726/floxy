CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

ALTER TABLE workflows.workflow_steps ADD COLUMN IF NOT EXISTS idempotency_key UUID NOT NULL DEFAULT gen_random_uuid();
