# 定时任务的核心语义

## 1. Timer 不是待执行任务列表

Timer 是一条规则，Execution 才是某个具体计划时间的执行实例。

```text
Timer:      每分钟第 0 秒执行
Execution:  10:00:00、10:01:00、10:02:00 各自独立
```

ChronoFlow 不提前生成未来窗口，也不创建时间桶。每个 ACTIVE Timer 只保存一个权威游标 `next_fire_at`。Scheduler 发现游标到期后，生成当前 Execution 并把游标推进到下一次。

这种模型的状态规模与 Timer 数量相关，而不是与“未来触发点数量”相关。

## 2. 时间与 Cron

- Cron 使用六字段格式：`秒 分 时 日 月 周`。
- `timezone` 决定 Cron 的日历语义。
- MySQL 中 `next_fire_at` 和 `scheduled_at` 统一保存 UTC。
- 激活 Timer 时从当前时间计算第一个 `next_fire_at`。
- 停用或删除 Timer 时清空 `next_fire_at`。

时区转换只发生在 Cron 计算边界；持久化和跨进程传输统一使用 UTC。

## 3. Misfire

进程停止、数据库故障或发布会让 Scheduler 晚于计划时间看到 Timer。`misfire_policy` 决定如何处理错过的触发点：

| 策略 | 行为 |
| --- | --- |
| `SKIP` | 不补执行错过的触发点，游标推进到现在之后 |
| `FIRE_ONCE` | 为最早错过点创建一次 Execution，然后推进到现在之后 |
| `CATCH_UP` | 按顺序补创建，单轮最多 `max_catch_up` 条 |

`scheduler.misfire_grace_seconds` 提供允许的调度延迟窗口。`CATCH_UP` 必须有限制，避免长时间停机后一次事务生成无界数据。

## 4. 三种不同的“重复”

### 重复生成

多个 Scheduler 可能同时观察到同一 Timer。行锁降低竞争，数据库唯一键 `(timer_id, scheduled_at)` 提供最终防线。

### 重复投递

Dispatcher 可能在 XADD 成功后、回写 MySQL 前崩溃。恢复后同一事件可能再次进入 Stream，这是 Outbox 至少一次模型的预期行为。

### 重复执行

Worker 必须先取得 Execution Lease。只有携带当前 `run_token` 的执行者才能提交结果。因此重复消息通常只会被确认，不会再次调用回调。

仍然存在一种不可完全消除的窗口：外部服务已经处理回调，但 Worker 在保存成功状态前崩溃。没有跨 HTTP 服务的分布式事务时，调度系统无法判断外部副作用是否发生。因此回调方应按 Execution ID 实现幂等。

## 5. ants 与 Redis Streams

两者解决不同层次的问题：

- Redis Streams 是跨进程的持久化消息通道，支持 Consumer Group、Pending 和故障接管。
- ants 是单个 Worker 进程内的 Goroutine 并发限制器，控制连接数、内存和下游压力。

ants 不能代替消息队列：进程退出后，内存中的任务不会由其他机器接管。Redis Streams 也不能代替本地限流：一个 Worker 仍需要限制同时执行的回调数。

## 6. 可靠性的权威来源

| 信息 | 权威来源 |
| --- | --- |
| Timer 是否应继续调度 | MySQL `timer_definitions` |
| 某计划时间是否已生成 | MySQL Execution 唯一键 |
| 执行状态和当前所有者 | MySQL `timer_executions` |
| 是否需要发布消息 | MySQL `outbox_events` |
| 哪些消息待某 Consumer 确认 | Redis Consumer Group |
| 单进程当前可运行多少回调 | ants Pool |

设计判断的简单原则是：Redis 消息可以重建，MySQL 权威状态不能依赖 Redis 才成立。
