-- Update cleanup function to include new terminal states: cancelled, aborted
CREATE OR REPLACE FUNCTION workflows.cleanup_old_workflows(days_to_keep INT DEFAULT 30)
    RETURNS TABLE(deleted_count BIGINT) AS $$
DECLARE
    result BIGINT;
BEGIN
    DELETE FROM workflows.workflow_instances
    WHERE status IN ('completed', 'failed', 'cancelled', 'aborted')
      AND completed_at < NOW() - INTERVAL '1 day' * days_to_keep;

    GET DIAGNOSTICS result = ROW_COUNT;
    RETURN QUERY SELECT result;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION workflows.cleanup_old_workflows IS 'Deletes completed workflows older than the specified number of days. Includes terminal states: completed, failed, cancelled, aborted';
