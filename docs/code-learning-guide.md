# ChronoFlow 重点代码学习指南

本文按一次任务从创建到回调完成的顺序，标出 API、Scheduler、Dispatcher、Worker 与共享基础设施中的关键调用。阅读代码时，优先沿着每节的调用链向下跟踪，不必从所有文件开始。

## 1. 全局入口与生命周期

四个角色的入口结构相同：

```text
cmd/<role>/main.go
  → launcher.Main(...)
  → launcher.Run(...)
  → app.New<Role>(cfg)
  → application.Run(ctx)
```

| 代码 | 重点 |
| --- | --- |
| [`cmd/api/main.go`](../cmd/api/main.go) | API 入口，传入 `app.NewAPI`。 |
| [`cmd/scheduler/main.go`](../cmd/scheduler/main.go) | Scheduler 入口，传入 `app.NewScheduler`。 |
| [`cmd/dispatcher/main.go`](../cmd/dispatcher/main.go) | Dispatcher 入口，传入 `app.NewDispatcher`。 |
| [`cmd/worker/main.go`](../cmd/worker/main.go) | Worker 入口，传入 `app.NewWorker`。 |
| [`internal/launcher/launcher.go`](../internal/launcher/launcher.go) | 加载配置、初始化日志、监听 `SIGINT`/`SIGTERM`，调用角色的 `Application.Run`。 |
| [`internal/app/application.go`](../internal/app/application.go) | 初始化 MySQL、启动后台组件与 HTTP 服务、协调优雅关闭。 |

`Application.Run` 会遍历 `background` 列表，为每个后台组件启动 goroutine；不同角色通过各自的 `NewAPI`、`NewScheduler`、`NewDispatcher`、`NewWorker` 把不同组件放进这个列表。

## 2. API：创建、激活与查询 Timer

API 是任务定义的管理入口。它把 HTTP 请求转换为 `timer_definitions` 表中的数据和状态变化。

```text
POST /api/v1/timers
  → TimerHandler.CreateTimer
  → TimerService.Create
  → TimerDefinitionRepository.Create
  → INSERT timer_definitions (status=INACTIVE)

POST /api/v1/timers/:id/activate
  → TimerHandler.ActivateTimer
  → TimerService.Activate
  → CronParser.NextTriggerTime
  → TimerDefinitionRepository.UpdateScheduleState
  → status=ACTIVE, next_fire_at=下一次触发点
```

### 关键文件与调用

| 阅读位置 | 关键调用 | 需要理解的内容 |
| --- | --- | --- |
| [`internal/app/api.go`](../internal/app/api.go) | `NewAPI` → `configureAPIHTTP` | 装配 Timer Repository、Timer Service、Execution 查询 Repository 与 HTTP Handler。 |
| [`internal/app/api_http.go`](../internal/app/api_http.go) | `newAPIHTTPHandler` | 注册业务路由、`/health`、`/ready`、`/metrics` 和前端静态资源。 |
| [`internal/handler/timer_handler.go`](../internal/handler/timer_handler.go) | `CreateTimer`、`ActivateTimer`、`DeactivateTimer` | HTTP 参数绑定、状态码和 Service 调用边界。 |
| [`internal/service/timer_service.go`](../internal/service/timer_service.go) | `Create` | 校验 Cron、回调 URL、misfire 策略；构造 Timer 定义。 |
| [`internal/service/timer_service.go`](../internal/service/timer_service.go) | `Activate` | 计算并保存权威的第一个 `next_fire_at`。 |
| [`internal/repository/timer_definition_repo.go`](../internal/repository/timer_definition_repo.go) | `Create`、`UpdateScheduleState` | Timer 定义的 MySQL 写入和状态转换条件。 |

### 学习重点

1. `Create` 只保存 Timer 定义，初始状态是 `INACTIVE`。
2. `Activate` 计算 Cron 的下一次触发点，并将 `status` 更新为 `ACTIVE`。
3. Scheduler 只扫描 `ACTIVE` 且 `next_fire_at <= now` 的 Timer，因此 `next_fire_at` 是调度入口字段。
4. 创建 Execution 时会保存回调请求快照；后续 Worker 使用快照执行回调。

## 3. Scheduler：从到期 Timer 生成 Execution 与 Outbox

Scheduler 是 MySQL 权威调度器。它从 `timer_definitions` 读取到期任务，在同一个事务中写入 Execution、Outbox 和新的 `next_fire_at`。

```text
Scheduler.Start
  → schedule（启动立即执行一次，之后按 poll_interval 循环）
  → DueTimerRepository.ScheduleDueBatch
      → SELECT ... FOR UPDATE SKIP LOCKED
      → resolveTimer
      → INSERT timer_executions (PENDING)
      → INSERT outbox_events (EXECUTION_READY)
      → UPDATE timer_definitions.next_fire_at
  → COMMIT
```

### 关键文件与调用

| 阅读位置 | 关键调用 | 需要理解的内容 |
| --- | --- | --- |
| [`internal/app/scheduler.go`](../internal/app/scheduler.go) | `NewScheduler` → `configureScheduler` | 创建 Scheduler 与 Reconciler，并注册为后台组件。 |
| [`internal/service/scheduler.go`](../internal/service/scheduler.go) | `Start` | 启动立即调用一次 `schedule`，随后按 `scheduler.poll_interval_ms` 使用 ticker 执行。 |
| [`internal/service/scheduler.go`](../internal/service/scheduler.go) | `schedule` | 调用 Repository 完成一个调度批次，并记录指标。 |
| [`internal/service/scheduler.go`](../internal/service/scheduler.go) | `resolveTimer` | 基于 Cron 和 misfire 策略计算本轮触发点和新的 `next_fire_at`。 |
| [`internal/repository/scheduler_repo.go`](../internal/repository/scheduler_repo.go) | `ScheduleDueBatch` | 调度事务的核心代码。 |
| [`internal/pkg/cron/parser.go`](../internal/pkg/cron/parser.go) | `NextTriggerTime` | 六字段 Cron 的解析和下一次时间计算。 |

### `ScheduleDueBatch` 内部阅读顺序

1. 查询使用 `FOR UPDATE SKIP LOCKED` 领取 `ACTIVE` 且到期的 Timer。
2. `resolveTimer` 返回本轮触发点列表 `occurrences` 与新的 `nextFireAt`。
3. 遍历 `occurrences`，为每个 `scheduled_at` 创建 `PENDING` Execution。
4. 唯一键 `(timer_id, scheduled_at)` 约束同一计划触发点。
5. 为新建 Execution 创建 `EXECUTION_READY` Outbox，`aggregate_id` 等于 `execution.ID`。
6. 使用 Timer 的 `version` 乐观锁推进 `next_fire_at`。
7. 事务提交后，Dispatcher 才能读取本次 Outbox。

### Reconciler

Scheduler 进程还启动 Reconciler：

| 阅读位置 | 关键调用 | 作用 |
| --- | --- | --- |
| [`internal/service/reconciler.go`](../internal/service/reconciler.go) | `Reconciler.Start` | 启动立即执行恢复扫描，随后按 `recovery.scan_interval_seconds` 循环。 |
| [`internal/service/reconciler.go`](../internal/service/reconciler.go) | `reconcile` | 扫描 `PENDING`、`RETRY_WAIT`、Lease 过期的 `RUNNING` Execution。 |
| [`internal/repository/recovery_repo.go`](../internal/repository/recovery_repo.go) | `RecoverBatch` | 创建 `EXECUTION_RECOVERY` Outbox，或更新 Execution 状态。 |
| [`internal/service/reconciler.go`](../internal/service/reconciler.go) | `cleanup` | 按保留周期清理已结束 Execution 和已发布 Outbox。 |

## 4. Dispatcher：从 Outbox 发布到 Redis Stream

Dispatcher 将已经提交到 MySQL 的 Outbox 事件发布为 Redis Stream 消息。

```text
OutboxDispatcher.Start
  → dispatchOnce（启动立即执行一次，之后按 outbox.poll_interval_ms 循环）
  → OutboxRepository.ClaimBatch
      → SELECT ... FOR UPDATE SKIP LOCKED
      → 写入 claim_owner / claim_until
  → StreamPublisher.Publish
      → Redis XADD
  → OutboxRepository.MarkPublished
      → 写入 published_at / published_message_id
```

### 关键文件与调用

| 阅读位置 | 关键调用 | 需要理解的内容 |
| --- | --- | --- |
| [`internal/app/dispatcher.go`](../internal/app/dispatcher.go) | `NewDispatcher` | 初始化 MySQL、Redis、指标与 Dispatcher HTTP 服务。 |
| [`internal/app/dispatcher.go`](../internal/app/dispatcher.go) | `configureDispatcher` | 调用 `EnsureConsumerGroup`，创建 Dispatcher 后台组件。 |
| [`internal/service/outbox_dispatcher.go`](../internal/service/outbox_dispatcher.go) | `Start` | Dispatcher 的轮询入口。 |
| [`internal/service/outbox_dispatcher.go`](../internal/service/outbox_dispatcher.go) | `dispatchOnce` | 领取 Outbox、发布 Stream、写回发布状态或重试时间。 |
| [`internal/repository/outbox_repo.go`](../internal/repository/outbox_repo.go) | `ClaimBatch` | 使用 MySQL 行锁和 Claim 字段领取事件。 |
| [`internal/repository/outbox_repo.go`](../internal/repository/outbox_repo.go) | `MarkPublished`、`MarkFailed` | 保存 Redis 消息 ID，或保存退避后的 `next_attempt_at`。 |
| [`internal/pkg/redis/stream.go`](../internal/pkg/redis/stream.go) | `StreamPublisher.Publish` | 使用 Redis `XADD` 写入 `event_id`、`execution_id`、`event_type`、`payload`。 |

### 关键状态字段

| 字段 | Dispatcher 写入时机 |
| --- | --- |
| `claim_owner`、`claim_until` | `ClaimBatch` 成功领取 Outbox 后。 |
| `published_at`、`published_message_id` | Redis `XADD` 成功且 MySQL 发布确认成功后。 |
| `attempts`、`next_attempt_at`、`last_error` | Redis 发布失败后。 |

先理解 `ClaimBatch`，再理解 `Publish` 和 `MarkPublished`，就能掌握 MySQL 到 Redis 的可靠投递路径。

## 5. Worker：消费 Stream、抢占 Lease、执行 HTTP 回调

Worker 从 Redis Consumer Group 读取消息，先在 MySQL 领取 Execution，再把回调工作提交给 ants 池。

```text
StreamWorker.Start
  → AutoClaim：接管空闲 Pending 消息
  → ReadNew：XREADGROUP 读取新消息
  → submit：提交 ants 池
  → process
      → TimerExecutionRepository.Claim
      → CallbackClient.Execute
      → CompleteSuccess 或 CompleteFailure
      → XACK
```

### 关键文件与调用

| 阅读位置 | 关键调用 | 需要理解的内容 |
| --- | --- | --- |
| [`internal/app/worker.go`](../internal/app/worker.go) | `NewWorker`、`configureWorker` | 初始化 Redis Consumer Group、ants 池、Worker 和 Stream 清理器。 |
| [`internal/service/stream_worker.go`](../internal/service/stream_worker.go) | `Start` | 按 `reclaim_interval_seconds` 调用 `AutoClaim`，并调用 `ReadNew` 读取新消息。 |
| [`internal/service/stream_worker.go`](../internal/service/stream_worker.go) | `submit` | 将消息处理函数提交到 ants 池；`worker.pool_size` 控制单进程回调并发量。 |
| [`internal/service/stream_worker.go`](../internal/service/stream_worker.go) | `process` | 解码消息、抢占 Execution、启动 Lease 心跳、执行回调、持久化结果、确认 Stream 消息。 |
| [`internal/repository/timer_execution_repo.go`](../internal/repository/timer_execution_repo.go) | `Claim` | 原子更新 Execution 为 `RUNNING`，增加 `attempt`，写入 Lease 与 `run_token`。 |
| [`internal/service/stream_worker.go`](../internal/service/stream_worker.go) | `heartbeat` | 按 `heartbeat_seconds` 续租 `lease_until`。 |
| [`internal/pkg/callback/client.go`](../internal/pkg/callback/client.go) | `Execute` | 构建 HTTP 请求、校验回调地址、设置稳定幂等标识、读取响应。 |
| [`internal/repository/timer_execution_repo.go`](../internal/repository/timer_execution_repo.go) | `CompleteSuccess`、`CompleteFailure` | 将结果写入 MySQL；可重试失败会创建新的重试 Outbox。 |
| [`internal/pkg/redis/stream.go`](../internal/pkg/redis/stream.go) | `StreamConsumer.Ack` | 通过 Redis `XACK` 将消息从 Consumer Group Pending 列表移除。 |

### Worker 的状态转换

```text
PENDING
  → Claim
RUNNING
  → HTTP 2xx + CompleteSuccess → SUCCESS
  → 可重试错误 + CompleteFailure → RETRY_WAIT + EXECUTION_RETRY Outbox
  → 其他失败或达到最大次数 → FAILED
```

`Claim`、`Heartbeat`、`CompleteSuccess` 和 `CompleteFailure` 都匹配 `lease_owner` 与 `run_token`。阅读这些 SQL 条件时，要把它们视为 Worker 写入执行结果的所有权校验。

## 6. 共享基础设施

### 6.1 MySQL Repository 与事务边界

Repository 层集中保存持久化逻辑：

| 文件 | 重点 |
| --- | --- |
| [`internal/repository/database.go`](../internal/repository/database.go) | 创建 GORM/MySQL 连接池。 |
| [`internal/repository/scheduler_repo.go`](../internal/repository/scheduler_repo.go) | Timer → Execution + Outbox 的事务边界。 |
| [`internal/repository/outbox_repo.go`](../internal/repository/outbox_repo.go) | Outbox Claim、发布确认与发布失败记录。 |
| [`internal/repository/timer_execution_repo.go`](../internal/repository/timer_execution_repo.go) | Execution Lease、结果状态与重试 Outbox。 |
| [`internal/repository/recovery_repo.go`](../internal/repository/recovery_repo.go) | 恢复扫描与历史清理。 |

阅读 Repository 时先找 `Transaction(...)`、`FOR UPDATE SKIP LOCKED`、`Where(...)` 中的状态条件和 `RowsAffected` 检查；这些位置决定了分布式并发下的数据所有权。

### 6.2 Redis Streams

[`internal/pkg/redis/stream.go`](../internal/pkg/redis/stream.go) 将 go-redis 操作封装为四类调用：

| 方法 | Redis 命令 | 调用方 |
| --- | --- | --- |
| `EnsureConsumerGroup` | `XGROUP CREATE ... MKSTREAM` | Dispatcher、Worker 启动阶段。 |
| `Publish` | `XADD` | Dispatcher。 |
| `ReadNew` | `XREADGROUP ... >` | Worker。 |
| `AutoClaim` | `XAUTOCLAIM` | Worker。 |
| `Ack` | `XACK` | Worker 在 MySQL 结果写入后。 |
| `TrimAcknowledgedBefore` | `XPENDING` + `XTRIM MINID` | Worker 内的 Stream 清理器。 |

### 6.3 ants 并发池

[`internal/pkg/pool/pool.go`](../internal/pkg/pool/pool.go) 封装 ants。Worker 在 [`internal/app/worker.go`](../internal/app/worker.go) 创建池，并在 `StreamWorker.submit` 中调用 `pool.Submit`。每个 Worker 进程的最大并发由 `worker.pool_size` 控制。

### 6.4 HTTP Callback 与幂等标识

[`internal/pkg/callback/client.go`](../internal/pkg/callback/client.go) 的 `Client.Execute` 负责：

1. 校验回调 URL 和解析后的 IP；
2. 构建 HTTP 请求和回调头；
3. 设置 `Idempotency-Key: chronoflow-execution-<execution_id>`；
4. 设置 `X-ChronoFlow-Execution-ID: <execution_id>`；
5. 发送请求并将 2xx、非 2xx、网络错误转为 Worker 可处理的结果。

回调服务可使用这两个稳定标识将同一 Execution 的重复请求映射到同一业务处理结果。

### 6.5 配置、迁移与可观测性

| 文件 | 重点 |
| --- | --- |
| [`config/config.yaml`](../config/config.yaml) | 轮询、Lease、重试、恢复和清理的默认参数。 |
| [`internal/config/config.go`](../internal/config/config.go) | `CHRONOFLOW_*` 环境变量覆盖与参数归一化。 |
| [`internal/migration/cli.go`](../internal/migration/cli.go) | `up`、`down`、`version`、`force` 命令入口。 |
| [`internal/migration/migrator.go`](../internal/migration/migrator.go) | `golang-migrate` 的文件源和 MySQL 数据库驱动。 |
| [`migrations/00001_init.up.sql`](../migrations/00001_init.up.sql) | 三张核心表、唯一键和查询索引。 |
| [`internal/pkg/metrics/reporter.go`](../internal/pkg/metrics/reporter.go) | Scheduler、Outbox、Worker、Recovery 的 Prometheus 指标定义。 |

## 7. 推荐阅读顺序

1. [`internal/app/application.go`](../internal/app/application.go)：理解角色如何启动和关闭。
2. [`internal/service/timer_service.go`](../internal/service/timer_service.go)：理解 Timer 从创建到激活。
3. [`internal/service/scheduler.go`](../internal/service/scheduler.go) 与 [`internal/repository/scheduler_repo.go`](../internal/repository/scheduler_repo.go)：理解 Execution 与 Outbox 如何生成。
4. [`internal/service/outbox_dispatcher.go`](../internal/service/outbox_dispatcher.go) 与 [`internal/repository/outbox_repo.go`](../internal/repository/outbox_repo.go)：理解 MySQL 到 Redis 的投递。
5. [`internal/service/stream_worker.go`](../internal/service/stream_worker.go) 与 [`internal/repository/timer_execution_repo.go`](../internal/repository/timer_execution_repo.go)：理解 Lease、重试和回调结果写入。
6. [`internal/service/reconciler.go`](../internal/service/reconciler.go)：理解故障恢复与清理。
7. [`e2e/flow_e2e_test.go`](../e2e/flow_e2e_test.go)：将完整调用链与可执行测试对应起来。

完成上述顺序后，再回到 [`docs/roles-and-runtime.md`](roles-and-runtime.md) 对照各角色的轮询频率、状态字段和故障处理路径。
