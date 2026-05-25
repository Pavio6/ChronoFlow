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
等待第一次 ticker 触发 → 每隔 migrate_step_minutes（默认 60 分钟）执行
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
- 对每个桶提交分片处理 worker
- worker 创建 token、抢到分布式锁后运行 Trigger

### 执行流程

```go
func (s *Scheduler) schedule(ctx context.Context) {
    now := time.Now()
    
    // 同时处理当前分钟和上一分钟，避免边界任务遗漏
    currentTimeRange := formatTimeRange(now)
    prevTimeRange := formatTimeRange(now.Add(-time.Minute))
    
    // 分别获取各分钟的动态分桶数（不同分钟可能不同）
    currentBucketNum, _ := s.queue.GetBucketNum(ctx, currentTimeRange, s.cfg.BucketNum)
    prevBucketNum, _ := s.queue.GetBucketNum(ctx, prevTimeRange, s.cfg.BucketNum)
    
    // 锁初始 TTL，参考 xTimer tryLockSeconds（必须大于时间片时长 60 秒）
    lockExpiration := time.Duration(s.cfg.LockExpiration) * time.Second
    
    // 处理当前分钟
    for bucket := 0; bucket < currentBucketNum; bucket++ {
        s.handleSlice(ctx, currentTimeRange, bucket, currentBucketNum, lockExpiration)
    }
    // 处理上一分钟
    for bucket := 0; bucket < prevBucketNum; bucket++ {
        s.handleSlice(ctx, prevTimeRange, bucket, prevBucketNum, lockExpiration)
    }
}

func (s *Scheduler) handleSlice(ctx context.Context, timeRange string, bucket int, bucketNum int, lockExpiration time.Duration) {
    s.pool.Submit(func() {
        lock := s.queue.NewSchedulerLock(timeRange, bucket)
        acquired, _ := lock.Lock(ctx, lockExpiration)
        if acquired {
            s.trigger.Run(ctx, timeRange, bucket, bucketNum, lock)
        }
    })
}
```

### 锁策略

- 锁初始 TTL = `lock_expiration`（默认 70 秒，参考 xTimer `tryLockSeconds`，必须大于时间片时长 60 秒）
- Trigger 成功扫描完整分片后将锁 TTL 设置为 `success_expiration`（默认 130 秒，参考 xTimer `successExpireSeconds`）
- 锁 token = `GetProcessAndGoroutineIDStr()`，由执行该分片的 worker goroutine 生成
- `Extend` / `Release` 通过 Lua 比较 token，仅当前锁持有者能够操作锁
- 锁粒度：每个 `{time_range}:{bucket}` 一把锁
- 多实例部署时，同一分片只会被一个实例处理
- 动态分桶：当前分钟和上一分钟分别获取各自的 bucketNum，避免桶数不一致导致遗漏或越界

## Trigger 详解

### 文件位置

`internal/service/trigger.go`

### 核心职责

- 在时间片内持续轮询 Redis ZSet
- 用不重叠扫描窗口读取到期任务，避免一次 Trigger 重复派发同一 member
- 扫描成功后保留锁 TTL（参考 xTimer successExpireSeconds），覆盖上一分钟回扫
- Redis 无结果时回退查询 MySQL（冷启动兜底）
- 提交 Executor 到协程池

### 执行流程

```go
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int, lock *redis.SchedulerLock) {
    timeSliceStart, timeSliceEnd := parseMinuteRange(timeRange)
    cursor := timeSliceStart
    successExpiration := time.Duration(t.cfg.SuccessExpiration) * time.Second

    for cursor.Before(timeSliceEnd) {
        dueEnd := min(nextWholeSecond(time.Now()), timeSliceEnd)
        if !cursor.Before(dueEnd) { continue }

        triggers, _ := t.queue.GetTasksByTime(ctx, timeRange, bucket, cursor, dueEnd)
        
        // Redis 无结果时，回退查询 MySQL（冷启动兜底）
        if len(triggers) == 0 {
            triggers, _ = t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum, cursor, dueEnd)
        }
        
        // 提交 Executor
        for _, trigger := range triggers {
            t.pool.Submit(func() {
                t.executor.Execute(ctx, trigger)
            })
        }
        cursor = dueEnd
    }
    lock.Extend(ctx, successExpiration)
}

// getDueTasksFromDB DB 回退：查询 MySQL 中 PENDING 状态的记录
func (t *Trigger) getDueTasksFromDB(ctx context.Context, timeRange string, bucket int, bucketNum int, start, end time.Time) ([]*redis.TaskTrigger, error) {
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
- 结束边界：下一分钟起点（不包含）

例如 `time_range = "2026-05-22-10:05"`，则时间片为 `[10:05:00, 10:06:00)`。历史分钟补扫时会立即完成全片读取。

## Executor 详解

### 文件位置

`internal/service/executor.go`

### 核心职责

- Bloom Filter 快速过滤已完成的重复任务
- 通过数据库原子状态抢占取得最终执行权
- 查询定时器定义
- 执行 HTTP 回调
- 更新执行记录状态

### 执行流程

```
1. Bloom Filter 快速查询
   ├─ miss/查询失败 → 继续
   └─ hit → MySQL 确认已处理则跳过；若为误判则继续

2. 查询 MySQL 中的实时状态
   ├─ ACTIVE → 继续执行
   └─ INACTIVE/DELETED → 跳过，不删除 Redis ZSet 中已打点数据

3. 查询不可变的定时器执行配置
   ├─ 内存缓存命中 → 直接使用
   └─ 缓存未命中 → 查 MySQL → 写入缓存

4. 原子状态抢占
   ├─ PENDING -> RUNNING 更新成功 → 获得回调执行权
   └─ 更新行数为 0 → 已被领取或完成，跳过

5. 执行 HTTP 回调
   ├─ 成功 → 记录 response_code/response_body
   └─ 失败 → 标记为 FAILED

6. 后处理
   - 执行成功 → Bloom Filter 打点，用于后续快速过滤
   - 更新 MySQL 记录
   - 上报 Prometheus 指标
```

定时器定义在创建后不提供修改操作；如需改变 Cron 或回调参数，需要删除旧定义并新建。由于可变化的状态不从本地缓存读取，停用和删除会在各执行节点后续的状态检查中生效，不受缓存 TTL 延迟影响；已经开始执行的回调不在此保证范围内。

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
