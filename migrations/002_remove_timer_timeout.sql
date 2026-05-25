-- Apply this migration to databases created before timer-level timeout was removed.
UPDATE timer_records SET status = 'FAILED' WHERE status = 'TIMEOUT';
ALTER TABLE timer_definitions DROP COLUMN timeout;
