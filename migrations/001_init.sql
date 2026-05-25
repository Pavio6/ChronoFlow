-- ChronoFlow 数据库初始化脚本
-- 基于 xTimer 架构的定时器定义与执行记录分离模型

-- 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS chronoflow
    DEFAULT CHARACTER SET utf8mb4
    DEFAULT COLLATE utf8mb4_unicode_ci;

USE chronoflow;

-- 定时器定义表
-- 存储定时器的配置信息，包括 Cron 表达式、回调地址等
CREATE TABLE IF NOT EXISTS timer_definitions (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '定时器ID',
    app             VARCHAR(128) NOT NULL COMMENT '应用名',
    name            VARCHAR(128) NOT NULL COMMENT '定时器名称',
    cron_expr       VARCHAR(64)  NOT NULL COMMENT 'Cron表达式（6字段：秒 分 时 日 月 周）',
    callback_url    VARCHAR(512) NOT NULL COMMENT '回调URL',
    callback_method VARCHAR(16)  NOT NULL DEFAULT 'POST' COMMENT '回调HTTP方法',
    callback_body   TEXT COMMENT '回调请求体',
    callback_headers TEXT COMMENT '回调请求头（JSON格式）',
    status          VARCHAR(32) NOT NULL DEFAULT 'INACTIVE' COMMENT '状态：ACTIVE/INACTIVE/DELETED',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    INDEX idx_status(status),
    INDEX idx_app(app)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时器定义表';

-- 定时器执行记录表
-- 存储每次定时任务的执行详情，包括请求/响应信息等
CREATE TABLE IF NOT EXISTS timer_records (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '记录ID',
    timer_id        BIGINT NOT NULL COMMENT '定时器ID',
    trigger_time    DATETIME NOT NULL COMMENT '计划触发时间',
    status          VARCHAR(32) NOT NULL DEFAULT 'PENDING' COMMENT '执行状态：PENDING/RUNNING/SUCCESS/FAILED',
    request_url     VARCHAR(512) COMMENT '实际请求URL',
    request_method  VARCHAR(16) COMMENT '实际请求方法',
    request_body    TEXT COMMENT '实际请求体',
    response_code   INT COMMENT 'HTTP响应状态码',
    response_body   TEXT COMMENT 'HTTP响应体',
    error_message   TEXT COMMENT '错误信息',
    started_at      DATETIME COMMENT '开始执行时间',
    finished_at     DATETIME COMMENT '执行完成时间',
    duration        BIGINT COMMENT '执行耗时（毫秒）',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    UNIQUE KEY uk_timer_trigger_time(timer_id, trigger_time),
    INDEX idx_timer_id(timer_id),
    INDEX idx_trigger_time(trigger_time),
    INDEX idx_status(status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='定时器执行记录表';
