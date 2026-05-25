# 分布式锁与幂等

## 分片锁

ChronoFlow 以 `{time_range}:{bucket}` 为锁粒度。锁保护的是一分钟分片的扫描和派发，不是 HTTP 回调的完整执行周期。

```text
chronoflow:scheduler_lock:{YYYY-MM-DD-HH:mm}:{bucket}
```

### 获取与所有权

与 xtimer 一致，Scheduler 先将分片处理提交到 worker pool；处理该分片的 worker 创建锁对象，并使用 `GetProcessAndGoroutineIDStr()` 生成 token：

```go
lock := queue.NewSchedulerLock(timeRange, bucket) // token = processID_goroutineID
acquired, err := lock.Lock(ctx, 70*time.Second)
if acquired {
    trigger.Run(ctx, timeRange, bucket, bucketNum, lock)
}
```

锁 value 为 token。Trigger 完整扫描分片后调用 `lock.Extend`；该操作通过 Lua 先校验 Redis 中的 token 是否仍属于当前 worker，再执行 `EXPIRE`。`lock.Release` 同样通过 Lua 校验后删除。

```lua
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("EXPIRE", KEYS[1], ARGV[2])
end
return 0
```

如果旧 Trigger 的租约已经过期，而同名分片锁已被另一个 worker 重新取得，旧 Trigger 无法续期或释放新锁。任务执行幂等仍不依赖该锁，由 MySQL 原子状态抢占保证。

### `70s` 与 `130s`

```text
Scheduler 取得分片锁，初始 TTL = 70s
    |
    v
Trigger 以不重叠窗口扫描完整分钟分片，并提交 Executor
    |
    v
扫描无错误完成后，Lua 校验 token，将 TTL 重置为 130s
    |
    v
锁自然过期；期间抑制 Scheduler 对当前/上一分钟分片重入
```

- `lock_expiration = 70s`：覆盖 60 秒分片处理，并保留调度余量。
- `success_expiration = 130s`：分片派发成功后的保留租约，覆盖上一分钟回扫窗口。
- `130s` 不表示任务执行期间一直持锁；Executor 的 HTTP 回调是异步执行的。

## 派发防重

Redis ZSet 中的任务在读取后仍然保留，以便进程异常后能够补扫。Trigger 不再重复读取 `score <= now` 的累计范围，而是推进扫描游标：

```text
[10:05:00, 10:05:01)
[10:05:01, 10:05:02)
...
[10:05:59, 10:06:00)
```

同一次 Trigger 运行中，每个触发时间只落入一个窗口。对于已经过去但需要恢复的上一分钟分片，Trigger 会直接扫描完整历史区间。

窗口扫描降低重复派发，但不是最终执行幂等屏障：锁失效或节点故障后仍允许重新派发尚未完成的任务。

## 执行幂等

### 创建唯一性

`timer_records` 具有业务唯一键：

```sql
UNIQUE KEY uk_timer_trigger_time (timer_id, trigger_time)
```

Migrator 与激活流程的存在性检查会识别任意状态的既有记录，数据库唯一约束负责处理并发竞争下的最终一致性。开发阶段该唯一键直接定义在 `migrations/001_init.sql` 中。

### 回调执行权

Executor 在调用外部 HTTP 回调前执行条件更新：

```sql
UPDATE timer_records
SET status = 'RUNNING', started_at = ?
WHERE timer_id = ? AND trigger_time = ? AND status = 'PENDING';
```

- `RowsAffected = 1`：当前 Executor 获得执行权，可以发起回调。
- `RowsAffected = 0`：任务已被其他 Executor 领取或已完成，直接跳过。

因此，即使一个 ZSet member 因故障恢复而被重复派发，也只有一个 Executor 能从数据库获得执行许可。

### Bloom Filter

Bloom Filter 参考 xtimer 同时用于读写路径：

1. Executor 启动时先查询 Bloom Filter。
2. 命中时查询 MySQL 状态确认；已离开 `PENDING` 的任务可提前跳过。
3. miss、误判或 Bloom 查询异常时，继续走数据库 `PENDING -> RUNNING` 原子抢占。
4. HTTP 回调成功后写入 Bloom Filter，供后续重复派发快速过滤。

Bloom Filter 是前置性能优化，不能代替数据库条件更新，因为它存在误判且无法关闭并发竞争窗口。
