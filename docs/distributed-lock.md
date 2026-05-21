# 分布式锁实现

## 概述

分布式锁是 ChronoFlow 实现多实例部署的关键机制，防止同一任务被多个实例同时执行。

## 实现方案

### Redis SETNX 实现

使用 Redis 的 `SETNX`（SET if Not eXists）命令实现分布式锁：

```go
// AcquireTaskLock 获取任务执行锁
func (q *RedisQueue) AcquireTaskLock(ctx context.Context, taskID int64, triggerTime time.Time, expiration time.Duration) (bool, error) {
    key := fmt.Sprintf("%s%d:%s", TaskLockPrefix, taskID, triggerTime.Format(time.RFC3339))
    result, err := q.client.SetNX(ctx, key, "1", expiration).Result()
    if err != nil {
        return false, fmt.Errorf("failed to acquire task lock: %w", err)
    }
    return result, nil
}
```

### 锁的 Key 设计

```
chronoflow:task_lock:{task_id}:{trigger_time}
```

**示例**：
```
chronoflow:task_lock:123:2024-01-01T00:00:00Z
```

**设计考虑**：
- `task_id`：任务唯一标识
- `trigger_time`：触发时间，精确到秒
- 组合保证同一任务在同一触发时间只有一个实例能获取锁

### 锁的过期时间

```go
locked, err := t.redisQueue.AcquireTaskLock(ctx, trigger.TaskID, trigger.TriggerTime, 5*time.Minute)
```

**默认过期时间**：5 分钟

**过期时间的作用**：
1. 防死锁：实例崩溃后锁自动释放
2. 资源回收：避免锁长期占用

## 锁的使用流程

```
┌──────────────────────────────────────────────────────────────┐
│                      任务执行流程                              │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  从 Redis 取任务  │
                    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  检查幂等性       │
                    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  获取分布式锁     │
                    └─────────────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
                    ▼                   ▼
          ┌─────────────────┐  ┌─────────────────┐
          │  获取成功        │  │  获取失败        │
          └─────────────────┘  └─────────────────┘
                    │                   │
                    ▼                   ▼
          ┌─────────────────┐  ┌─────────────────┐
          │  设置幂等键       │  │  跳过执行        │
          └─────────────────┘  │  (其他实例处理)   │
                    │          └─────────────────┘
                    ▼
          ┌─────────────────┐
          │  创建执行记录     │
          └─────────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  释放锁          │
          └─────────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  提交到执行器     │
          └─────────────────┘
```

## 代码实现

### 获取锁

```go
// processTrigger 处理单个任务触发
func (t *Trigger) processTrigger(ctx context.Context, trigger *redis.TaskTrigger) {
    // 检查幂等性
    isIdempotent, err := t.redisQueue.IsIdempotent(ctx, trigger.TaskID, trigger.TriggerTime)
    if err != nil {
        // 错误处理
        return
    }
    if isIdempotent {
        // 已执行过，跳过
        return
    }

    // 获取分布式锁
    locked, err := t.redisQueue.AcquireTaskLock(ctx, trigger.TaskID, trigger.TriggerTime, 5*time.Minute)
    if err != nil {
        // 错误处理
        return
    }
    if !locked {
        // 其他实例已获取锁，跳过
        return
    }

    // 后续处理...
}
```

### 释放锁

```go
// 释放锁
t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
```

**释放时机**：
1. 创建执行记录后
2. 任务状态检查失败后
3. 幂等键设置失败后

## 安全性保证

### 1. 原子性

`SETNX` 命令是原子操作，保证只有一个客户端能成功设置键：

```go
result, err := q.client.SetNX(ctx, key, "1", expiration).Result()
```

### 2. 过期时间

设置过期时间防止死锁：

```go
result, err := q.client.SetNX(ctx, key, "1", expiration).Result()
```

### 3. 释放锁

显式释放锁，避免等待过期：

```go
func (q *RedisQueue) ReleaseTaskLock(ctx context.Context, taskID int64, triggerTime time.Time) error {
    key := fmt.Sprintf("%s%d:%s", TaskLockPrefix, taskID, triggerTime.Format(time.RFC3339))
    return q.client.Del(ctx, key).Err()
}
```

## 多实例部署

### 场景说明

假设有 3 个 ChronoFlow 实例同时运行：

```
Instance A ──┐
Instance B ──┼──▶ Redis ──▶ MySQL
Instance C ──┘
```

### 执行流程

1. **Instance A** 取出任务，获取锁成功
2. **Instance B** 取出同一任务，获取锁失败，跳过
3. **Instance C** 取出同一任务，获取锁失败，跳过
4. **Instance A** 执行任务，完成后释放锁

### 结果

- 只有 Instance A 执行了任务
- 避免了重复执行
- 保证了任务的幂等性

## 注意事项

### 1. 锁的粒度

锁的粒度是 `task_id + trigger_time`，保证：
- 同一任务的不同触发时间可以并行执行
- 同一任务的同一触发时间只能执行一次

### 2. 锁的超时

锁的超时时间应该大于任务执行时间：
- 默认 5 分钟
- 可根据任务执行时间调整

### 3. 锁的释放

必须在所有路径上释放锁：
- 正常完成
- 异常退出
- 状态检查失败

### 4. 网络分区

在网络分区情况下：
- 锁可能被多个实例获取
- 通过幂等键进一步保证唯一性
- 通过数据库唯一索引最终保证一致性
