# 调度器模块详解

## 模块概览

调度引擎由四个模块组成，通过 ants 协程池串联：

```
Migrator ──[协程池]──→ Scheduler ──[协程池]──→ Trigger ──[协程池]──→ Executor
```

## Migrator 详解

### 文件位置

`internal/service/migrator.go`

### 核心职责

- 定时扫描 MySQL 中 ACTIVE 状态的定时器定义
- 解析 Cron 表达式，计算未来 step1 时间范围内的触发时间点
- 批量创建执行记录到 MySQL
- 按分桶规则批量推送到 Redis ZSet

### 执行时机

```
等待第一次 ticker 触发 → 每隔 step1_duration（默认 60 分钟）执行
```

启动时不立即执行，冷启动期间的任务由 Trigger 的 DB 回退机制兜底。

### 关键代码

```go
func (m *Migrator) Start(ctx context.Context) {
    // 不立即执行，等第一次 ticker
    ticker := time.NewTicker(time.Duration(m.cfg.MigrateStepMinutes) * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        m.doMigrate(ctx)
    }
}

func (m *Migrator) doMigrate(ctx context.Context) {
    // 1. 查询所有 ACTIVE 定时器
    definitions, _ := m.defRepo.GetActiveDefinitions()
    
    // 2. 计算时间范围（小时级取整）
    now := time.Now()
    startTime := getStartHour(now.Add(time.Duration(m.cfg.MigrateStepMinutes) * time.Minute))
    endTime := getStartHour(now.Add(time.Duration(m.cfg.MigrateStepMinutes*2) * time.Minute))
    
    for _, def := range definitions {
        // 3. 解析触发时间点
        triggerTimes, _ := m.parser.NextTriggerTimesBefore(def.CronExpr, startTime, endTime)
        
        for _, triggerTime := range triggerTimes {
            // 4. 幂等检查
            if exists, _ := m.recRepo.ExistsByTimerIDAndTriggerTime(def.ID, triggerTime); exists {
                continue
            }
            
            // 5. 创建记录
            m.recRepo.Create(&model.TimerRecord{...})
            
            // 6. 推入 Redis
            bucket := int(def.ID) % bucketNum
            timeRange := formatTimeRange(triggerTime)
            m.queue.PushTask(ctx, timeRange, bucket, &redis.TaskTrigger{...})
        }
    }
}

// getStartHour 将时间取整到小时
func getStartHour(t time.Time) time.Time {
    return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}
```

### 分桶算法

```go
bucket = timer_id % bucket_num
```

- 同一个定时器的所有触发记录都在同一个桶中
- 不同定时器可能在同一个桶中（hash 冲突）
- 桶数量在配置中指定，默认 3

## Scheduler 详解

### 文件位置

`internal/service/scheduler.go`

### 核心职责

- 每秒轮询一次
- 计算当前时间范围
- 对每个桶尝试抢分布式锁
- 抢到锁后提交 Trigger 到协程池

### 执行流程

```go
func (s *Scheduler) schedule(ctx context.Context) {
    now := time.Now()
    
    // 同时处理当前分钟和上一分钟，避免边界任务遗漏
    currentTimeRange := formatTimeRange(now)
    prevTimeRange := formatTimeRange(now.Add(-time.Minute))
    
    bucketNum, _ := s.queue.GetBucketNum(ctx, currentTimeRange, s.cfg.BucketNum)
    lockExpiration := time.Duration(s.cfg.ScanInterval*2) * time.Second
    
    for bucket := 0; bucket < bucketNum; bucket++ {
        s.handleSlice(ctx, currentTimeRange, bucket, bucketNum, lockExpiration)
        s.handleSlice(ctx, prevTimeRange, bucket, bucketNum, lockExpiration)
    }
}

func (s *Scheduler) handleSlice(ctx context.Context, timeRange string, bucket int, bucketNum int, lockExpiration time.Duration) {
    acquired, _ := s.queue.AcquireSchedulerLock(ctx, timeRange, bucket, lockExpiration)
    if acquired {
        s.pool.Submit(func() {
            s.trigger.Run(ctx, timeRange, bucket, bucketNum)
        })
    }
}
```

### 锁策略

- 锁 TTL = `scan_interval * 2`（默认 2 秒）
- 锁粒度：每个 `{time_range}:{bucket}` 一把锁
- 多实例部署时，同一分片只会被一个实例处理

## Trigger 详解

### 文件位置

`internal/service/trigger.go`

### 核心职责

- 在时间片内持续轮询 Redis ZSet
- Redis 无结果时回退查询 MySQL（冷启动兜底）
- 提交 Executor 到协程池
- 续期锁 TTL

### 执行流程

```go
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int) {
    timeSliceEnd := calculateTimeSliceEnd(timeRange)
    
    for {
        // 检查时间片是否结束
        if time.Now().After(timeSliceEnd) { break }
        
        // 从 Redis 获取到期任务
        triggers, _ := t.queue.GetDueTasks(ctx, timeRange, bucket, batchSize)
        
        // Redis 无结果时，回退查询 MySQL（冷启动兜底）
        if len(triggers) == 0 {
            triggers, _ = t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum)
        }
        
        if len(triggers) == 0 {
            time.Sleep(500 * time.Millisecond) // 无任务时短暂等待
            continue
        }
        
        // 提交 Executor
        for _, trigger := range triggers {
            t.pool.Submit(func() {
                t.executor.Execute(ctx, trigger)
            })
        }
        
        // 续期锁
        t.queue.ExtendSchedulerLock(ctx, timeRange, bucket, lockTTL)
    }
}

// getDueTasksFromDB DB 回退：查询 MySQL 中 PENDING 状态的记录
func (t *Trigger) getDueTasksFromDB(ctx context.Context, timeRange string, bucket int, bucketNum int) ([]*redis.TaskTrigger, error) {
    start, _ := time.Parse("2006-01-02-15:04", timeRange)
    end := start.Add(time.Minute)
    
    records, _ := t.recRepo.GetPendingByTimeRange(start, end)
    
    var triggers []*redis.TaskTrigger
    for _, record := range records {
        if int(record.TimerID)%bucketNum != bucket {
            continue
        }
        triggers = append(triggers, &redis.TaskTrigger{
            TimerID:     record.TimerID,
            TriggerTime: record.TriggerTime,
        })
    }
    return triggers, nil
}
```

### 时间片计算

时间片按分钟划分：
- 开始时间：`time_range` 对应的分钟
- 结束时间：该分钟的 59 秒

例如 `time_range = "2026-05-22-10:05"`，则时间片为 `10:05:00 ~ 10:05:59`。

## Executor 详解

### 文件位置

`internal/service/executor.go`

### 核心职责

- 两层幂等去重
- 查询定时器定义
- 执行 HTTP 回调
- 更新执行记录状态
- 处理重试逻辑

### 执行流程

```
1. Bloom Filter 查重
   ├─ miss → 继续执行
   └─ hit → MySQL 查记录状态
            ├─ status != PENDING → 跳过（已执行）
            └─ status == PENDING → 执行（bloom 误判）

2. 查询定时器定义
   ├─ 内存缓存命中 → 直接使用
   └─ 缓存未命中 → 查 MySQL → 写入缓存

3. 检查定时器状态
   ├─ ACTIVE → 继续执行
   └─ INACTIVE/DELETED → 跳过

4. 执行 HTTP 回调
   ├─ 成功 → 记录 response_code/response_body
   └─ 失败 → 检查是否可重试
            ├─ 可重试 → 设置 next_retry_time
            └─ 不可重试 → 标记为 FAILED

5. 后处理
   - 执行成功 → Bloom Filter 打点（仅成功时写入）
   - 更新 MySQL 记录
   - 上报 Prometheus 指标
```

### 重试逻辑

```go
if record.IsRetryable(maxRetries) {
    record.Status = model.RecordStatusRetrying
    record.RetryCount++
    nextRetryTime := retryCalculator.CalculateNextRetryTime(record.RetryCount)
    record.NextRetryTime = &nextRetryTime
}
```

重试策略（指数退避）：
- 第 1 次重试：10 秒后
- 第 2 次重试：30 秒后
- 第 3 次重试：60 秒后

## 模块间通信

```
Migrator ──[MySQL]──> timer_records ──[Redis ZSet]──> Scheduler
                                                      │
                                                      ▼
                                                  [协程池]
                                                      │
                                                      ▼
                                                  Trigger
                                                      │
                                                      ▼
                                                  [协程池]
                                                      │
                                                      ▼
                                                  Executor
                                                      │
                                                      ▼
                                                  [HTTP 回调]
```

所有模块通过 ants 协程池通信，而非传统的 channel 或 MQ。这样做的好处：
1. 零额外依赖（不需要 Kafka/Pulsar）
2. 低延迟（进程内通信）
3. 可控并发（协程池大小限制）
