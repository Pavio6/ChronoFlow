-- Upgrade an existing database after resolving any historical duplicate rows:
-- SELECT timer_id, trigger_time, COUNT(*) FROM timer_records
-- GROUP BY timer_id, trigger_time HAVING COUNT(*) > 1;
ALTER TABLE timer_records
    ADD UNIQUE KEY uk_timer_trigger_time (timer_id, trigger_time);
