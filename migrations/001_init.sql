-- ChronoFlow 数据库初始化脚本

-- 创建任务表
CREATE TABLE IF NOT EXISTS `tasks` (
    `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
    `name` VARCHAR(128) NOT NULL COMMENT '任务名称',
    `description` VARCHAR(512) DEFAULT '' COMMENT '任务描述',
    `cron_expr` VARCHAR(64) NOT NULL COMMENT 'Cron表达式',
    `callback_url` VARCHAR(512) NOT NULL COMMENT '回调URL',
    `callback_method` VARCHAR(16) NOT NULL DEFAULT 'POST' COMMENT '回调方法',
    `callback_body` TEXT COMMENT '回调请求体',
    `callback_headers` TEXT COMMENT '回调请求头(JSON)',
    `status` VARCHAR(32) NOT NULL DEFAULT 'INIT' COMMENT '任务状态',
    `timeout` INT DEFAULT 30 COMMENT '超时时间(秒)',
    `max_retries` INT DEFAULT 3 COMMENT '最大重试次数',
    `next_trigger_time` DATETIME COMMENT '下次触发时间',
    `last_trigger_time` DATETIME COMMENT '上次触发时间',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX `idx_status` (`status`),
    INDEX `idx_next_trigger_time` (`next_trigger_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='任务表';

-- 创建执行记录表
CREATE TABLE IF NOT EXISTS `task_executions` (
    `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
    `task_id` BIGINT NOT NULL COMMENT '任务ID',
    `trigger_time` DATETIME NOT NULL COMMENT '触发时间',
    `status` VARCHAR(32) NOT NULL DEFAULT 'PENDING' COMMENT '执行状态',
    `retry_count` INT DEFAULT 0 COMMENT '重试次数',
    `request_url` VARCHAR(512) COMMENT '请求URL',
    `request_method` VARCHAR(16) COMMENT '请求方法',
    `request_body` TEXT COMMENT '请求体',
    `response_code` INT COMMENT '响应状态码',
    `response_body` TEXT COMMENT '响应体',
    `error_message` TEXT COMMENT '错误信息',
    `started_at` DATETIME COMMENT '开始时间',
    `finished_at` DATETIME COMMENT '完成时间',
    `duration` BIGINT COMMENT '执行时长(毫秒)',
    `next_retry_time` DATETIME COMMENT '下次重试时间',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX `idx_task_id` (`task_id`),
    INDEX `idx_status` (`status`),
    INDEX `idx_trigger_time` (`trigger_time`),
    INDEX `idx_next_retry_time` (`next_retry_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='任务执行记录表';
