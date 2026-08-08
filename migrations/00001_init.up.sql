-- ChronoFlow 00001 initial schema (UP).
-- This migration creates the complete current schema for a new environment.

CREATE DATABASE IF NOT EXISTS chronoflow
    DEFAULT CHARACTER SET utf8mb4
    DEFAULT COLLATE utf8mb4_unicode_ci;

USE chronoflow;

CREATE TABLE IF NOT EXISTS timer_definitions (
    id               BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '定时器ID',
    app              VARCHAR(128) NOT NULL COMMENT '应用名',
    name             VARCHAR(128) NOT NULL COMMENT '定时器名称',
    cron_expr        VARCHAR(64) NOT NULL COMMENT 'Cron表达式（6字段：秒 分 时 日 月 周）',
    callback_url     VARCHAR(512) NOT NULL COMMENT '回调URL',
    callback_method  VARCHAR(16) NOT NULL DEFAULT 'POST' COMMENT '回调HTTP方法',
    callback_body    TEXT COMMENT '回调请求体',
    callback_headers TEXT COMMENT '回调请求头（JSON格式）',
    status           VARCHAR(32) NOT NULL DEFAULT 'INACTIVE' COMMENT 'ACTIVE/INACTIVE/DELETED',
    next_fire_at     DATETIME(3) NULL COMMENT '下一次计划触发时间（UTC）',
    timezone         VARCHAR(64) NOT NULL DEFAULT 'UTC' COMMENT 'Cron计算时区',
    misfire_policy   VARCHAR(32) NOT NULL DEFAULT 'FIRE_ONCE' COMMENT 'SKIP/FIRE_ONCE/CATCH_UP',
    max_catch_up     INT NOT NULL DEFAULT 10 COMMENT '单轮最大补偿次数',
    version          BIGINT NOT NULL DEFAULT 1 COMMENT '乐观锁版本',
    created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_timer_due (status, next_fire_at, id),
    INDEX idx_timer_app (app)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时器定义';

CREATE TABLE IF NOT EXISTS timer_executions (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    timer_id          BIGINT NOT NULL,
    scheduled_at      DATETIME(3) NOT NULL COMMENT '计划触发时间（UTC）',
    status            VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    attempt           INT NOT NULL DEFAULT 0,
    max_attempts      INT NOT NULL DEFAULT 3,
    next_attempt_at   DATETIME(3) NULL,
    lease_owner       VARCHAR(128) NOT NULL DEFAULT '',
    lease_until       DATETIME(3) NULL,
    run_token         VARCHAR(64) NOT NULL DEFAULT '',
    last_enqueued_at  DATETIME(3) NULL COMMENT '最近一次创建投递事件的时间（UTC）',
    request_snapshot  JSON NOT NULL,
    started_at        DATETIME(3) NULL,
    finished_at       DATETIME(3) NULL,
    response_code     INT NOT NULL DEFAULT 0,
    response_body     TEXT,
    error_message     TEXT,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_execution_schedule (timer_id, scheduled_at),
    INDEX idx_timer_executions_timer_id (timer_id),
    INDEX idx_timer_executions_scheduled_at (scheduled_at),
    INDEX idx_execution_recovery (status, lease_until, next_attempt_at, last_enqueued_at),
    INDEX idx_timer_executions_run_token (run_token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时任务执行记录';

CREATE TABLE IF NOT EXISTS outbox_events (
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
    UNIQUE KEY uk_outbox_event_id (event_id),
    INDEX idx_outbox_events_aggregate_id (aggregate_id),
    INDEX idx_outbox_publish (published_at, available_at, next_attempt_at, claim_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='事务Outbox事件';
