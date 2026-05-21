# ChronoFlow 系统架构

## 整体架构

ChronoFlow 是一个分布式定时任务调度系统，采用经典的调度-触发-执行三阶段架构。

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  Scheduler  │───▶│   Redis     │───▶│   Trigger   │
│  调度器      │    │   ZSet      │    │   触发器     │
└─────────────┘    └─────────────┘    └─────────────┘
       │                                     │
       ▼                                     ▼
┌─────────────┐                      ┌─────────────┐
│   MySQL     │                      │   Executor  │
│   数据库     │                      │   执行器     │
└─────────────┘                      └─────────────┘
```

## 核心组件

### 1. Scheduler（调度器）

**职责**：定期扫描数据库中的任务，计算下次触发时间，推入 Redis 队列。

**工作流程**：
1. 定时扫描 `tasks` 表中需要调度的任务
2. 使用 Cron 解析器计算下次触发时间
3. 更新数据库中的 `next_trigger_time`
4. 将任务推入 Redis ZSet（score 为触发时间戳）

**配置参数**：
- `scan_interval`：扫描间隔（秒）
- `batch_size`：每批扫描任务数

### 2. Trigger（触发器）

**职责**：从 Redis 队列取出到期任务，创建执行记录，交给执行器处理。

**工作流程**：
1. 每秒轮询 Redis ZSet，取出到期任务
2. 检查幂等性（防止重复执行）
3. 获取分布式锁
4. 创建执行记录
5. 提交到执行器异步执行

**分布式保障**：
- Lua 脚本原子取任务
- SETNX 分布式锁
- 幂等键防重复

### 3. Executor（执行器）

**职责**：执行 HTTP 回调，处理重试逻辑和超时控制。

**工作流程**：
1. 从工作池获取令牌（控制并发）
2. 更新执行状态为运行中
3. 执行 HTTP 回调请求
4. 记录执行结果（成功/失败）
5. 处理重试逻辑

**配置参数**：
- `timeout`：HTTP 回调超时时间（秒）
- `worker_pool_size`：工作池大小
- `max_retries`：最大重试次数

## 数据存储

### MySQL

- `tasks`：任务配置表
- `task_executions`：执行记录表

### Redis

- `chronoflow:task_queue`：任务调度队列（ZSet）
- `chronoflow:task_lock:{task_id}:{trigger_time}`：分布式锁
- `chronoflow:idempotent:{task_id}:{trigger_time}`：幂等键

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.26.2+ |
| Web 框架 | Gin |
| 数据库 | MySQL |
| 缓存与队列 | Redis ZSet |
| Cron 解析 | robfig/cron |
| 日志 | zap |
| 配置 | viper |
| 前端 | React + TypeScript + Ant Design |
