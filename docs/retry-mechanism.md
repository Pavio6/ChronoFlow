# 重试机制

## 概述

重试机制是 ChronoFlow 可靠性的重要保障，当任务执行失败时，系统会自动进行重试，提高任务执行的成功率。

## 重试策略

### 支持的策略

1. **指数退避（Exponential）**
   - 重试间隔按指数增长
   - 适合网络请求等场景

2. **固定间隔（Fixed）**
   - 重试间隔固定不变
   - 适合定时任务等场景

### 配置说明

```yaml
retry:
  strategy: exponential    # exponential | fixed
  initial_interval: 10     # 初始重试间隔（秒）
  max_interval: 60         # 最大重试间隔（秒）
  multiplier: 3.0          # 指数退避倍数
```

## 重试流程

```
┌──────────────────────────────────────────────────────────────┐
│                      任务执行失败                              │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  检查是否可重试   │
                    └─────────────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
                    ▼                   ▼
          ┌─────────────────┐  ┌─────────────────┐
          │  可以重试        │  │  超过最大次数    │
          └─────────────────┘  └─────────────────┘
                    │                   │
                    ▼                   ▼
          ┌─────────────────┐  ┌─────────────────┐
          │  计算下次重试时间 │  │  标记为失败      │
          └─────────────────┘  └─────────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  更新执行记录     │
          │  status=RETRYING │
          └─────────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  等待重试时间     │
          └─────────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  重新执行任务     │
          └─────────────────┘
```

## 代码实现

### 重试计算器

```go
// Calculator 重试时间计算器
type Calculator struct {
    config Config
}

// Config 重试配置
type Config struct {
    Strategy        Strategy
    InitialInterval int  // 初始间隔（秒）
    MaxInterval     int  // 最大间隔（秒）
    Multiplier      float64 // 倍数
}

// Strategy 重试策略
type Strategy string

const (
    StrategyExponential Strategy = "exponential"
    StrategyFixed       Strategy = "fixed"
)

// CalculateNextRetryTime 计算下次重试时间
func (c *Calculator) CalculateNextRetryTime(retryCount int) time.Time {
    var interval int

    switch c.config.Strategy {
    case StrategyExponential:
        // 指数退避：initial_interval * multiplier^retryCount
        interval = int(float64(c.config.InitialInterval) * math.Pow(c.config.Multiplier, float64(retryCount)))
    case StrategyFixed:
        // 固定间隔
        interval = c.config.InitialInterval
    default:
        interval = c.config.InitialInterval
    }

    // 限制最大间隔
    if interval > c.config.MaxInterval {
        interval = c.config.MaxInterval
    }

    return time.Now().Add(time.Duration(interval) * time.Second)
}
```

### 重试处理

```go
// handleRetry 处理重试逻辑
func (e *Executor) handleRetry(execution *model.TaskExecution, task *model.Task) {
    // 检查是否可重试
    if !execution.IsRetryable(task.MaxRetries) {
        // 超过最大重试次数，标记为失败
        execution.Status = model.ExecutionStatusFAILED
        e.taskRepo.UpdateStatus(task.ID, model.TaskStatusFAILED)

        logger.Warn("task exceeded max retries",
            zap.Int64("task_id", task.ID),
            zap.Int64("execution_id", execution.ID),
            zap.Int("retry_count", execution.RetryCount),
            zap.Int("max_retries", task.MaxRetries),
        )
        return
    }

    // 计算下次重试时间
    nextRetryTime := e.retryCalc.CalculateNextRetryTime(execution.RetryCount)
    execution.NextRetryTime = &nextRetryTime
    execution.RetryCount++
    execution.Status = model.ExecutionStatusRETRYING

    logger.Info("task scheduled for retry",
        zap.Int64("task_id", task.ID),
        zap.Int64("execution_id", execution.ID),
        zap.Int("retry_count", execution.RetryCount),
        zap.Time("next_retry_time", nextRetryTime),
    )
}
```

### 重试执行

```go
// ProcessRetries 处理待重试的任务
func (e *Executor) ProcessRetries(ctx context.Context) {
    executions, err := e.execRepo.GetPendingRetries()
    if err != nil {
        logger.Error("failed to get pending retries", zap.Error(err))
        return
    }

    for _, execution := range executions {
        // 获取任务详情
        task, err := e.taskRepo.GetByID(execution.TaskID)
        if err != nil {
            logger.Error("failed to get task for retry",
                zap.Int64("execution_id", execution.ID),
                zap.Error(err),
            )
            continue
        }

        // 重新提交执行
        e.Submit(execution, task)
    }
}
```

## 重试次数

### 配置说明

每个任务可以配置最大重试次数：

```go
type Task struct {
    // ...
    MaxRetries int `json:"max_retries" gorm:"default:3;comment:最大重试次数"`
    // ...
}
```

### 默认值

- 默认最大重试次数：3 次
- 可在创建任务时自定义

### 重试计数

```go
type TaskExecution struct {
    // ...
    RetryCount int `json:"retry_count" gorm:"default:0;comment:重试次数"`
    // ...
}
```

## 重试时间计算

### 指数退避示例

假设配置：
- `initial_interval`: 10 秒
- `multiplier`: 3.0
- `max_interval`: 60 秒

重试时间计算：

| 重试次数 | 计算公式 | 间隔（秒） | 实际间隔 |
|---------|---------|-----------|---------|
| 0 | 10 * 3^0 | 10 | 10 |
| 1 | 10 * 3^1 | 30 | 30 |
| 2 | 10 * 3^2 | 90 | 60 (限制) |
| 3 | 10 * 3^3 | 270 | 60 (限制) |

### 固定间隔示例

假设配置：
- `initial_interval`: 10 秒

重试时间计算：

| 重试次数 | 间隔（秒） |
|---------|-----------|
| 0 | 10 |
| 1 | 10 |
| 2 | 10 |
| 3 | 10 |

## 超时处理

### 超时检测

```go
// HandleTimeouts 处理超时的执行
func (e *Executor) HandleTimeouts(ctx context.Context) {
    timeout := time.Duration(e.config.Timeout) * time.Second
    executions, err := e.execRepo.GetRunningExecutions(timeout)
    if err != nil {
        logger.Error("failed to get running executions", zap.Error(err))
        return
    }

    for _, execution := range executions {
        execution.Status = model.ExecutionStatusTIMEOUT
        execution.ErrorMessage = "execution timeout"
        finishedAt := time.Now()
        execution.FinishedAt = &finishedAt

        if err := e.execRepo.Update(execution); err != nil {
            logger.Error("failed to update timeout execution",
                zap.Int64("execution_id", execution.ID),
                zap.Error(err),
            )
            continue
        }

        // 处理重试
        task, err := e.taskRepo.GetByID(execution.TaskID)
        if err != nil {
            continue
        }
        e.handleRetry(execution, task)

        logger.Warn("task execution timeout",
            zap.Int64("task_id", execution.TaskID),
            zap.Int64("execution_id", execution.ID),
        )
    }
}
```

### 超时配置

```yaml
executor:
  timeout: 30  # HTTP 回调超时时间（秒）
```

## 重试状态

### 执行记录状态

```go
const (
    ExecutionStatusPENDING  ExecutionStatus = "PENDING"   // 待执行
    ExecutionStatusRUNNING  ExecutionStatus = "RUNNING"   // 执行中
    ExecutionStatusSUCCESS  ExecutionStatus = "SUCCESS"   // 执行成功
    ExecutionStatusFAILED   ExecutionStatus = "FAILED"    // 执行失败
    ExecutionStatusRETRYING ExecutionStatus = "RETRYING"  // 重试中
    ExecutionStatusTIMEOUT  ExecutionStatus = "TIMEOUT"   // 超时
)
```

### 状态流转

```
PENDING → RUNNING → SUCCESS
                  → FAILED
                  → TIMEOUT → RETRYING → RUNNING
```

## 最佳实践

### 1. 合理设置重试次数

- 网络请求：3-5 次
- 数据库操作：1-2 次
- 文件操作：2-3 次

### 2. 选择合适的重试策略

- 网络请求：指数退避
- 定时任务：固定间隔
- 批量处理：指数退避

### 3. 设置合理的超时时间

- 短任务：10-30 秒
- 长任务：1-5 分钟
- 批量任务：5-10 分钟

### 4. 监控重试情况

- 记录重试次数
- 监控重试成功率
- 分析失败原因
