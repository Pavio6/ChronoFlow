-- Remove storage used only by the retired pre-generated/bucket scheduler.
-- Apply after application traffic has switched to timer_executions and the old
-- timer_records data has been archived if it must be retained.

USE chronoflow;

DROP TABLE IF EXISTS timer_records;
