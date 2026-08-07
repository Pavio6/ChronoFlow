-- ChronoFlow worker/recovery support.
-- Apply exactly once after 002_durable_scheduler.sql.

USE chronoflow;

ALTER TABLE timer_executions
    ADD COLUMN last_enqueued_at DATETIME(3) NULL
        COMMENT '最近一次创建投递事件的时间(UTC)'
        AFTER run_token,
    DROP INDEX idx_execution_recovery,
    ADD INDEX idx_execution_recovery(
        status,
        lease_until,
        next_attempt_at,
        last_enqueued_at
    );

UPDATE timer_executions
SET last_enqueued_at = created_at
WHERE last_enqueued_at IS NULL;
