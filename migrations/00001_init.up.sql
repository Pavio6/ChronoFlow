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
    cron_expr        VARCHAR(64) NOT NULL COMMENT 'Cron表达式',
    callback_url     VARCHAR(512) NOT NULL COMMENT '回调URL',
    callback_method  VARCHAR(16) NOT NULL DEFAULT 'POST' COMMENT '回调HTTP方法',
    callback_body    TEXT COMMENT '回调请求体',
    callback_headers TEXT COMMENT '回调请求头（JSON格式）',
    status           VARCHAR(32) NOT NULL DEFAULT 'INACTIVE' COMMENT 'ACTIVE/INACTIVE/DELETED',
    next_fire_at     DATETIME(3) NULL COMMENT '下一次计划触发时间',
    timezone         VARCHAR(64) NOT NULL DEFAULT 'Local' COMMENT 'Cron计算时区',
    misfire_policy   VARCHAR(32) NOT NULL DEFAULT 'FIRE_ONCE' COMMENT 'SKIP/FIRE_ONCE/CATCH_UP',
    max_catch_up     INT NOT NULL DEFAULT 10 COMMENT '单轮最大补偿次数',
    version          BIGINT NOT NULL DEFAULT 1 COMMENT '乐观锁版本',
    created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_timer_due (status, next_fire_at, id),
    INDEX idx_timer_app (app)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时器定义';

CREATE TABLE IF NOT EXISTS timer_executions (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '执行记录ID',
    timer_id          BIGINT NOT NULL COMMENT '所属定时器ID',
    scheduled_at      DATETIME(3) NOT NULL COMMENT '计划触发时间',
    status            VARCHAR(32) NOT NULL DEFAULT 'PENDING' COMMENT 'PENDING/RUNNING/RETRY_WAIT/SUCCESS/FAILED/CANCELLED',
    attempt           INT NOT NULL DEFAULT 0 COMMENT '已开始的执行尝试次数',
    max_attempts      INT NOT NULL DEFAULT 3 COMMENT '最大执行尝试次数',
    next_attempt_at   DATETIME(3) NULL COMMENT '下一次重试时间',
    lease_owner       VARCHAR(128) NOT NULL DEFAULT '' COMMENT '当前执行 Lease 所有者',
    lease_until       DATETIME(3) NULL COMMENT '当前执行 Lease 过期时间',
    run_token         VARCHAR(64) NOT NULL DEFAULT '' COMMENT '本次执行尝试的防过期结果令牌',
    last_enqueued_at  DATETIME(3) NULL COMMENT '最近一次创建投递事件的时间',
    request_snapshot  JSON NOT NULL COMMENT '执行时固定的回调请求快照',
    started_at        DATETIME(3) NULL COMMENT '本次尝试开始时间',
    finished_at       DATETIME(3) NULL COMMENT '本次尝试结束时间',
    response_code     INT NOT NULL DEFAULT 0 COMMENT '回调 HTTP 响应状态码',
    response_body     TEXT COMMENT '截断后的回调响应内容',
    error_message     TEXT COMMENT '最后一次执行错误信息',
    duration_ms       BIGINT NOT NULL DEFAULT 0 COMMENT '最后一次执行耗时（毫秒）',
    created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    UNIQUE KEY uk_execution_schedule (timer_id, scheduled_at),
    INDEX idx_timer_executions_timer_id (timer_id),
    INDEX idx_timer_executions_scheduled_at (scheduled_at),
    INDEX idx_execution_recovery (status, lease_until, next_attempt_at, last_enqueued_at),
    INDEX idx_timer_executions_run_token (run_token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时任务执行记录';

CREATE TABLE IF NOT EXISTS outbox_events (
    id                   BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT 'Outbox 事件ID',
    event_id             VARCHAR(64) NOT NULL COMMENT '全局唯一业务事件标识',
    aggregate_type       VARCHAR(64) NOT NULL COMMENT '聚合类型，例如 execution',
    aggregate_id         BIGINT NOT NULL COMMENT '关联聚合ID',
    event_type           VARCHAR(64) NOT NULL COMMENT '事件类型，例如 execution.ready',
    payload              JSON NOT NULL COMMENT '投递到 Redis Stream 的事件载荷',
    available_at         DATETIME(3) NOT NULL COMMENT '首次允许投递时间',
    published_at         DATETIME(3) NULL COMMENT '成功投递到 Stream 的时间',
    published_message_id VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Redis Stream 消息ID',
    attempts             INT NOT NULL DEFAULT 0 COMMENT 'Outbox 投递尝试次数',
    next_attempt_at      DATETIME(3) NULL COMMENT '下一次投递重试时间',
    claim_owner          VARCHAR(128) NOT NULL DEFAULT '' COMMENT '当前投递 Lease 所有者',
    claim_until          DATETIME(3) NULL COMMENT '当前投递 Lease 过期时间',
    last_error           TEXT COMMENT '最近一次投递失败信息',
    created_at           DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at           DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    UNIQUE KEY uk_outbox_event_id (event_id),
    INDEX idx_outbox_events_aggregate_id (aggregate_id),
    INDEX idx_outbox_publish (published_at, available_at, next_attempt_at, claim_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='事务Outbox事件';
