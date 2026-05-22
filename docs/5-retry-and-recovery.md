# 重试与恢复机制

## 重试策略

### 指数退避（Exponential Backoff）

默认重试策略，间隔公式：

```
delay = initial_interval * multiplier^retry_count
```

配置示例：
```yaml
retry:
  strategy: exponential
  initial_interval: 10  # 初始间隔 10 秒
  max_interval: 60      # 最大间隔 60 秒
  multiplier: 3.0       # 退避倍数
```

重试时间线：
```
失败 → 10s 后重试 → 失败 → 30s 后重试 → 失败 → 60s 后重试 → 失败 → 标记为 FAILED
```

### 固定间隔（Fixed Interval）

每次重试间隔固定：

```yaml
retry:
  strategy: fixed
  initial_interval: 30  # 每次间隔 30 秒
```

### 重试计算器

```go
type Calculator struct {
    cfg Config
}

func (c *Calculator) CalculateNextRetryTime(retryCount int) time.Time {
    var delay time.Duration
    
    switch c.cfg.Strategy {
    case StrategyExponential:
        backoff := float64(c.cfg.InitialInterval) * math.Pow(c.cfg.Multiplier, float64(retryCount))
        delay = time.Duration(backoff)
        if delay > c.cfg.MaxInterval {
            delay = c.cfg.MaxInterval
        }
    case StrategyFixed:
        delay = c.cfg.InitialInterval
    }
    
    return time.Now().Add(delay)
}
```

## 重试流程

```
Executor 执行 HTTP 回调
    │
    ├─ 成功 → 记录 SUCCESS
    │
    └─ 失败 → 检查 retry_count < max_retries
              │
              ├─ 可重试 → 设置 status=RETRYING
              │           设置 next_retry_time
              │           retry_count++
              │
              └─ 不可重试 → 设置 status=FAILED
```

### 重试记录状态变化

```
PENDING → RUNNING → RETRYING → RUNNING → RETRYING → RUNNING → SUCCESS
                                         │
                                         └→ FAILED（超过最大重试次数）
```

### next_retry_time 的作用

`next_retry_time` 字段用于标记下次重试时间。待重试的记录会被定期扫描：

```go
func (r *timerRecordRepo) GetPendingRetries() ([]*model.TimerRecord, error) {
    now := time.Now()
    r.db.Where("status = ? AND next_retry_time <= ?", model.RecordStatusFailed, now).
        Order("next_retry_time ASC").
        Find(&items)
    return items, nil
}
```

## 宕机恢复

### 超时任务检测

当 Executor 在执行过程中崩溃，记录会停留在 RUNNING 状态。系统定期检测超时的 RUNNING 记录：

```go
func (r *timerRecordRepo) GetRunningRecords(timeout time.Duration) ([]*model.TimerRecord, error) {
    threshold := time.Now().Add(-timeout)
    r.db.Where("status = ? AND started_at < ?", model.RecordStatusRunning, threshold).
        Order("started_at ASC").
        Find(&items)
    return items, nil
}
```

### 恢复策略

1. 检测到超时的 RUNNING 记录
2. 将状态重置为 PENDING
3. 重新推入 Redis 队列
4. 等待下一次调度执行

### 状态机保障

```go
var timerTransitions = map[TimerStatus][]TimerStatus{
    TimerStatusActive:   {TimerStatusInactive, TimerStatusDeleted},
    TimerStatusInactive: {TimerStatusActive, TimerStatusDeleted},
    TimerStatusDeleted:  {},
}

var recordTransitions = map[RecordStatus][]RecordStatus{
    RecordStatusPending:  {RecordStatusRunning},
    RecordStatusRunning:  {RecordStatusSuccess, RecordStatusFailed, RecordStatusRetrying, RecordStatusTimeout},
    RecordStatusSuccess:  {},
    RecordStatusFailed:   {RecordStatusRetrying},
    RecordStatusRetrying: {RecordStatusRunning},
    RecordStatusTimeout:  {RecordStatusRunning},
}
```

状态机保证：
- 只有 PENDING 的记录可以变为 RUNNING
- 只有 RUNNING 的记录可以变为 SUCCESS/FAILED/RETRYING/TIMEOUT
- 只有 FAILED 的记录可以重试（RETRYING）
- SUCCESS/FAILED/TIMEOUT 是终态（不可再转换，除了 FAILED → RETRYING）

## Prometheus 监控指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `chronoflow_timer_exec_total` | Counter | 执行总次数（含重试） |
| `chronoflow_timer_exec_duration_ms` | Histogram | 执行耗时分布（毫秒） |
| `chronoflow_timer_exec_success_total` | Counter | 执行成功次数 |
| `chronoflow_timer_exec_failed_total` | Counter | 执行失败次数 |
| `chronoflow_timer_exec_retry_total` | Counter | 重试次数 |
| `chronoflow_timer_trigger_total` | Counter | 触发总次数 |
| `chronoflow_timer_queue_size` | Gauge | 队列当前大小 |

### 告警建议

| 告警条件 | 严重程度 | 说明 |
|----------|----------|------|
| 失败率 > 10% | P1 | 大量任务执行失败 |
| P99 延迟 > 5s | P2 | 执行延迟过高 |
| 重试率 > 20% | P2 | 频繁重试，可能回调服务异常 |
| 队列积压 > 1000 | P3 | 任务处理不及时 |
