-- ChronoFlow 00001 initial schema (DOWN).
-- DESTRUCTIVE: this removes all ChronoFlow application data. Run manually only
-- after a verified backup; Docker Compose never mounts or executes this file.

USE chronoflow;

DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS timer_executions;
DROP TABLE IF EXISTS timer_definitions;
