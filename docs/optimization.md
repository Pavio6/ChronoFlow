# 优化点

## 1. 动态分桶

### 问题

当前实现采用静态分桶，`bucket_num` 在配置文件中写死，运行时不可调整。当任务规模变化时，无法动态扩缩分桶数量。

### 解决方案

根据每个时间步的任务数量动态计算分桶数，存储映射关系到 Redis。

### 分桶规则

```
bucket_num = min(max(task_count / 100, 1), max_bucket)
```

- 每 100 个任务一个桶，最少 1 个桶
- 不超过配置中的最大桶数 `bucket_num`

### 实现

- `internal/pkg/redis/queue.go`：添加 `SetBucketNum`/`GetBucketNum` 方法
- `internal/service/migrator.go`：统计任务数量，计算动态分桶数，存储映射到 Redis
- `internal/service/scheduler.go`：从 Redis 读取动态分桶数，遍历桶
- `internal/service/timer_service.go`：激活时使用动态分桶

### 已完成

- [x] Migrator 统计每个时间步的任务数量
- [x] 根据任务数量计算动态分桶数
- [x] 存储 `time_range → bucket_num` 映射到 Redis
- [x] Scheduler 从 Redis 读取动态分桶数
- [x] Activate 时使用动态分桶

## 2. 激活时立即同步任务到 Redis

### 问题

新创建的定时器激活后，需要等待最长 60 分钟才会被 Migrator 处理，任务无法立即被调度。

### 解决方案

在 `Activate` 方法中，激活定时器时立即同步未来 `migrate_step_minutes*2` 时间窗口内的任务到 Redis。

### 实现

- `internal/pkg/cron/parser.go`：新增 `NextTriggerTimesBefore` 方法
- `internal/service/timer_service.go`：修改 `Activate` 方法，激活时立即创建记录并同步 Redis
- `cmd/server/main.go`：传入 `RedisQueue` 和 `SchedulerConfig` 依赖

### 已完成

- [x] 激活时立即同步任务到 Redis
- [x] 幂等性检查（MySQL 唯一键）
- [x] 错误处理（Redis 同步失败返回错误）
- [x] 配置项统一改为分钟单位（`migrate_step_minutes`）
