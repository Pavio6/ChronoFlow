# 分布式锁与幂等

## 分布式锁

### 实现方式

ChronoFlow 使用 Redis SETNX 实现分布式锁，锁粒度为 `{time_range}:{bucket}`。

```go
// 获取锁
func (q *RedisQueue) AcquireSchedulerLock(ctx context.Context, timeRange string, bucket int, expiration time.Duration) (bool, error) {
    key := fmt.Sprintf("chronoflow:scheduler_lock:%s:%d", timeRange, bucket)
    return q.client.SetNX(ctx, key, "1", expiration).Result()
}
```

### 锁的 Key 格式

```
chronoflow:scheduler_lock:{YYYY-MM-DD-HH:mm}:{bucket}
```

示例：
```
chronoflow:scheduler_lock:2026-05-22-10:05:0   # 10:05 第 0 桶
chronoflow:scheduler_lock:2026-05-22-10:05:1   # 10:05 第 1 桶
chronoflow:scheduler_lock:2026-05-22-10:05:2   # 10:05 第 2 桶
```

### 锁的生命周期

```
Scheduler 获取锁 (TTL=2s)
    │
    ▼
提交 Trigger 到协程池
    │
    ▼
Trigger 开始处理
    │
    ▼
续期锁 TTL (延长 2s)  ←── 每次 PopDueTasks 后续期
    │
    ▼
时间片结束 → 锁自然过期
```

### 锁的安全性

1. **互斥性**：SETNX 保证同一 `{time_range}:{bucket}` 只有一个实例获取锁
2. **防死锁**：锁有 TTL，即使持有者崩溃，锁也会自动过期
3. **续期机制**：Trigger 处理过程中定期续期，防止锁提前过期

### 多实例协调

```
实例 A: Scheduler → 获取锁 bucket:0 ✓ → 提交 Trigger
实例 B: Scheduler → 获取锁 bucket:0 ✗ → 跳过
实例 A: Scheduler → 获取锁 bucket:1 ✗ → 跳过
实例 B: Scheduler → 获取锁 bucket:1 ✓ → 提交 Trigger
```

## 幂等控制

### 三层幂等架构

```
请求到达
    │
    ▼
┌─────────────────┐
│  Bloom Filter   │ ← 第一层：快速过滤
│  (Redis Bitmap) │    误判率约 1%
└────────┬────────┘
         │ hit
         ▼
┌─────────────────┐
│  Redis SETNX    │ ← 第二层：精确判断
│  (幂等键)       │    无误判
└────────┬────────┘
         │ hit
         ▼
┌─────────────────┐
│  MySQL 查询     │ ← 第三层：最终确认
│  (执行记录表)    │    无误判
└─────────────────┘
         │
         ▼
    已执行 → 跳过
```

### Bloom Filter

**原理**：使用 SHA1 + Murmur3 双哈希，在 Redis Bitmap 中设置两个位。

```go
// 检查
func (f *Filter) Exist(ctx context.Context, key, val string) (bool, error) {
    bit1 := hashSHA1(val) % MaxBits
    bit2 := hashMurmur3(val) % MaxBits
    
    pipe := f.client.Pipeline()
    cmd1 := pipe.GetBit(ctx, key, int64(bit1))
    cmd2 := pipe.GetBit(ctx, key, int64(bit2))
    pipe.Exec(ctx)
    
    return cmd1.Val() == 1 && cmd2.Val() == 1, nil
}

// 设置
func (f *Filter) Set(ctx context.Context, key, val string, expireSeconds int64) error {
    bit1 := hashSHA1(val) % MaxBits
    bit2 := hashMurmur3(val) % MaxBits
    
    pipe := f.client.Pipeline()
    pipe.SetBit(ctx, key, int64(bit1), 1)
    pipe.SetBit(ctx, key, int64(bit2), 1)
    pipe.Exec(ctx)
    
    return nil
}
```

**Key 格式**：`chronoflow:bloom:{YYYY-MM-DD}`（按天分片）

**Value 格式**：`{timer_id}:{trigger_time_ms}`

**过期时间**：24 小时（自动清理历史数据）

### Redis 幂等键

```go
// 设置幂等键（SETNX）
func (q *RedisQueue) SetIdempotentKey(ctx context.Context, timerID int64, triggerTime time.Time, expiration time.Duration) (bool, error) {
    key := fmt.Sprintf("chronoflow:idempotent:%d:%d", timerID, triggerTime.UnixMilli())
    return q.client.SetNX(ctx, key, "1", expiration).Result()
}
```

**Key 格式**：`chronoflow:idempotent:{timer_id}:{trigger_time_ms}`

**过期时间**：24 小时

### MySQL 查重

```go
func (r *timerRecordRepo) ExistsByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error) {
    var count int64
    r.db.Model(&model.TimerRecord{}).
        Where("timer_id = ? AND trigger_time = ? AND status != ?", timerID, triggerTime, model.RecordStatusPending).
        Count(&count)
    return count > 0, nil
}
```

查询条件：同一 `timer_id` + `trigger_time` 且状态不是 PENDING 的记录。

### 三层幂等的性能分析

| 场景 | Bloom Filter | Redis | MySQL | 总延迟 |
|------|-------------|-------|-------|--------|
| 首次执行 | miss → 执行 | - | - | ~0.1ms |
| 重复执行（99%） | hit → 跳过 | - | - | ~0.1ms |
| 重复执行（1%误判） | hit → Redis 检查 | hit → 跳过 | - | ~0.5ms |
| 重复执行（极罕见） | hit → Redis 检查 | hit → MySQL 确认 | hit → 跳过 | ~2ms |

99% 的重复任务在 Bloom Filter 层就被拦截，1% 穿透到 Redis，极少数穿透到 MySQL。
