# 系统架构详解

## 整体架构

ChronoFlow 采用 xTimer 架构，核心思想是**时间分片 + 分桶并发 + 三级存储**。

### 架构分层

```
┌─────────────────────────────────────────────────┐
│                  HTTP API 层                      │
│         Gin + Handler + Middleware                │
├─────────────────────────────────────────────────┤
│                  业务服务层                        │
│    TimerService (CRUD) + 状态机                   │
├─────────────────────────────────────────────────┤
│                  调度引擎层                        │
│  Migrator → Scheduler → Trigger → Executor       │
├─────────────────────────────────────────────────┤
│                  基础设施层                        │
│  Cron Parser | Bloom Filter | Memory Cache       │
│  Redis Queue | Retry Calculator | Metrics        │
│  Worker Pool (ants)                               │
├─────────────────────────────────────────────────┤
│                  数据存储层                        │
│       MySQL (持久化) + Redis (队列) + Memory (缓存)│
└─────────────────────────────────────────────────┘
```

## 四大核心模块

### 1. Migrator（迁移器）

**职责**：将 MySQL 中的定时器定义批量预创建为执行记录，并推送到 Redis ZSet 队列。

**触发时机**：每隔 `step1_duration`（默认 60 分钟）执行一次。

**核心流程**：
1. 全量扫描 `timer_definitions` 表中 `status = ACTIVE` 的记录
2. 对每个定时器，用 Cron 解析器计算未来 `step1_duration` 内的所有触发时间点
3. 幂等检查：跳过已存在的记录（`timer_id + trigger_time` 唯一）
4. 批量插入 `timer_records` 表
5. 按 `{time_range}:{bucket}` 分组，批量 ZAdd 到 Redis

**分桶规则**：`bucket = timer_id % bucket_num`

### 2. Scheduler（调度器）

**职责**：每秒轮询，为每个二维分片抢分布式锁，抢到后提交 Trigger 到协程池。

**核心流程**：
1. 计算当前分钟级时间范围 `time_range = YYYY-MM-DD-HH:mm`
2. 遍历桶号 `0 ~ bucket_num-1`
3. 对每个桶 `SETNX` 抢锁，TTL = `scan_interval * 2`
4. 抢到锁 → 从 ants 协程池提交 Trigger 协程

### 3. Trigger（触发器）

**职责**：在时间片内持续轮询 Redis ZSet，取出到期任务提交 Executor。

**核心流程**：
1. 计算时间片结束时间（当前分钟的 59 秒）
2. 循环调用 `PopDueTasks`（Lua 脚本原子弹出）
3. 对每个到期任务，从协程池提交 Executor 协程
4. 续期锁 TTL
5. 时间片结束后退出

**Lua 脚本保证原子性**：
```lua
local members = redis.call('ZRANGEBYSCORE', key, '-inf', now, 'LIMIT', 0, count)
for i, member in ipairs(members) do
    redis.call('ZREM', key, member)
end
return members
```

### 4. Executor（执行器）

**职责**：执行单个定时任务，包含三层幂等去重。

**核心流程**：
1. **Bloom Filter 查重** → miss → 继续执行
2. **Bloom 命中** → Redis 幂等键检查
3. **Redis 也命中** → MySQL 查重确认
4. 查询定时器定义（先内存缓存，miss 再 MySQL）
5. 检查定时器状态（INACTIVE/DELETED 则跳过）
6. 更新记录状态为 RUNNING
7. 执行 HTTP 回调
8. 根据结果更新状态（SUCCESS/FAILED/RETRYING）
9. Bloom Filter 打点 + Redis 幂等键设置

## 数据模型

### timer_definitions（定时器定义）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT PK | 定时器 ID |
| app | VARCHAR(128) | 应用名 |
| name | VARCHAR(128) | 定时器名称 |
| cron_expr | VARCHAR(64) | Cron 表达式 |
| callback_url | VARCHAR(512) | 回调 URL |
| callback_method | VARCHAR(16) | HTTP 方法 |
| callback_body | TEXT | 请求体 |
| callback_headers | TEXT | 请求头 JSON |
| status | VARCHAR(32) | ACTIVE/INACTIVE/DELETED |
| timeout | INT | 超时秒数 |
| max_retries | INT | 最大重试次数 |

### timer_records（执行记录）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT PK | 记录 ID |
| timer_id | BIGINT FK | 定时器 ID |
| trigger_time | DATETIME | 计划触发时间 |
| status | VARCHAR(32) | PENDING/RUNNING/SUCCESS/FAILED/RETRYING/TIMEOUT |
| retry_count | INT | 已重试次数 |
| request_url | VARCHAR(512) | 实际请求 URL |
| response_code | INT | HTTP 响应码 |
| response_body | TEXT | 响应体 |
| error_message | TEXT | 错误信息 |
| duration | BIGINT | 执行耗时（毫秒） |
| next_retry_time | DATETIME | 下次重试时间 |

## 设计决策

### 为什么用 ants 协程池而不是 MQ？

| 方案 | 优点 | 缺点 |
|------|------|------|
| Apache Pulsar MQ | 高吞吐、持久化 | 接入成本高、运维复杂 |
| **ants 协程池** | **低延迟、零依赖、简单** | 单进程内通信 |

ChronoFlow 选择 ants 协程池，因为：
1. 模块间通信在同一进程内完成，无需跨进程
2. 减少 IO 交互，延迟更低
3. 降低外部依赖，仅需 MySQL + Redis

### 为什么用三级存储？

| 级别 | 存储 | 用途 | 访问速度 |
|------|------|------|----------|
| L1 | MySQL | 持久化、全量数据 | 慫（ms 级） |
| L2 | Redis ZSet | 任务队列、分布式协调 | 快（sub-ms） |
| L3 | 节点内存 | 热点定时器定义缓存 | 最快（ns 级） |

Migrator 负责 L1→L2 迁移，Executor 查询时自动 L3→L2→L1 回源。
