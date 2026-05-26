# E2E 流程详解

本文档描述从创建定时器到任务执行完成的完整流程，包括数据流和状态变化。

## 流程概览

```
用户创建定时器 → 激活 → Migrator 预创建任务 → Redis 入队
     ↓
Scheduler 轮询 → `[schedulerPool]` 抢锁/Trigger 扫描 → `[triggerPool]` Executor 执行 → `[12s timeout]` HTTP 回调
     ↓
执行完成 → 更新记录 → 设置幂等标记
```

## 1. 创建定时器

定时器定义一经创建不可修改。系统仅允许后续激活、停用或删除；需要调整 Cron 或回调参数时，删除旧定义并重新创建。

### 数据流

```
HTTP API (POST /timers)
    ↓
Handler.CreateTimer()
    ↓
TimerService.Create()
    ↓
MySQL INSERT → timer_definitions 表
```

### 状态变化

```
新创建 → INACTIVE（默认状态）
```

### 代码示例

```go
// timer_service.go
func (s *TimerService) Create(req *model.CreateTimerDefinitionRequest) (*model.TimerDefinition, error) {
    // 验证 Cron 表达式
    if err := s.parser.ValidateCronExpr(req.CronExpr); err != nil {
        return nil, err
    }
    
    def := &model.TimerDefinition{
        Status: model.TimerStatusInactive,  // 默认未激活
        // ...
    }
    s.defRepo.Create(def)
    return def, nil
}
```

## 2. 激活定时器

### 数据流

```
HTTP API (POST /timers/:id/activate)
    ↓
Handler.ActivateTimer()
    ↓
TimerService.Activate()
    ↓
状态机验证
    ↓
MySQL UPDATE status = 'ACTIVE'
    ↓
计算时间窗口：now ~ now + migrate_step_minutes*2
    ↓
解析 Cron 表达式，计算触发时间点
    ↓
幂等检查 + 创建 MySQL 记录（status = PENDING）
    ↓
按 {time_range}:{bucket} 分组，批量推送到 Redis
```

### 状态变化

```
INACTIVE → ACTIVE（激活）
```

### 状态机规则

```go
// state_machine.go
var timerTransitions = map[TimerStatus][]TimerStatus{
    TimerStatusActive:   {TimerStatusInactive, TimerStatusDeleted},
    TimerStatusInactive: {TimerStatusActive, TimerStatusDeleted},
    TimerStatusDeleted:  {},  // 终态
}
```

## 3. Migrator 预创建任务

### 触发时机

- 等待第一次 ticker 触发（启动时不立即执行）
- 之后每隔 `migrate_step_minutes`（默认 60 分钟）执行一次

### 数据流

```
Migrator.doMigrate()
    ↓
查询 MySQL: SELECT * FROM timer_definitions WHERE status = 'ACTIVE'
    ↓
解析 Cron 表达式，计算触发时间点
时间范围：GetStartHour(now + step) ~ GetStartHour(now + 2*step)（小时级取整）
    ↓
幂等检查: SELECT EXISTS(timer_id + trigger_time)，并由唯一键兜底并发插入
    ↓
MySQL INSERT → timer_records 表（status = PENDING）
    ↓
按 {time_range}:{bucket} 分组
    ↓
Redis ZADD → ZSet（score = trigger_time_unix）
```

### 状态变化

```
记录创建 → PENDING（等待执行）
```

### 分桶规则

```go
bucket := timer_id % bucket_num
timeRange := triggerTime.Format("2006-01-02-15:04")
key := fmt.Sprintf("%s:%d", timeRange, bucket)
```

### Redis 数据结构

```
ZSet Key: chronoflow:timer:2026-05-22-10:30:0
Member: {timer_id}:{trigger_time_ms}
Score: trigger_time_unix
```

## 4. Scheduler 轮询调度

### 触发时机

每隔 `scan_interval`（默认 1 秒）执行一次

### 数据流

```
Scheduler.Run()
    ↓
计算当前 time_range = YYYY-MM-DD-HH:mm
    ↓
同时处理当前分钟和上一分钟，避免边界遗漏
    ↓
分别获取各分钟的动态分桶数（可能不同）
    ↓
遍历桶号 0 ~ bucketNum-1，提交分片处理 worker
    ↓
worker 创建 token = processID_goroutineID
    ↓
SET NX 抢锁（value = token，TTL = lock_expiration，默认 70s）
    ↓
抢到锁 → 同一 worker 运行 Trigger
```

### 分布式锁

```go
lock := queue.NewSchedulerLock(timeRange, bucket) // token = GetProcessAndGoroutineIDStr()
acquired := lock.Lock(ctx, lockExpiration)
```

## 5. Trigger 读取任务

### 数据流

```
Trigger.Run()
    ↓
计算时间片区间 `[minuteStart, minuteStart + 1min)`
    ↓
循环推进 cursor:
    GetTasksByTime(time_range, bucket, cursor, dueEnd)
    ↓
    ZRANGEBYSCORE 获取 `[cursor, dueEnd)` 的任务（只读不删，窗口不重叠）
    ↓
    合并已有 MySQL PENDING 记录（Redis 部分失败兜底）
    ↓
    提交 Executor 到 triggerPool，并推进 cursor
    ↓
完整扫描成功后，Lua 校验 token 并将锁 TTL 设为 success_expiration（默认 130s）
```

### 代码示例

```go
// trigger.go
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int, lock *redis.SchedulerLock) {
    timeSliceStart, timeSliceEnd := parseMinuteRange(timeRange)
    cursor := timeSliceStart
    successExpiration := time.Duration(t.cfg.SuccessExpiration) * time.Second

    for cursor.Before(timeSliceEnd) {
        dueEnd := min(nextWholeSecond(time.Now()), timeSliceEnd)
        if !cursor.Before(dueEnd) { continue }

        triggers, _ := t.queue.GetTasksByTime(ctx, timeRange, bucket, cursor, dueEnd)
        
        // 合并已有 MySQL PENDING 记录，覆盖 Redis 部分写入失败
        dbTriggers, _ := t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum, cursor, dueEnd)
        triggers = mergeTaskTriggers(triggers, dbTriggers)
        
        for _, trigger := range triggers {
            t.triggerPool.Submit(func() {
                t.executor.Execute(ctx, trigger)
            })
        }
        cursor = dueEnd
    }
    lock.Extend(ctx, successExpiration)
}
```

## 6. Executor 执行任务

### 数据流

```
Executor.Execute(trigger)
    ↓
Bloom Filter 快速查询；命中后 MySQL 确认已处理则跳过
    ↓
查询包含状态的定时器定义（内存缓存 → MySQL）
    ↓
按定义状态判断；非 ACTIVE 则跳过
    ↓
原子抢占状态: PENDING → RUNNING
    ↓ 更新行数为 0：跳过
获得执行权
    ↓
使用固定 12s 超时执行 HTTP 回调
    ↓
更新状态: RUNNING → SUCCESS/FAILED
    ↓
执行成功 → Bloom Filter 打点（仅成功时写入）
```

停用或删除定时器不会删除 Redis ZSet 中已经打下的点。点仍可被 Trigger 读取并提交；Executor 按本地定义缓存中的状态判断是否执行，因此持有旧 `ACTIVE` 缓存的节点可能在一个 `step2_duration` 周期内继续发起回调，缓存过期回源 MySQL 后才跳过。

### Bloom 快速过滤、存储唯一性与原子抢占

```
执行前: Bloom Filter 查询
    ↓ 命中且 MySQL 确认已处理
    跳过重复任务
    ↓ miss / 误判 / 查询异常
    继续竞争执行权
建记录: UNIQUE(timer_id, trigger_time)
    ↓ 防止 Migrator/Activate 并发创建重复记录
执行前: UPDATE ... WHERE status = 'PENDING'
    ↓ RowsAffected = 1
    执行任务
    ↓ RowsAffected = 0
    跳过重复派发

Bloom Filter 在成功后写入，并在后续派发前快速查询；命中必须由 MySQL 确认，最终执行权仍由原子状态抢占授予。
```

### 状态变化

```
PENDING → RUNNING → SUCCESS（执行成功）
                  → FAILED（执行失败）
```

### 记录状态流转图

```
                    ┌─────────┐
                    │ PENDING │
                    └────┬────┘
                         ↓
                    ┌─────────┐
                    │ RUNNING │
                    └────┬────┘
                         ↓
        ┌────────────────┴────────────────┐
        ↓                                 ↓
   ┌─────────┐                      ┌─────────┐
   │ SUCCESS │                      │ FAILED  │
   └─────────┘                      └─────────┘
```

## 7. 完成后处理

### 执行成功

```go
// 更新记录状态
record.Status = model.RecordStatusSuccess
record.ResponseCode = responseCode
record.ResponseBody = responseBody
recRepo.Update(record)

// 设置幂等标记
bloom.Set(ctx, bloomKey, bloomVal, 86400)  // Bloom Filter，24 小时过期
```

### 执行失败

```go
record.Status = model.RecordStatusFailed
record.ErrorMessage = err.Error()
```

## 数据存储总结

| 存储 | 数据内容 | 用途 |
|------|----------|------|
| MySQL timer_definitions | 定时器定义（Cron 表达式、回调地址等） | 定时器配置持久化 |
| MySQL timer_records | 执行记录（触发时间、状态、结果等） | 执行历史 |
| Redis ZSet | 待执行任务（score = 触发时间） | 高效任务调度 |
| Redis Lock | 分布式锁（time_range + bucket，owner token） | 降低重复派发 |
| Redis Bloom | 已成功执行任务标记 | 重复任务快速过滤 |
