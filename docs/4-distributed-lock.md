# 并发控制、Lease 与分布式锁

ChronoFlow 不使用一个覆盖全系统的“分布式锁”。不同阶段使用与其权威数据位于同一系统内的并发原语。

## 1. 单体锁和分布式锁

进程内锁（`sync.Mutex`）只协调同一进程的 Goroutine。部署两个实例后，每个实例都有自己的锁，彼此不可见。

分布式锁让多台机器争夺一个逻辑所有权，常见实现位于 Redis、ZooKeeper、etcd 或数据库。但“拿到锁”不等于业务安全：锁可能过期，旧持有者可能继续运行，因此关键写入仍需要条件更新、唯一约束或 fencing token。

## 2. ChronoFlow 的并发控制矩阵

| 阶段 | 并发原语 | 权威系统 | 最终防线 |
| --- | --- | --- | --- |
| Scheduler 领取 Timer | 行锁 + `SKIP LOCKED` | MySQL | Execution 唯一键、Timer `version` |
| Dispatcher 领取 Outbox | 有期限的 Claim | MySQL | 唯一 `event_id`、Worker 幂等抢占 |
| Worker 分配消息 | Consumer Group / Pending | Redis | MySQL Execution Lease |
| Worker 提交结果 | Lease + `run_token` | MySQL | 条件更新 |
| 单 Worker 并发 | ants Pool | 进程内 | `pool_size` 容量 |

## 3. 数据库行锁

Scheduler 的行锁只在短事务中持有。多个副本执行相同查询时：

```sql
SELECT ...
FROM timer_definitions
WHERE status='ACTIVE' AND next_fire_at<=?
ORDER BY next_fire_at, id
LIMIT ?
FOR UPDATE SKIP LOCKED;
```

副本 A 锁定的行，副本 B 会跳过并处理其他行。事务提交或回滚后锁自动释放。

这不是“单体锁”：锁状态由共享 MySQL 维护，所有机器都能看到。

## 4. Lease 为什么不是普通锁

Dispatcher 和 Worker 的工作会跨越一个较长过程，不适合始终持有数据库事务。因此使用有过期时间的 Lease：

```text
claim_owner / claim_until     Outbox 发布所有权
lease_owner / lease_until     Execution 执行所有权
```

Worker 在回调期间续租。进程崩溃后不会主动释放，但其他实例可在 `lease_until` 到期后接管。

Lease 有一个固有问题：旧执行者可能在网络暂停后恢复。`run_token` 用作 fencing token：

```sql
UPDATE timer_executions
SET status='SUCCESS', ...
WHERE id=? AND status='RUNNING' AND run_token=?;
```

新执行者接管时会生成新 token，旧执行者即使回来也无法覆盖新状态。

## 5. Lua 能解决什么

Redis Lua 可以把多条 Redis 命令原子化，例如“值匹配才释放锁”。它不能让 MySQL 写入和 Redis 写入成为同一个原子事务：

```text
MySQL COMMIT 成功
进程崩溃
Redis Lua 尚未执行
```

因此 MySQL → Redis 的可靠投递使用 Transactional Outbox，而不是 Lua 双写。Lua 适用于 Redis 内部原子操作，不适用于跨 MySQL 和 Redis 的事务。

## 6. 需要接受的语义

系统保证任务状态可恢复，并尽量避免重复回调，但 HTTP 副作用无法与 MySQL 状态提交组成原子事务。调用方应：

- 接收稳定的 Execution ID 作为幂等键；
- 重复请求返回已有结果；
- 不把 Worker IP 或 Consumer 名称当作业务身份。

这是分布式任务系统的正常边界，不应通过延长锁时间来掩盖。
