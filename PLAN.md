下面是一个适合简历的 **Go 分布式定时任务调度系统开发计划**。

## 项目名称

```text
Go Distributed Task Scheduler
```

## 技术栈

```text
语言：Go
Web 框架：Gin
数据库：MySQL / PostgreSQL
缓存与调度队列：Redis ZSet
分布式锁：Redis SETNX + Lua
Cron 解析：robfig/cron
并发控制：goroutine / worker pool
部署：Docker / Docker Compose
日志：zap
配置：viper
```

## 第一阶段：基础功能

目标：实现一个可用的分布式定时器。

核心功能：

```text
任务创建
任务修改
任务删除
任务启用 / 停用
任务查询
Cron 表达式解析
计算 next_trigger_time
HTTP Callback 执行
执行结果记录
```

核心流程：

```text
创建任务
  ↓
保存到 MySQL
  ↓
Scheduler 定期扫描任务
  ↓
计算下一次触发时间
  ↓
写入 Redis ZSet
  ↓
Trigger 从 Redis 取到期任务
  ↓
Executor 执行 HTTP 回调
  ↓
保存执行日志
```

主要数据表：

```text
tasks
task_executions
```

---

## 第二阶段：可靠性增强

目标：让任务执行更稳定，适合写进简历。

需要完成：

```text
失败重试
执行日志详情
手动触发一次
任务状态机
```

任务状态：

```text
INIT
ENABLED
DISABLED
RUNNING
SUCCESS
FAILED
DELETED
```

执行记录状态：

```text
PENDING
RUNNING
SUCCESS
FAILED
RETRYING
```

失败重试策略：

```text
第一次失败：10 秒后重试
第二次失败：30 秒后重试
第三次失败：60 秒后重试
超过最大次数：标记失败
```

这一阶段可以写到简历：

```text
设计任务状态机与失败重试机制，支持手动触发和完整执行日志记录。
```

---

## 第三阶段：分布式能力

目标：支持多实例部署，避免任务重复执行。

需要完成：

```text
Redis 分布式锁
Lua 原子取任务
多 Scheduler 实例
任务幂等控制
宕机恢复
```

关键点：

```text
同一个 task_id + trigger_time 只能执行一次
```

Redis Lua 做：

```text
ZRANGEBYSCORE 获取到期任务
ZREM 删除任务
```

保证取任务和删除任务是原子的。

幂等方式：

```text
task_id + trigger_time 建唯一索引
Redis SETNX 防重复
execution_id 防重复执行
```

宕机恢复：

```text
异常退出后由恢复机制识别未完成记录并重新调度
```

这一阶段简历含金量最高：

```text
使用 Redis Lua 脚本保证到期任务原子获取与删除，并通过分布式锁和幂等键解决多实例调度下任务重复执行问题。
```

---

## 推荐最终功能范围

不要做太大，建议最终完成这些：

```text
任务 CRUD
Cron 调度
Redis ZSet 延迟触发
HTTP Callback
执行日志
失败重试
手动触发
Redis Lua 原子取任务
幂等控制
宕机恢复
Docker Compose 部署
```

## 简历项目描述

```text
基于 Go 实现分布式定时任务调度系统，支持 Cron 表达式、任务 CRUD、HTTP Callback、手动触发、执行日志和失败重试。使用 Redis ZSet 管理任务触发时间，并通过 Redis Lua 脚本保证到期任务原子获取与删除；设计幂等键和任务状态机，解决多实例部署下任务重复执行和宕机恢复问题。
```
