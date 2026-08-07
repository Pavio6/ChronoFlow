-- Introduce the MySQL-authoritative scheduler, durable executions and Outbox.
-- Apply exactly once after 001_init.sql.

USE chronoflow;

ALTER TABLE timer_definitions
    ADD COLUMN next_fire_at DATETIME(3) NULL COMMENT '下一次计划触发时间(UTC)' AFTER status,
    ADD COLUMN timezone VARCHAR(64) NOT NULL DEFAULT 'UTC' COMMENT 'Cron计算时区' AFTER next_fire_at,
    ADD COLUMN misfire_policy VARCHAR(32) NOT NULL DEFAULT 'FIRE_ONCE' COMMENT '错过触发策略' AFTER timezone,
    ADD COLUMN max_catch_up INT NOT NULL DEFAULT 10 COMMENT '单轮最大补偿次数' AFTER misfire_policy,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1 COMMENT '乐观锁版本' AFTER max_catch_up,
    ADD INDEX idx_timer_due(status, next_fire_at, id);

CREATE TABLE timer_executions (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    timer_id          BIGINT NOT NULL,
    scheduled_at      DATETIME(3) NOT NULL COMMENT '计划触发时间(UTC)',
    status            VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    attempt           INT NOT NULL DEFAULT 0,
    max_attempts      INT NOT NULL DEFAULT 3,
    next_attempt_at   DATETIME(3) NULL,
    lease_owner       VARCHAR(128) NOT NULL DEFAULT '',
    lease_until       DATETIME(3) NULL,
    run_token         VARCHAR(64) NOT NULL DEFAULT '',
    request_snapshot  JSON NOT NULL,
    started_at        DATETIME(3) NULL,
    finished_at       DATETIME(3) NULL,
    response_code     INT NOT NULL DEFAULT 0,
    response_body     TEXT,
    error_message     TEXT,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_execution_schedule(timer_id, scheduled_at),
    INDEX idx_timer_executions_timer_id(timer_id),
    INDEX idx_timer_executions_scheduled_at(scheduled_at),
    INDEX idx_execution_recovery(status, lease_until, next_attempt_at),
    INDEX idx_timer_executions_run_token(run_token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时任务执行记录';

CREATE TABLE outbox_events (
    id                   BIGINT PRIMARY KEY AUTO_INCREMENT,
    event_id             VARCHAR(64) NOT NULL,
    aggregate_type       VARCHAR(64) NOT NULL,
    aggregate_id         BIGINT NOT NULL,
    event_type           VARCHAR(64) NOT NULL,
    payload              JSON NOT NULL,
    available_at         DATETIME(3) NOT NULL,
    published_at         DATETIME(3) NULL,
    published_message_id VARCHAR(128) NOT NULL DEFAULT '',
    attempts             INT NOT NULL DEFAULT 0,
    next_attempt_at      DATETIME(3) NULL,
    claim_owner          VARCHAR(128) NOT NULL DEFAULT '',
    claim_until          DATETIME(3) NULL,
    last_error           TEXT,
    created_at           DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at           DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_outbox_event_id(event_id),
    INDEX idx_outbox_events_aggregate_id(aggregate_id),
    INDEX idx_outbox_publish(published_at, available_at, next_attempt_at, claim_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='事务Outbox事件';
