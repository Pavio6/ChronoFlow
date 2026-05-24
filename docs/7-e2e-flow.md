# E2E 流程详解

本文档描述从创建定时器到任务执行完成的完整流程，包括数据流和状态变化。

## 流程概览

```
用户创建定时器 → 激活 → Migrator 预创建任务 → Redis 入队
     ↓
Scheduler 轮询 → 抢锁 → Trigger 读取任务 → Executor 执行
     ↓
执行完成 → 更新记录 → 设置幂等标记
```

## 1. 创建定时器

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
HTTP API (PUT /timers/:id/activate)
    ↓
Handler.ActivateTimer()
    ↓
TimerService.Activate()
    ↓
状态机验证
    ↓
计算时间窗口：now ~ now + step1_duration*2
    ↓
解析 Cron 表达式，计算触发时间点
    ↓
幂等检查 + 创建 MySQL 记录（status = PENDING）
    ↓
按 {time_range}:{bucket} 分组，批量推送到 Redis
    ↓
MySQL UPDATE status = 'ACTIVE'
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

- 等待第一次 ticker 触发（启动时不立即执行，冷启动任务由 Trigger DB 回退兜底）
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
幂等检查: SELECT EXISTS(timer_id + trigger_time)
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
遍历桶号 0 ~ bucket_num-1
    ↓
SETNX 抢锁（TTL = scan_interval * 2）
    ↓
抢到锁 → 提交 Trigger 到协程池
```

### 分布式锁

```go
lockKey := fmt.Sprintf("chronoflow:scheduler_lock:%s:%d", timeRange, bucket)
acquired := redis.SetNX(lockKey, "1", lockExpiration)
```

## 5. Trigger 读取任务

### 数据流

```
Trigger.Run()
    ↓
计算时间片结束时间（当前分钟的 :59）
    ↓
循环: GetDueTasks(time_range, bucket)
    ↓
ZRANGEBYSCORE 获取 score <= now 的任务（只读不删）
    ↓
Redis 无结果 → 回退查询 MySQL（冷启动兜底）
    ↓
提交 Executor 到协程池
    ↓
续期锁 TTL
```

### 代码示例

```go
// trigger.go
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int) {
    timeSliceEnd := calculateTimeSliceEnd(timeRange)
    
    for {
        if time.Now().After(timeSliceEnd) {
            break  // 时间片结束
        }
        
        // 先查 Redis
        triggers, _ := t.queue.GetDueTasks(ctx, timeRange, bucket, batchSize)
        
        // Redis 无结果时回退查询 MySQL（冷启动兜底）
        if len(triggers) == 0 {
            triggers, _ = t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum)
        }
        
        for _, trigger := range triggers {
            t.pool.Submit(func() {
                t.executor.Execute(ctx, trigger)
            })
        }
        
        // 续期锁
        t.queue.ExtendSchedulerLock(ctx, timeRange, bucket, lockExpiration)
    }
}
```

## 6. Executor 执行任务

### 数据流

```
Executor.Execute(trigger)
    ↓
两层幂等去重检查
    ↓
查询定时器定义（内存缓存 → MySQL）
    ↓
查找/创建执行记录
    ↓
更新状态: PENDING → RUNNING
    ↓
执行 HTTP 回调
    ↓
更新状态: RUNNING → SUCCESS/FAILED/RETRYING
    ↓
执行成功 → Bloom Filter 打点（仅成功时写入）
```

### 两层幂等去重

```
第 1 层: Bloom Filter 快速查重
    ↓ 未命中（100% 确定未执行）
    执行任务
    ↓ 命中（可能有误判）
第 2 层: MySQL 查记录状态
    ↓ status == PENDING（bloom 误判）
    执行任务
    ↓ status != PENDING（已执行）
    跳过
```

### 状态变化

```
PENDING → RUNNING → SUCCESS（执行成功）
                  → FAILED（执行失败）
                  → RETRYING（可重试，等待重试）
                  → TIMEOUT（执行超时）
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
        ┌────────────────┼────────────────┐
        ↓                ↓                ↓
   ┌─────────┐     ┌─────────┐     ┌─────────┐
   │ SUCCESS │     │ FAILED  │     │ TIMEOUT │
   └─────────┘     └────┬────┘     └─────────┘
                        ↓
                   ┌─────────┐
                   │ RETRYING│ ←── 重试次数 < max_retries
                   └────┬────┘
                        ↓
                   (重新执行)
```

## 7. 完成后处理

### 执行成功

```go
// 更新记录状态
record.Status = model.RecordStatusSuccess
record.ResponseCode = responseCode
record.ResponseBody = responseBody
recRepo.Update(record)

// 设置幂等标记（仅成功时写入，防止失败任务被标记导致无法重试）
bloom.Set(ctx, bloomKey, bloomVal, 86400)  // Bloom Filter，24 小时过期
```

### 执行失败

```go
record.Status = model.RecordStatusFailed
record.ErrorMessage = err.Error()

// 检查是否可重试
if record.IsRetryable(maxRetries) {
    record.Status = model.RecordStatusRetrying
    record.RetryCount++
    nextRetryTime := retryCalc.CalculateNextRetryTime(record.RetryCount)
    record.NextRetryTime = &nextRetryTime
}
```

## 数据存储总结

| 存储 | 数据内容 | 用途 |
|------|----------|------|
| MySQL timer_definitions | 定时器定义（Cron 表达式、回调地址等） | 定时器配置持久化 |
| MySQL timer_records | 执行记录（触发时间、状态、结果等） | 执行历史、重试管理 |
| Redis ZSet | 待执行任务（score = 触发时间） | 高效任务调度 |
| Redis Lock | 分布式锁（time_range + bucket） | 防止重复调度 |
| Redis Bloom | 已执行任务标记 | 快速幂等检查 |
| Redis Idempotent | 幂等键（timer_id + trigger_time） | 精确幂等检查 |
