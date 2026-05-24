# 系统架构详解

## 整体架构

ChronoFlow 核心思想是**时间分片 + 分桶并发 + 三级存储**。

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

**触发时机**：每隔 `step1_duration`（默认 60 分钟）执行一次。启动时不立即执行，等第一次 ticker 触发；冷启动期间的任务由 Trigger 的 DB 回退机制兜底。

**核心流程**：
1. 全量扫描 `timer_definitions` 表中 `status = ACTIVE` 的记录
2. 对每个定时器，用 Cron 解析器计算未来 `step1_duration` 内的所有触发时间点
3. 时间窗口使用小时级取整：`start = GetStartHour(now + step)`, `end = GetStartHour(now + 2*step)`
4. 幂等检查：跳过已存在的记录（`timer_id + trigger_time` 唯一）
5. 批量插入 `timer_records` 表
6. 按 `{time_range}:{bucket}` 分组，批量 ZAdd 到 Redis
   - `time_range`：分钟级时间窗口，格式 `YYYY-MM-DD-HH:mm`，由触发时间向下取整得到
   - `bucket`：分桶号，`timer_id % bucket_num`

**分桶规则**：`bucket = timer_id % bucket_num`

### 2. Scheduler（调度器）

**职责**：每秒轮询，为每个二维分片抢分布式锁，抢到后提交 Trigger 到协程池。

**二维分片**：由 `(time_range, bucket)` 组成的二维矩阵，同一时刻不同桶可并行处理，不同节点可认领不同分片。

**核心流程**：
1. 计算当前分钟级时间范围 `time_range = YYYY-MM-DD-HH:mm`
2. 同时处理当前分钟和上一分钟，避免边界任务遗漏
3. 遍历桶号 `0 ~ bucket_num-1`
4. 对每个桶 `SETNX` 抢锁，TTL = `scan_interval * 2`（见下方说明）
5. 抢到锁 → 从 ants 协程池提交 Trigger 协程

**为什么 TTL = scan_interval * 2？**

- `scan_interval` 是 Scheduler 的轮询间隔（默认 1 秒）
- `scan_interval * 2 = 2 秒`，即锁的过期时间
- **为什么是 2 倍而非 1 倍？** 假设 TTL = 1 秒，当 Scheduler 在第 0.9 秒抢到锁，但 Trigger 还没来得及续期，锁就过期了，其他节点可能抢占同一分片
- **2 倍的作用**：确保在下一轮轮询（1 秒后）到来前，锁不会过期。即使第一轮抢到锁后 Trigger 启动稍慢，也有足够时间续期
- **避免脑裂**：如果两个节点同时持有同一分片的锁，会导致同一任务被重复执行。2 倍 TTL 保证锁的生命周期覆盖整个轮询周期

### 3. Trigger（触发器）

**职责**：在时间片内持续轮询 Redis ZSet，读取到期任务提交 Executor。Redis 无结果时回退查询 MySQL（冷启动兜底）。

**核心流程**：
1. 计算时间片结束时间（`time_range` 对应分钟的 `:59`，如 `10:30` → `10:30:59`）
2. 循环调用 `GetDueTasks`（`ZRANGEBYSCORE` 读取到期任务，只读不删）
3. 若 Redis 无结果，回退查询 MySQL 中 `status = PENDING` 的记录（冷启动兜底）
4. 对每个到期任务，从协程池提交 Executor 协程
5. 续期锁 TTL（见下方说明）
6. 时间片结束后退出（见下方说明）

**时间片结束**：每个 Trigger 负责处理一分钟内某个桶的任务，`10:30` 时间片的结束时间是 `10:30:59`。到达该时刻后，当前 Trigger 退出，由下一分钟的 Scheduler 轮询启动新的 Trigger。

**续期锁 TTL**：Trigger 在循环中定期延长锁的过期时间，防止锁在任务处理完前过期被其他节点抢占。锁的初始 TTL 为 `scan_interval * 2`（如 2 秒），每次循环续期为相同值。

**防重复机制**：任务保留在 ZSet 中，依赖分布式锁（`time_range + bucket` 维度）保证同一分片同一时刻只有一个节点处理。进程崩溃后任务仍在 ZSet 中，下次轮询可重新捞取。

### 4. Executor（执行器）

**职责**：执行单个定时任务，包含两层幂等去重。

**核心流程**：
1. **Bloom Filter 查重** → miss → 继续执行
2. **Bloom 命中** → MySQL 查记录状态确认（`status != PENDING` 则跳过）
3. 查询定时器定义（先内存缓存，miss 再 MySQL）
4. 检查定时器状态（INACTIVE/DELETED 则跳过）
5. 更新记录状态为 RUNNING
6. 执行 HTTP 回调
7. 根据结果更新状态（SUCCESS/FAILED/RETRYING）
8. 执行成功 → Bloom Filter 打点（仅成功时写入，失败任务可重试）

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

| 级别 | 存储 | 用途 |
|------|------|------|
| L1 | MySQL | 持久化、全量数据 |
| L2 | Redis ZSet | 任务队列、分布式协调 |
| L3 | 节点内存 | 热点定时器定义缓存 |

Migrator 负责 L1→L2 迁移，Executor 查询时自动 L3→L2→L1 回源。
