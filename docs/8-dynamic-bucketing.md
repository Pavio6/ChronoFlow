# 动态分桶设计：分钟级原子扩桶

## 1. 目标

ChronoFlow 以分钟分片保存待触发任务。动态分桶的目标是让热点分钟使用更多并行桶，同时保证已经写入 Redis 的任务不会因为桶映射变化而脱离 Scheduler 扫描范围。

本实现补充了在线激活、并发更新和 Redis 不完整投递的处理规则：

```text
任务数量可以增加或失效；
同一分钟已开放的 bucketNum 在分片生命周期内只增不减。
```

MySQL 仍然只有统一的任务记录表，不会按桶创建物理表。动态桶仅影响 Redis ZSet 的 key 和 Scheduler 扫描范围。

## 2. Redis 数据结构

每个分钟分片维护两类元数据和若干队列：

| Key | 类型 | 用途 |
| --- | --- | --- |
| `chronoflow:bucket:{minute}` | String | 当前分钟最终可扫描的桶数 |
| `chronoflow:task_count:{minute}` | String | 该分钟累计注册到队列的新增任务数 |
| `chronoflow:timer:{minute}:{bucket}` | ZSet | 任务队列，score 为触发秒时间戳 |
| `chronoflow:scheduler_lock:{minute}:{bucket}` | String | 分片处理锁 |

例如：

```text
chronoflow:bucket:2026-05-25-10:30 = 3
chronoflow:task_count:2026-05-25-10:30 = 208
chronoflow:timer:2026-05-25-10:30:0
chronoflow:timer:2026-05-25-10:30:1
chronoflow:timer:2026-05-25-10:30:2
```

## 3. 配置

```yaml
scheduler:
  base_bucket_num: 1
  bucket_num: 3
  tasks_per_bucket: 100
  bucket_metadata_ttl: 600
  worker_pool_size: 16

trigger:
  worker_pool_size: 100
```

| 配置项 | 含义 |
| --- | --- |
| `base_bucket_num` | metadata 不存在时的基础扫描桶数，也是最小桶数 |
| `bucket_num` | 单分钟动态扩桶上限 |
| `tasks_per_bucket` | 每桶目标投递数量 |
| `bucket_metadata_ttl` | 时间片结束后，队列和 metadata 继续保留的秒数 |
| `scheduler.worker_pool_size` | 分片扫描与 Trigger 运行的并发容量 |
| `trigger.worker_pool_size` | Executor HTTP 回调的并发容量 |

桶数按累计新增任务数计算：

```text
required = ceil(task_count / tasks_per_bucket)
actual = clamp(required, base_bucket_num, bucket_num)
```

配置为上例时：

| 累计任务数 | 桶数 |
| ---: | ---: |
| `1 - 100` | `1` |
| `101 - 200` | `2` |
| `201+` | `3` |

## 4. 生产路径

`Migrator` 和定时器在线 `Activate` 使用相同流程：

```text
1. 计算触发点。
2. 在 MySQL 创建不存在的 PENDING 执行记录。
3. 只收集本次真正创建成功的记录，并按分钟聚合。
4. 对每分钟调用 ReserveMinuteBuckets。
5. 使用返回的 actualBucketNum 计算 timerID % actualBucketNum。
6. 将新增触发点批量 ZADD 到相应 Redis ZSet。
```

只登记创建成功的记录，可以避免 Migrator 重试或唯一键冲突导致 `task_count` 重复膨胀。

## 5. 原子扩桶

`ReserveMinuteBuckets` 在 Redis Lua 脚本中一次完成：

```text
INCRBY task_count:{minute} newTaskCount
根据累计值计算 requiredBucketNum
读取 bucket:{minute} 当前值
仅在 requiredBucketNum 更大时更新 bucket:{minute}
为两个 metadata key 刷新 TTL
返回最终 actualBucketNum
```

它保证并发生产者不会出现以下错误覆盖：

```text
请求 A 将桶数扩到 3
请求 B 使用较小统计结果又写回 1
```

已有任务不需要在扩桶时迁移。例如任务最初落在 `bucket=0`，随后该分钟从 `1` 扩至 `3`，Scheduler 会扫描 `0..2`，旧任务仍然可见。

反向缩桶是不允许的。若任务已经存在于 `bucket=2`，将桶数从 `3` 降为 `1` 会使该队列不再被扫描。

## 6. 停用与修改

停用、删除或修改定时器不会减少该分钟的 `task_count`，也不会缩小 `bucketNum`。

这里的 `task_count` 表示该分钟曾经为队列注册过的任务规模，不是当前仍有效任务数。旧触发点可以保留在 Redis 中，由执行阶段读取最新定时器状态后跳过失效任务。时间片以及补偿窗口结束后，Redis key 由 TTL 清理。

## 7. Scheduler 与锁

Scheduler 每次轮询分别读取当前分钟和上一分钟的 `bucketNum`：

```text
bucket metadata 存在 -> 扫描 0 .. actualBucketNum-1
bucket metadata 不存在 -> 扫描 0 .. base_bucket_num-1
```

仍然同时处理当前分钟与上一分钟，以覆盖分钟边界附近的投递和调度延迟。

锁粒度保持不变：

```text
(minute, bucket)
```

扩桶只会增加新的独立分片锁，不会改变旧桶已有锁的语义。

分片处理任务进入 `schedulerPool`，与 Trigger 提交 Executor 使用的 `triggerPool` 分离。动态扩桶增加调度扫描并行度时，慢 HTTP 回调不会直接占用分片扫描 worker。

## 8. MySQL 补偿与 Redis 部分失败

任务记录先进入 MySQL，Redis 是执行加速索引。因此可能出现：

```text
MySQL 已成功创建 PENDING 记录
Redis metadata 或 ZADD 写入失败
```

Trigger 每个到期扫描窗口都会同时查询已经创建的记录：

```text
Redis ZSet 到期触发点
MySQL 中该桶对应的 PENDING 记录
```

两者按照 `(timerID, triggerTime)` 合并去重，再提交 Executor。这样即使 Redis 仅写入了一部分任务，也不会因为 Redis 返回非空而跳过数据库中遗漏的 PENDING 任务。

动态扩桶后，历史 Redis 任务可能保留在旧桶中，而 DB 补偿会按当前桶数路由。极端情况下同一个触发点可能从两个桶被提交，最终仍由 MySQL 的原子 `PENDING -> RUNNING` 抢占保证回调只执行一次。

Executor 由 `triggerPool` 承载回调并发，HTTP 客户端固定超时为 `12s`；该资源隔离与超时设置不改变动态桶映射或 DB 补偿规则。

## 9. 与 xTimer 的差异

| 事项 | xTimer 保留但禁用的动态代码 | ChronoFlow 当前实现 |
| --- | --- | --- |
| 桶数依据 | 查询 MySQL 中分钟任务总量 | 原子累计本次实际新增记录 |
| 在线激活 | 未形成完整动态协同路径 | 与 Migrator 共用动态注册路径 |
| 桶数回退 | 代码中未阻止普通覆盖 | Lua 保证只增不减 |
| Redis 部分写入 | 未在动态逻辑中解决 | Trigger 合并 MySQL PENDING |
| MySQL 动态建表 | 否 | 否 |

## 10. 实现位置

| 功能 | 文件 |
| --- | --- |
| 动态配置项 | `internal/config/config.go`, `config/config.yaml` |
| Redis 原子累计与扩桶 | `internal/pkg/redis/queue.go` |
| 统一生产路径 | `internal/service/dynamic_bucket.go` |
| 迁移写入 | `internal/service/migrator.go` |
| 在线激活写入 | `internal/service/timer_service.go` |
| Scheduler 读取分钟桶数 | `internal/service/scheduler.go` |
| DB 部分投递补偿 | `internal/service/trigger.go` |
| 调度池与执行池注入 | `cmd/server/main.go`, `internal/pkg/pool/pool.go` |
| HTTP 回调超时 | `internal/service/executor.go` |
