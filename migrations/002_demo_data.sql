-- ChronoFlow screenshot-ready demo dataset
-- Seeds 24 timers and 72 execution records. It is safe to run repeatedly.

USE chronoflow;

SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci;

START TRANSACTION;

DROP TEMPORARY TABLE IF EXISTS demo_timer_seed;
CREATE TEMPORARY TABLE demo_timer_seed (
    seed_no        INT PRIMARY KEY,
    app            VARCHAR(128) NOT NULL,
    name           VARCHAR(128) NOT NULL,
    cron_expr      VARCHAR(64) NOT NULL,
    callback_url   VARCHAR(512) NOT NULL,
    job            VARCHAR(128) NOT NULL,
    status         VARCHAR(32) NOT NULL
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

INSERT INTO demo_timer_seed (seed_no, app, name, cron_expr, callback_url, job, status) VALUES
    (1,  'order-service',        '订单超时自动关闭',   '0 */5 * * * *',  'http://localhost:9091/callback/success', 'close-expired-orders',   'ACTIVE'),
    (2,  'payment-service',      '支付渠道对账',       '0 0 */1 * * *',  'http://localhost:9091/callback/success', 'payment-reconciliation', 'ACTIVE'),
    (3,  'notification-service', '消息失败补偿',       '0 */10 * * * *', 'http://localhost:9091/callback/error',   'notification-retry',      'ACTIVE'),
    (4,  'inventory-service',    '库存缓存同步',       '30 */5 * * * *', 'http://localhost:9091/callback/success', 'sync-inventory-cache',   'ACTIVE'),
    (5,  'risk-service',         '风控名单扫描',       '0 */15 * * * *', 'http://localhost:9091/callback/slow',    'scan-risk-rules',        'ACTIVE'),
    (6,  'analytics-service',    '经营日报生成',       '0 0 8 * * *',    'http://localhost:9091/callback/success', 'daily-business-report',  'INACTIVE'),
    (7,  'billing-service',      '月度账单归档',       '0 0 2 1 * *',    'http://localhost:9091/callback/success', 'archive-monthly-bills', 'INACTIVE'),
    (8,  'crm-service',          '会员权益到期提醒',   '0 0 9 * * *',    'http://localhost:9091/callback/success', 'membership-reminder',   'INACTIVE'),
    (9,  'warehouse-service',    '仓库波次任务下发',   '0 */20 * * * *', 'http://localhost:9091/callback/success', 'release-picking-wave',   'ACTIVE'),
    (10, 'delivery-service',     '配送轨迹状态同步',   '0 */3 * * * *',  'http://localhost:9091/callback/success', 'sync-delivery-track',    'ACTIVE'),
    (11, 'coupon-service',       '优惠券过期处理',     '0 0 */2 * * *',  'http://localhost:9091/callback/success', 'expire-coupons',        'ACTIVE'),
    (12, 'search-service',       '商品索引增量刷新',   '0 */10 * * * *', 'http://localhost:9091/callback/success', 'refresh-search-index',   'ACTIVE'),
    (13, 'report-service',       '销售看板指标聚合',   '0 */30 * * * *', 'http://localhost:9091/callback/success', 'aggregate-sales-kpi',    'ACTIVE'),
    (14, 'settlement-service',   '商户结算批处理',     '0 30 1 * * *',   'http://localhost:9091/callback/slow',    'merchant-settlement',     'ACTIVE'),
    (15, 'refund-service',       '退款状态轮询',       '0 */5 * * * *',  'http://localhost:9091/callback/success', 'poll-refund-status',     'ACTIVE'),
    (16, 'audit-service',        '操作日志冷归档',     '0 0 3 * * *',    'http://localhost:9091/callback/success', 'archive-audit-log',      'INACTIVE'),
    (17, 'marketing-service',    '营销人群标签刷新',   '0 0 */4 * * *',  'http://localhost:9091/callback/success', 'refresh-segment-tags',   'ACTIVE'),
    (18, 'product-service',      '商品价格生效检查',   '0 */15 * * * *', 'http://localhost:9091/callback/error',   'apply-product-price',     'ACTIVE'),
    (19, 'finance-service',      '资金流水汇总',       '0 0 6 * * *',    'http://localhost:9091/callback/success', 'summarize-cash-flow',    'INACTIVE'),
    (20, 'support-service',      '工单超时升级',       '0 */10 * * * *', 'http://localhost:9091/callback/success', 'escalate-tickets',       'ACTIVE'),
    (21, 'identity-service',     '登录风险会话清理',   '0 */30 * * * *', 'http://localhost:9091/callback/success', 'cleanup-risk-sessions',  'ACTIVE'),
    (22, 'export-service',       '报表导出文件清理',   '0 0 */6 * * *',  'http://localhost:9091/callback/success', 'cleanup-export-files',   'INACTIVE'),
    (23, 'recommend-service',    '推荐模型特征同步',   '0 */20 * * * *', 'http://localhost:9091/callback/slow',    'sync-model-features',     'ACTIVE'),
    (24, 'message-service',      '短信发送回执补拉',   '0 */5 * * * *',  'http://localhost:9091/callback/error',   'pull-sms-receipts',       'ACTIVE');

INSERT INTO timer_definitions (
    app,
    name,
    cron_expr,
    callback_url,
    callback_method,
    callback_body,
    callback_headers,
    status,
    created_at,
    updated_at
)
SELECT
    seed.app,
    seed.name,
    seed.cron_expr,
    seed.callback_url,
    'POST',
    CONCAT('{"dataset":"screenshot-seed","job":"', seed.job, '"}'),
    '{"Content-Type":"application/json","X-Demo-Source":"ChronoFlow"}',
    seed.status,
    TIMESTAMPADD(MINUTE, -seed.seed_no, NOW()),
    TIMESTAMPADD(MINUTE, -seed.seed_no, NOW())
FROM demo_timer_seed AS seed
LEFT JOIN timer_definitions AS existing
    ON existing.app = seed.app
    AND existing.callback_body = CONCAT('{"dataset":"screenshot-seed","job":"', seed.job, '"}')
    AND existing.status != 'DELETED'
WHERE existing.id IS NULL;

-- Repair existing seed rows as well as insert new ones, including rows imported with a wrong client charset.
UPDATE timer_definitions AS definition
JOIN demo_timer_seed AS seed
    ON definition.app = seed.app
    AND definition.callback_body = CONCAT('{"dataset":"screenshot-seed","job":"', seed.job, '"}')
    AND definition.status != 'DELETED'
SET definition.name = seed.name,
    definition.cron_expr = seed.cron_expr,
    definition.callback_url = seed.callback_url,
    definition.status = seed.status,
    definition.updated_at = NOW();

INSERT INTO timer_records (
    timer_id,
    trigger_time,
    status,
    request_url,
    request_method,
    request_body,
    response_code,
    response_body,
    error_message,
    started_at,
    finished_at,
    duration,
    created_at,
    updated_at
)
SELECT
    definition.id,
    TIMESTAMPADD(MINUTE, record_seed.trigger_offset, NOW()),
    record_seed.record_status,
    definition.callback_url,
    definition.callback_method,
    CONCAT('{"dataset":"screenshot-seed","run_id":"', record_seed.run_id, '"}'),
    CASE record_seed.record_status WHEN 'SUCCESS' THEN 200 WHEN 'FAILED' THEN 500 ELSE 0 END,
    CASE record_seed.record_status
        WHEN 'SUCCESS' THEN '{"code":0,"message":"job completed successfully"}'
        WHEN 'FAILED' THEN '{"code":500,"message":"downstream service unavailable"}'
        ELSE NULL
    END,
    CASE WHEN record_seed.record_status = 'FAILED' THEN '回调返回非成功状态码: 500' ELSE NULL END,
    CASE
        WHEN record_seed.record_status = 'PENDING' THEN NULL
        ELSE TIMESTAMPADD(SECOND, 1, TIMESTAMPADD(MINUTE, record_seed.trigger_offset, NOW()))
    END,
    CASE
        WHEN record_seed.record_status IN ('SUCCESS', 'FAILED')
            THEN TIMESTAMPADD(MICROSECOND, record_seed.duration * 1000,
                TIMESTAMPADD(SECOND, 1, TIMESTAMPADD(MINUTE, record_seed.trigger_offset, NOW())))
        ELSE NULL
    END,
    record_seed.duration,
    TIMESTAMPADD(MINUTE, record_seed.created_offset, NOW()),
    TIMESTAMPADD(MINUTE, record_seed.created_offset, NOW())
FROM (
    SELECT
        seed.seed_no,
        seed.app,
        seed.name,
        CONCAT(seed.job, '-run-', run.slot) AS run_id,
        CASE
            WHEN run.slot = 1 THEN 'SUCCESS'
            WHEN run.slot = 2 AND MOD(seed.seed_no, 4) = 0 THEN 'FAILED'
            WHEN run.slot = 2 THEN 'SUCCESS'
            WHEN MOD(seed.seed_no, 6) = 0 THEN 'PENDING'
            WHEN MOD(seed.seed_no, 6) = 1 THEN 'RUNNING'
            WHEN MOD(seed.seed_no, 6) = 2 THEN 'FAILED'
            ELSE 'SUCCESS'
        END AS record_status,
        CASE
            WHEN run.slot = 1 THEN -(seed.seed_no + 45)
            WHEN run.slot = 2 THEN -(seed.seed_no + 18)
            WHEN MOD(seed.seed_no, 6) = 0 THEN seed.seed_no + 15
            ELSE -seed.seed_no
        END AS trigger_offset,
        CASE
            WHEN run.slot = 1 THEN -(seed.seed_no + 45)
            WHEN run.slot = 2 THEN -(seed.seed_no + 18)
            ELSE -MOD(seed.seed_no, 10)
        END AS created_offset,
        CASE
            WHEN run.slot = 3 AND MOD(seed.seed_no, 6) IN (0, 1) THEN 0
            WHEN run.slot = 1 THEN 80 + seed.seed_no * 13
            WHEN run.slot = 2 THEN 160 + seed.seed_no * 21
            ELSE 220 + seed.seed_no * 17
        END AS duration
    FROM demo_timer_seed AS seed
    CROSS JOIN (
        SELECT 1 AS slot
        UNION ALL SELECT 2
        UNION ALL SELECT 3
    ) AS run
) AS record_seed
JOIN timer_definitions AS definition
    ON definition.app = record_seed.app
    AND definition.name = record_seed.name
    AND definition.status != 'DELETED'
LEFT JOIN timer_records AS existing
    ON existing.timer_id = definition.id
    AND existing.request_body = CONCAT('{"dataset":"screenshot-seed","run_id":"', record_seed.run_id, '"}')
WHERE existing.id IS NULL;

UPDATE timer_records
SET duration = 0,
    response_code = 0,
    response_body = NULL,
    error_message = NULL,
    finished_at = NULL,
    updated_at = NOW()
WHERE request_body LIKE '%"dataset":"screenshot-seed"%'
    AND status IN ('PENDING', 'RUNNING');

DROP TEMPORARY TABLE demo_timer_seed;

COMMIT;
