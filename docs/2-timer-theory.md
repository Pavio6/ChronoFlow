# 分布式定时器原理与实现

## 背景

ChronoFlow 采用主动轮询 + 二维分片的架构设计，解决大规模定时任务调度的核心问题。

## 核心问题

传统定时任务调度面临三个核心问题：

1. **全量扫描效率低**：每次触发都要扫描所有任务，O(N) 复杂度
2. **单点瓶颈**：单个 Redis ZSet 存储所有任务，成为性能瓶颈
3. **重复执行**：多实例部署时，同一任务可能被多个实例执行

## 解决方案

### 1. 主动轮询 + 有序表（时间 O(N) → O(logN)，空间 O(N) → O(N)）

**传统方案**：定时扫描数据库，查询所有到期任务
```
SELECT * FROM tasks WHERE next_trigger_time <= NOW()
```
问题：任务量大时，全表扫描效率极低。

**优化方案**：用 Redis ZSet（score = 执行时间戳）替代全盘扫描
```
ZRANGEBYSCORE chronoflow:timer:{range}:{bucket} -inf now LIMIT 0 100
```
优势：ZSet 底层使用 skiplist（跳表）按 score 排序，`ZRANGEBYSCORE` 通过跳表实现 O(logN + M) 的范围查询（M 为返回元素数），远优于全表扫描的 O(N)。

**空间复杂度对比**：

| 方案 | 时间复杂度 | 空间复杂度 | 说明 |
|------|-----------|-----------|------|
| MySQL 全表扫描 | O(N) | O(N) | 每行存储完整字段（timer_id、cron_expr、callback_url 等），单行约几百字节 |
| Redis ZSet | O(logN + M) | O(N) | 每个元素仅存 member（`timer_id:trigger_time_ms`）+ score（8 字节），单条约 30~50 字节 |

两者空间复杂度同为 O(N)，但 Redis ZSet 单元素开销远小于 MySQL 行记录；配合时间分片 + 分桶，每个 ZSet 实际元素量极小（N 被分摊到多个 key），进一步降低单次查询的内存占用和跳表遍历深度。

### 2. 纵向分治（时间分片）

**问题**：单个 ZSet 存储所有任务，key 越大查询越慢。

**方案**：按分钟级时间范围切分 ZSet，每个分片独立 key。

```
# 不同时间片的 key
chronoflow:timer:2026-05-22-10:00:0   # 10:00 第 0 桶
chronoflow:timer:2026-05-22-10:01:0   # 10:01 第 0 桶
chronoflow:timer:2026-05-22-10:02:0   # 10:02 第 0 桶
```

**复杂度变化**（设总任务数 N，时间分片数 T）：

| 维度 | 分片前 | 分片后 |
|------|--------|--------|
| 单次查询时间 | O(logN + M) | O(log(N/T) + M) |
| 单 key 空间 | O(N) | O(N/T) |
| 总空间 | O(N) | O(N) |

将 N 个任务分散到 T 个时间片中，每个 ZSet 的元素量从 N 降为 N/T，跳表层数减少，查询更快；过期分片可直接 DEL 释放内存。

### 3. 横向分治（分桶并发）

**问题**：单个时间片内任务量大，单 goroutine 处理慢。

**方案**：按 `timer_id % bucket_num` 分桶，每桶独立 goroutine 并发处理。

```
# 同一时间片的不同桶
chronoflow:timer:2026-05-22-10:00:0   # 桶 0
chronoflow:timer:2026-05-22-10:00:1   # 桶 1
chronoflow:timer:2026-05-22-10:00:2   # 桶 2
```

**复杂度变化**（设总任务数 N，分桶数 B）：

| 维度 | 分桶前 | 分桶后 |
|------|--------|--------|
| 单次查询时间 | O(logN + M) | O(log(N/B) + M) |
| 单 key 空间 | O(N) | O(N/B) |
| 总空间 | O(N) | O(N) |
| 并发度 | 1 goroutine | B goroutine |

将同一个时间片的任务分散到 B 个桶中，每个桶的元素量从 N 降为 N/B；B 个 goroutine 并行处理，吞吐量提升 B 倍。

### 4. 二维分片

将纵向分治和横向分治组合，得到二维分片：

```
key = {time_range}:{bucket}
```

- **第一维（时间）**：按分钟切分，减少单次查询量
- **第二维（桶）**：按 timer_id 分桶，实现并发处理

## ChronoFlow 的实现

### Migrator（一级迁移：MySQL → Redis）

```go
// 核心逻辑
for _, def := range activeDefinitions {
    triggerTimes := cronParser.NextTriggerTimesBefore(def.CronExpr, startTime, endTime)
    for _, triggerTime := range triggerTimes {
        if triggerTime.After(endTime) { break }
        
        // 幂等检查
        if exists, _ := repo.ExistsByTimerIDAndTriggerTime(def.ID, triggerTime); exists {
            continue
        }
        
        // 创建记录
        record := &TimerRecord{TimerID: def.ID, TriggerTime: triggerTime, Status: "PENDING"}
        repo.Create(record)
        
        // 推入 Redis
        bucket := def.ID % bucketNum
        timeRange := formatTimeRange(triggerTime)
        queue.PushTask(ctx, timeRange, bucket, &TaskTrigger{TimerID: def.ID, TriggerTime: triggerTime})
    }
}
```

### Scheduler → Trigger → Executor 双协程池通信

```go
// Scheduler：提交 worker；worker 内抢锁并运行 Trigger（同时处理当前分钟和上一分钟）
currentBucketNum, _ := queue.GetBucketNum(ctx, currentTimeRange, defaultBucketNum)
prevBucketNum, _ := queue.GetBucketNum(ctx, prevTimeRange, defaultBucketNum)
lockExpiration := time.Duration(cfg.LockExpiration) * time.Second // 默认 70s

for bucket := 0; bucket < currentBucketNum; bucket++ {
    schedulerPool.Submit(func() {
        lock := queue.NewSchedulerLock(currentTimeRange, bucket) // token = processID_goroutineID
        if acquired, _ := lock.Lock(ctx, lockExpiration); acquired {
            trigger.Run(ctx, currentTimeRange, bucket, currentBucketNum, lock)
        }
    })
}
for bucket := 0; bucket < prevBucketNum; bucket++ {
    schedulerPool.Submit(func() {
        lock := queue.NewSchedulerLock(prevTimeRange, bucket)
        if acquired, _ := lock.Lock(ctx, lockExpiration); acquired {
            trigger.Run(ctx, prevTimeRange, bucket, prevBucketNum, lock)
        }
    })
}

// Trigger：非重叠窗口轮询 + DB 部分投递补偿 + 提交 Executor + 成功保留锁
successExpiration := time.Duration(cfg.SuccessExpiration) * time.Second // 默认 130s
cursor := sliceStart
for cursor.Before(sliceEnd) {
    dueEnd := min(nextWholeSecond(time.Now()), sliceEnd)
    if !cursor.Before(dueEnd) { continue }
    triggers, _ := queue.GetTasksByTime(ctx, timeRange, bucket, cursor, dueEnd)
    // 每个窗口始终合并已有 MySQL PENDING 记录（Redis 部分失败兜底）
    dbTriggers, _ := recRepo.GetPendingByTimeRange(cursor, dueEnd)
    // 过滤 DB 结果: timer_id % bucketNum == bucket
    triggers = mergeTaskTriggers(triggers, filterBucket(dbTriggers, bucket, bucketNum))
    for _, t := range triggers {
        triggerPool.Submit(func() {
            executor.Execute(ctx, t)
        })
    }
    cursor = dueEnd
}
lock.Extend(ctx, successExpiration) // Lua 校验 token 后设置完整扫描成功后的保留 TTL
```

`schedulerPool` 仅负责分片扫描与 Trigger 运行，`triggerPool` 承载 Executor HTTP 回调；Executor 使用固定 `12s` HTTP 超时限制异常下游占用执行 worker 的时间。

### 数据库原子执行抢占

```
唯一索引 (timer_id, trigger_time) → 防止重复创建执行记录
UPDATE ... WHERE status = PENDING → RowsAffected = 1 → 获得执行权
                                  → RowsAffected = 0 → 跳过重复派发
```

| 层级 | 作用 | 正确性职责 |
|------|------|------------|
| 唯一索引 | 阻止重复建任务记录 | 必须 |
| 条件状态更新 | 竞争执行权 | 必须 |
