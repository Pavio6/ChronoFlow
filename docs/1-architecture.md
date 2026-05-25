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
│  Redis Queue | Metrics                           │
│  Worker Pool (ants)                               │
├─────────────────────────────────────────────────┤
│                  数据存储层                        │
│       MySQL (持久化) + Redis (队列) + Memory (缓存)│
└─────────────────────────────────────────────────┘
```

## 四大核心模块

### 1. Migrator（迁移器）

**职责**：将 MySQL 中的定时器定义批量预创建为执行记录，并推送到 Redis ZSet 队列。

**触发时机**：每隔 `migrate_step_minutes`（默认 60 分钟）执行一次。启动时不立即执行，等第一次 ticker 触发；冷启动期间的任务由 Trigger 的 DB 回退机制兜底。

**核心流程**：
1. 全量扫描 `timer_definitions` 表中 `status = ACTIVE` 的记录
2. 对每个定时器，用 Cron 解析器计算未来 `migrate_step_minutes` 内的所有触发时间点
3. 时间窗口使用小时级取整：`start = GetStartHour(now + step)`, `end = GetStartHour(now + 2*step)`
4. 幂等检查：跳过已存在的记录（`timer_id + trigger_time` 唯一）
5. 批量插入 `timer_records` 表
6. 按 `{time_range}:{bucket}` 分组，批量 ZAdd 到 Redis
   - `time_range`：分钟级时间窗口，格式 `YYYY-MM-DD-HH:mm`，由触发时间向下取整得到
   - `bucket`：分桶号，`timer_id % bucket_num`

**分桶规则**：`bucket = timer_id % bucket_num`

### 2. Scheduler（调度器）

**职责**：每秒轮询，为每个二维分片提交 worker；worker 抢到分布式锁后执行 Trigger。

**二维分片**：由 `(time_range, bucket)` 组成的二维矩阵，同一时刻不同桶可并行处理，不同节点可认领不同分片。

**核心流程**：
1. 计算当前分钟级时间范围 `time_range = YYYY-MM-DD-HH:mm`
2. 同时处理当前分钟和上一分钟，避免边界任务遗漏
3. 分别获取各分钟的动态分桶数（不同分钟可能不同）
4. 对每个桶向 ants 协程池提交分片处理 worker
5. worker 以 `processID_goroutineID` token 执行 `SET NX` 抢锁，TTL = `lock_expiration`（默认 70 秒）
6. 抢到锁 → 在同一 worker 中运行 Trigger

**锁策略（参考 xTimer）**：

- 初始锁 TTL = `lock_expiration`（默认 70 秒），确保覆盖整个时间片（60 秒）且有余量
- Trigger 成功扫描完整分片后将锁 TTL 设置为 `success_expiration`（默认 130 秒），覆盖上一分钟回扫窗口
- 锁 value = `GetProcessAndGoroutineIDStr()`；续期与释放通过 Lua 校验当前 worker 仍持有该锁
- 锁粒度：每个 `{time_range}:{bucket}` 一把锁
- 多实例部署时，同一分片只会被一个实例处理
- 动态分桶：当前分钟和上一分钟分别获取各自的 bucketNum，避免桶数不一致导致遗漏或越界

#### 为什么同时处理当前分钟和上一分钟？

任务按分钟分片保存，例如 `10:02` 分片表示 `[10:02:00, 10:03:00)`。当前分钟负责正常触发正在到期的任务；上一分钟用于补偿刚跨过分钟边界、但尚未成功派发的任务。

例如一个任务计划在 `10:02:59` 触发：

```
10:02:59  任务到期，但 Scheduler/Trigger 因抢锁失败、协程池拥塞或短暂存储错误未成功派发
10:03:00  时间进入下一分钟
10:03:01  Scheduler 同时扫描 10:03 当前分片和 10:02 上一分片
           └─ 10:02:59 的任务仍可在上一分片补扫中被发现并提交执行
```

若只处理当前分钟，进入 `10:03` 后不会再次扫描 `10:02` 分片，边界附近遗漏的任务将失去补偿机会。

#### 分片锁与协程关系

锁粒度是 `(time_range, bucket)`，而不是整个 Scheduler 或单独的 bucket。当前分钟与上一分钟即使桶号相同，也使用两把不同的锁：

```
chronoflow:scheduler_lock:2026-05-25-10:02:2  // 上一分钟，bucket 2
chronoflow:scheduler_lock:2026-05-25-10:03:2  // 当前分钟，bucket 2
```

因此上一分钟的补扫不会阻塞当前分钟同桶任务的正常触发；不同桶也可以并行执行。若每分钟有 3 个桶，同时关注当前分钟和上一分钟，则最多涉及 6 个独立分片锁。

Scheduler 每次轮询都会为有任务的分片提交 worker 尝试抢锁，因此同一 `(time_range, bucket)` 在一段时间内可能出现多个短生命周期的抢锁协程。但正常情况下，只有一个 worker 可以持有该分片锁并运行 Trigger；其他 worker 抢锁失败后立即退出。只有在持锁节点异常或锁未成功续期而过期后，后续 worker 才可能再次取得该分片，用于故障恢复。

### 3. Trigger（触发器）

**职责**：在时间片内持续轮询 Redis ZSet，读取到期任务提交 Executor。Redis 无结果时回退查询 MySQL（冷启动兜底）。

**核心流程**：
1. 计算分钟半开区间 `[sliceStart, sliceEnd)`
2. 以游标循环调用 `GetTasksByTime(cursor, dueEnd)`，每个扫描窗口互不重叠
3. 若 Redis 无结果，回退查询同一扫描窗口中 `status = PENDING` 的 MySQL 记录
4. 对每个到期任务，从协程池提交 Executor 协程
5. 完整扫描分片后，通过 Lua 校验 owner token 并将锁 TTL 设置为 `success_expiration`

**时间片结束**：每个 Trigger 负责处理一分钟内某个桶的任务，`10:30` 时间片表示 `[10:30:00, 10:31:00)`。如果 Scheduler 在下一分钟补扫该分片，Trigger 会立即处理尚未完成的整个历史区间。

**成功保留 TTL（参考 xTimer）**：初始锁 TTL 为 `lock_expiration`（默认 70 秒），覆盖 60 秒分片处理。分片扫描成功完成后，Trigger 将锁的剩余 TTL 重置为 `success_expiration`（默认 130 秒），避免当前/上一分钟扫描重入；这不表示 HTTP 回调全程持锁。

**防重复机制**：任务保留在 ZSet 中；非重叠读取窗口避免同一次 Trigger 重复派发。分布式锁降低多实例重复派发，数据库原子 `PENDING -> RUNNING` 抢占则保证重复派发不会重复执行回调。

### 4. Executor（执行器）

**职责**：执行单个定时任务，Bloom Filter 加速重复任务过滤，数据库状态抢占保证执行权唯一。

**核心流程**：
1. 查询 Bloom Filter；命中时查询 MySQL 状态确认，已处理任务直接跳过
2. 查询完整的定时器定义（先内存缓存，miss 再 MySQL）
3. 使用定义中的状态判断是否执行（INACTIVE/DELETED 则跳过）
4. 以条件更新原子抢占 `PENDING -> RUNNING`；抢占失败直接跳过
5. 执行 HTTP 回调
6. 根据结果更新状态（SUCCESS/FAILED）
7. 执行成功 → Bloom Filter 打点，为后续重复派发提供快速过滤

**定时器定义约束**：定义创建后不可修改，仅允许激活、停用和删除；如需修改 Cron 或回调配置，删除原定义并新建定时器。停用或删除仅修改 MySQL 状态，不移除已经写入 Redis ZSet 的任务点。Executor 使用带状态的节点本地定义缓存判断是否执行，以减少完整定义回源；因此状态变更采用最终一致性，最晚在一个 `step2_duration` 缓存周期后反映到已经缓存该定时器的执行节点。

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

### timer_records（执行记录）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT PK | 记录 ID |
| timer_id | BIGINT FK | 定时器 ID |
| trigger_time | DATETIME | 计划触发时间 |
| status | VARCHAR(32) | PENDING/RUNNING/SUCCESS/FAILED/TIMEOUT |
| request_url | VARCHAR(512) | 实际请求 URL |
| response_code | INT | HTTP 响应码 |
| response_body | TEXT | 响应体 |
| error_message | TEXT | 错误信息 |
| duration | BIGINT | 执行耗时（毫秒） |

唯一约束：`uk_timer_trigger_time(timer_id, trigger_time)`，从存储层阻止相同触发点重复建记录。

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
| L3 | 节点内存 | 热点定时器完整定义及状态缓存 |

Migrator 负责 L1→L2 迁移，Executor 查询定时器定义时优先读取 L3、未命中再回源 L1。

L3 缓存采用按需加载：Executor 首次读取到 `ACTIVE` 定义时写入本地缓存，有效期由 `scheduler.step2_duration` 配置（默认 120 秒）。读取到过期条目时立即删除并回源 MySQL；后台也按相同周期清理不再被访问的过期条目。单节点缓存容量上限为 10000 条，达到上限时优先淘汰最早过期的条目。
