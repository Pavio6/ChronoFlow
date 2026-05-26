# 双协程池与 HTTP 回调超时改造设计

## 实施状态

该设计已实施。当前代码使用 `schedulerPool` 承载分片扫描与 `Trigger.Run`，使用 `triggerPool` 承载 `Executor.Execute`，HTTP 回调固定超时为 `12s`。配置项为 `scheduler.worker_pool_size` 与 `trigger.worker_pool_size`。

## 1. 改造目标

改造前，ChronoFlow 在 `Scheduler -> Trigger -> Executor` 执行主链路中共享一个 `ants` 协程池：

```text
Shared Pool
  |- Scheduler 分片处理与 Trigger.Run
  `- Executor HTTP 回调执行
```

`Trigger.Run` 会在一个分钟时间片内持续扫描任务，HTTP 回调也可能长时间占用 worker。共享资源池会使分片扫描和回调执行竞争同一组 worker，在回调阻塞或集中触发时影响后续调度。

本次改造目标为：

1. 参考 xTimer 的主链路结构，将调度扫描和回调执行拆分到两个独立协程池。
2. 为 HTTP 回调客户端设置固定 `12s` 超时，限制异常下游长期占用执行池资源。
3. 保留 ChronoFlow 已实现的动态分桶、MySQL 补偿扫描与数据库原子抢占防重能力。

## 2. 参考 xTimer 的边界

xTimer 的调度执行链路为三个模块、两个协程池：

```text
Scheduler --[Scheduler Pool]--> Trigger --[Trigger Pool]--> Executor
```

- `Scheduler` 创建一个协程池，用于异步处理分钟分片、竞争分片锁并运行 `Trigger`。
- `Trigger` 创建另一个协程池，用于将到期任务提交给 `Executor` 执行。
- `Executor` 本身不创建协程池。

ChronoFlow 按该职责边界拆池，但不复制以下 xTimer 行为：

- 不退回固定分桶：ChronoFlow 已支持按分钟任务量动态扩桶。
- 不移除数据库补偿：ChronoFlow 的 Trigger 会合并 MySQL 中的 `PENDING` 记录，覆盖 Redis 部分投递失败。
- 不削弱最终防重：ChronoFlow 已通过执行记录唯一约束和 `PENDING -> RUNNING` 原子抢占保证回调执行权唯一。

## 3. 目标架构

改造后的执行链路：

```text
Migrator

Scheduler --[schedulerPool]--> Trigger --[triggerPool]--> Executor --[12s timeout]--> HTTP Callback
```

### 3.1 `schedulerPool`

归属于 `Scheduler`，负责：

- 当前分钟和上一分钟的分片处理；
- Redis 分片锁竞争；
- 持锁状态下运行 `Trigger.Run`；
- Trigger 完成时间片扫描后的锁续期。

该池保护的是调度扫描吞吐，不承载 HTTP 回调。

### 3.2 `triggerPool`

归属于 `Trigger`，负责：

- 对扫描到期、并与数据库补偿结果合并后的任务提交 `Executor.Execute`；
- 承载 HTTP 回调期间的 worker 占用。

该池容量用于控制外部回调并发，避免回调阻塞影响 Scheduler 的分片扫描资源。

### 3.3 `Executor`

`Executor` 保持无池设计，由 `Trigger` 的池调用。执行逻辑保持：

1. Bloom Filter 快速判断成功任务，并由 MySQL 状态确认；
2. 查询本地缓存或 MySQL 中的任务定义；
3. 仅处理 `ACTIVE` 任务；
4. 原子抢占 `PENDING -> RUNNING`；
5. 发起 HTTP 回调；
6. 更新最终执行结果与监控指标。

## 4. HTTP 回调超时

### 4.1 决策

HTTP 回调客户端设置固定超时：

```go
&http.Client{Timeout: 12 * time.Second}
```

本次不提供配置项。

### 4.2 原因

- 无超时的外部 HTTP 请求可能无限期占用 `triggerPool` worker。
- `12s` 能覆盖当前测试回调服务的 `10s` 慢请求场景，避免将正常的慢回调直接判定为失败。
- 固定值可以控制本次改造范围，避免引入额外配置管理与文档分支。

### 4.3 行为变化

- 回调在 `12s` 内完成时，按 HTTP 响应状态更新记录。
- 回调超过 `12s` 时，客户端返回超时错误，执行记录更新为 `FAILED` 并记录错误信息。
- 超时只限制回调执行周期，不改变 Scheduler 分片锁和 Trigger 扫描窗口语义。

## 5. 配置设计

双池配置按实际持有模块划分，不将 Trigger 的执行池挂在 Executor 配置下：

```yaml
scheduler:
  worker_pool_size: 16
  # 现有 scheduler 配置保持不变

trigger:
  worker_pool_size: 100

executor:
  # 无协程池及超时配置；HTTP 超时固定为 12s
```

### 5.1 容量原则

- `scheduler.worker_pool_size` 负责同时处理活跃分片，默认值不应低于当前及上一分钟可能同时参与扫描的分片量。
- `trigger.worker_pool_size` 控制 HTTP 回调并发，应结合数据库连接池和下游服务承载能力设置。
- 不直接套用 xTimer 的大规模默认并发值；当前项目应从保守容量起步，通过指标和压测再调整。

## 6. 代码改动清单

| 文件 | 改动 |
| --- | --- |
| `internal/config/config.go` | 新增 `TriggerConfig`；在 `SchedulerConfig` 增加 `WorkerPoolSize`；为两个池设置默认值及合法值修正；移除原本表示共享池的 `ExecutorConfig.WorkerPoolSize`。 |
| `config/config.yaml` | 增加 `scheduler.worker_pool_size`、`trigger.worker_pool_size`；移除 `executor.worker_pool_size`。 |
| `config/config.docker.yaml` | 同步双池配置变更。 |
| `internal/pkg/pool/pool.go` | 增加 `WorkerPool` 接口以便注入和测试；保留 `ants` 默认阻塞提交语义；修正池满即返回错误的不准确注释。 |
| `cmd/server/main.go` | 分别创建及释放 `schedulerPool` 和 `triggerPool`；将两个池注入对应模块；启动日志输出各池容量。 |
| `internal/service/scheduler.go` | 持有 `pool.WorkerPool` 类型的 `schedulerPool`，仅提交分片扫描任务。 |
| `internal/service/trigger.go` | 持有 `pool.WorkerPool` 类型的 `triggerPool`，仅提交 `Executor.Execute`。 |
| `internal/service/executor.go` | 将 HTTP 客户端初始化为固定 `12s` 超时。 |
| `README.md` | 更新核心设计与关键配置，将共享协程池说明改为双池隔离。 |
| `docs/1-architecture.md`、`docs/2-timer-theory.md`、`docs/3-scheduler.md`、`docs/4-distributed-lock.md`、`docs/6-api-reference.md`、`docs/7-e2e-flow.md`、`docs/8-dynamic-bucketing.md`、`docs/9-observability-roadmap.md` | 同步执行链路、锁语义、API 回调约束、协程池职责与固定回调超时说明。 |
| `internal/config/config_test.go`、`internal/pkg/pool/pool_test.go`、`internal/service/worker_pool_test.go`、`internal/service/executor_test.go` | 验证默认/非法池容量、双池隔离与注入、固定 HTTP 超时。 |

## 7. 明确保留的现有语义

双池改造只隔离并发资源，不改变下列业务与可靠性机制：

### 7.1 动态分桶

继续使用分钟级任务量登记和动态桶数扩展逻辑；Scheduler 按每个分钟时间片实际桶数扫描。

### 7.2 当前分钟与上一分钟扫描

Scheduler 继续同时处理当前分钟和上一分钟分片，用于覆盖时间边界与短暂调度失败场景。

### 7.3 MySQL 补偿

Trigger 继续将 Redis 查询结果与 MySQL 中同一扫描窗口内的 `PENDING` 记录合并去重，MySQL 仍是待执行记录的可靠依据。

### 7.4 分布式锁语义

分片锁继续用于降低多实例重复扫描：

- worker 获取分片锁后运行 Trigger；
- Trigger 完成分片扫描及任务提交后，使用 Lua 校验所有权并续期；
- 不等待 HTTP 回调全部完成后再续期。

### 7.5 最终幂等保证

分片锁不作为最终回调防重条件。最终执行权仍由：

- `(timer_id, trigger_time)` 唯一约束；
- `PENDING -> RUNNING` 数据库条件更新；
- Bloom Filter 辅助过滤已成功执行任务；

共同保障。

## 8. 实施注意事项

### 8.1 不修改为非阻塞提交

当前 `ants.Pool` 采用默认阻塞 `Submit`。本次改造继续保留该行为。

若开启非阻塞提交，执行池满时任务提交可能直接失败；在没有额外重试或确认机制前，这会增加任务遗漏风险。

### 8.2 处理执行池背压

双池隔离后，慢回调不会占用 Scheduler worker，但 `Trigger.Run` 向满载的 `triggerPool` 提交任务时仍可能等待。固定 `12s` 回调超时能够限制该等待持续放大的风险，但高并发容量仍需要通过压测调整。

### 8.3 不将迁移器纳入双池描述

本次所称“双协程池”仅描述 `Scheduler -> Trigger -> Executor` 执行主链路。`Migrator` 继续独立完成未来窗口的记录生成与 Redis 投递。

## 9. 测试与验收

已补充以下验证：

1. 配置加载测试：未提供配置时，两个协程池容量均使用合法默认值。
2. 池注入测试：Scheduler 与 Trigger 分别持有注入的 `schedulerPool` 与 `triggerPool`，不会复用同一池实例。
3. 资源隔离测试：执行池存在耗时任务时，调度池仍可以处理新的分片任务。
4. 超时配置测试：Executor 初始化出的 HTTP client 固定使用 `12s` 超时。
5. 回归测试：通过现有服务测试覆盖动态分桶合并、缓存与监控指标行为，并运行 `go test ./...`。

## 10. 改造后可描述的核心设计

实施完成后，项目可准确表述为：

> 将定时任务执行链路拆分为 Scheduler、Trigger、Executor 三个核心模块，通过独立的调度扫描池与回调执行池隔离资源竞争；结合固定 12 秒 HTTP 超时、Redis 分片锁和 MySQL 原子抢占机制，避免慢回调阻塞调度链路并保障任务幂等执行。
