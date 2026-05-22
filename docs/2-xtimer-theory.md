# xTimer 原理与实现

## 背景

ChronoFlow 的架构设计参考了字节跳动的 xTimer 系统。xTimer 是一个基于协程池的分布式定时器，解决了大规模定时任务调度的核心问题。

## 核心问题

传统定时任务调度面临三个核心问题：

1. **全量扫描效率低**：每次触发都要扫描所有任务，O(N) 复杂度
2. **单点瓶颈**：单个 Redis ZSet 存储所有任务，成为性能瓶颈
3. **重复执行**：多实例部署时，同一任务可能被多个实例执行

## xTimer 的解决方案

### 1. 主动轮询 + 有序表（O(N) → O(logN)）

**传统方案**：定时扫描数据库，查询所有到期任务
```
SELECT * FROM tasks WHERE next_trigger_time <= NOW()
```
问题：任务量大时，全表扫描效率极低。

**xTimer 方案**：用 Redis ZSet（score = 执行时间戳）替代全盘扫描
```
ZRANGEBYSCORE chronoflow:timer:{range}:{bucket} -inf now LIMIT 0 100
```
优势：ZSet 按 score 排序，查询复杂度 O(logN)。

### 2. 纵向分治（时间分片）

**问题**：单个 ZSet 存储所有任务，key 越大查询越慢。

**方案**：按分钟级时间范围切分 ZSet，每个分片独立 key。

```
# 不同时间片的 key
chronoflow:timer:2026-05-22-10:00:0   # 10:00 第 0 桶
chronoflow:timer:2026-05-22-10:01:0   # 10:01 第 0 桶
chronoflow:timer:2026-05-22-10:02:0   # 10:02 第 0 桶
```

优势：每个分片数据量小，查询快；过期分片可自动清理。

### 3. 横向分治（分桶并发）

**问题**：单个时间片内任务量大，单 goroutine 处理慢。

**方案**：按 `timer_id % bucket_num` 分桶，每桶独立 goroutine 并发处理。

```
# 同一时间片的不同桶
chronoflow:timer:2026-05-22-10:00:0   # 桶 0
chronoflow:timer:2026-05-22-10:00:1   # 桶 1
chronoflow:timer:2026-05-22-10:00:2   # 桶 2
```

优势：多桶并行处理，充分利用多核 CPU。

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
    triggerTimes := cronParser.NextNTriggerTimes(def.CronExpr, now, maxTriggers)
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

### Scheduler → Trigger → Executor 协程池通信

```go
// Scheduler：抢锁 + 提交 Trigger
for bucket := 0; bucket < bucketNum; bucket++ {
    if acquired, _ := queue.AcquireSchedulerLock(ctx, timeRange, bucket, lockTTL); acquired {
        pool.Submit(func() {
            trigger.Run(ctx, timeRange, bucket)
        })
    }
}

// Trigger：轮询 + 提交 Executor
for !timeSliceExpired {
    triggers, _ := queue.PopDueTasks(ctx, timeRange, bucket, batchSize)
    for _, t := range triggers {
        pool.Submit(func() {
            executor.Execute(ctx, t)
        })
    }
}
```

### 三层幂等去重

```
Bloom Filter → miss → 执行
Bloom Filter → hit → Redis 幂等键检查
                    → miss → 执行（bloom 误判）
                    → hit  → MySQL 查重
                           → 已执行 → 跳过
                           → 未执行 → 执行
```

**为什么需要三层？**

| 层级 | 作用 | 误判 | 延迟 |
|------|------|------|------|
| Bloom Filter | 快速过滤大部分已执行任务 | 有（false positive） | 极低 |
| Redis SETNX | bloom 误判时二次确认 | 无 | 低 |
| MySQL 查询 | 最终确认 | 无 | 中 |

Bloom Filter 的误判率约 1%，即 1% 的已执行任务会穿透到 Redis 检查。
Redis SETNX 无误判，只有真正重复的任务才会穿透到 MySQL。

## 性能数据

根据 xTimer 压测结果：

| 场景 | 成功率 | P99 延时 |
|------|--------|----------|
| 1000 笔定时器同时执行 | 100% | 2.4s |
| 2000 笔定时器同时执行 | 100% | 9.1s |

瓶颈不在调度逻辑，而在瞬时 Redis/MySQL 连接数激增。逻辑层面无性能问题。
