# 生产级 MVP 重构记录

> 状态：核心重构已完成。本文保留关键决策、验收标准和后续边界，不再描述已经删除的实现细节。

## 1. 重构目标

本轮重构把项目收敛为一套可独立部署、可测试、可恢复的分布式定时任务架构：

- API、Scheduler、Dispatcher、Worker 可以分别运行和扩缩容；
- MySQL 是 Timer、Execution 和 Outbox 的权威存储；
- MySQL 到 Redis 采用 Transactional Outbox；
- Redis Streams 提供跨进程至少一次投递；
- ants 控制 Worker 单实例并发；
- 崩溃、Redis 短时故障和重复消息可以自动恢复；
- 具备基本的安全、指标、健康检查和运维文档。

## 2. 已完成的结构调整

### 运行角色

单个 `chronoflow` 制品提供：

```text
chronoflow api
chronoflow scheduler
chronoflow dispatcher
chronoflow worker
chronoflow all
```

`all` 只用于本地组合运行。Docker Compose 默认启动前四个独立角色，验证真实的进程边界。

### 调度

- Timer 使用单一 `next_fire_at` 权威游标。
- Scheduler 使用 MySQL 8 `FOR UPDATE SKIP LOCKED` 多副本领取。
- Execution 唯一键抵御重复生成。
- 支持 `SKIP`、`FIRE_ONCE`、`CATCH_UP` 三种 misfire 策略。
- Scheduler 不依赖 Redis。

### 投递

- Execution、Outbox 和游标推进在同一 MySQL 事务内。
- Dispatcher 使用有期限的 Claim 多副本发布。
- Redis Streams 使用 Consumer Group、Pending、XACK 和 XAUTOCLAIM。
- Redis 故障期间 Outbox 持续积压，恢复后补投。

### 执行

- Worker 在 HTTP 回调前抢占 MySQL Lease。
- 心跳续租，`run_token` 阻止过期执行者提交结果。
- ants Pool 提供单实例有界并发。
- 网络错误、超时、408、425、429、5xx 退避重试；确定性 4xx 直接失败。
- 响应体有大小限制，错误和日志避免泄露回调头与完整敏感快照。

### 恢复与保留

- Reconciler 重新投递陈旧 PENDING。
- 回收租约过期 RUNNING。
- 处理到期 RETRY_WAIT 和重试耗尽状态。
- 定期清理已发布 Outbox、终态 Execution 和已确认 Stream 历史。
- Stream 清理保留最早 Pending 边界。

### API 与安全

- Timer 和 Execution 使用一套明确 API，不再暴露重复的旧执行记录接口。
- API Key 支持 `X-API-Key` 与 Bearer Token。
- CORS 使用显式白名单。
- 默认阻止私网、回环和保留地址回调，降低 SSRF 风险。
- `/health`、`/ready`、`/metrics` 按角色提供。

### 可观测性

- Scheduler 批次、Execution 生成和重复防护指标；
- Outbox 发布结果与积压；
- Worker 执行、时延、重试、Lease 丢失和重新投递；
- Reconciler 动作与失败；
- Execution 状态 Gauge；
- Prometheus 抓取四个独立角色，Grafana 提供统一面板和告警。

## 3. 已删除的旧实现

以下概念不再属于运行时：

- 提前扫描未来窗口并批量预生成任务；
- 分钟时间片和动态分桶；
- Redis ZSet 定时队列；
- Redis Scheduler 锁；
- 进程内 Trigger → Executor 传递链；
- 旧执行记录模型和 API；
- 双引擎配置与运行分支。

代码、测试、配置、前端类型和文档已经按当前架构收敛。数据库升级链中的旧表仅作为历史迁移入口存在，最终由 `004_remove_obsolete_scheduler.sql` 删除。

## 4. 数据迁移策略

既有环境升级顺序：

1. 停止旧任务生成和消费进程；
2. 备份数据库；
3. 归档仍需审计的旧执行历史；
4. 按文件名顺序执行 `migrations/002`、`003`、`004`；
5. 检查 ACTIVE Timer 的时区和下次触发时间；
6. 启动 API 与 Scheduler，观察新 Execution/Outbox；
7. 启动 Dispatcher 与 Worker；
8. 确认无旧进程后，按明确前缀清理旧 Redis Key。

迁移必须由单一发布任务执行，不能依赖多个 API 副本同时运行 AutoMigrate。UTC 切换前应确认既有 MySQL `DATETIME` 的实际语义。

## 5. MVP 验收标准

### 正确性

- 同一 `(timer_id, scheduled_at)` 永不出现两条 Execution；
- 激活、停用、删除与 Scheduler 并发时不会静默覆盖状态；
- 每条新 Execution 与 Outbox 原子提交；
- 重复 Redis 消息不会让终态 Execution 再次回调；
- 旧 `run_token` 无法提交结果。

### 可恢复性

- Scheduler 任意时刻崩溃后可继续推进；
- Redis 停机时不丢失已提交 Execution；
- Dispatcher 在 XADD 前后崩溃都可恢复；
- Worker 崩溃后 Pending 消息和过期 Lease 可接管；
- Retry Outbox 与 Execution 状态原子提交。

### 可部署性

- 四个角色可以使用同一镜像独立启动；
- API/Scheduler 在 Redis 不可用时仍能保持就绪；
- 每个角色拥有独立健康与指标端口；
- Scheduler、Dispatcher、Worker 均支持多副本；
- `docker compose config` 可以验证完整拓扑。

### 工程质量

- `go test -race ./...`
- `go vet ./...`
- `go build ./cmd/chronoflow`
- 前端 lint 与 build
- Docker Compose 配置校验
- 关键调度、Outbox、Worker、恢复和安全分支有自动化测试

## 6. 生产上线前仍需由部署环境完成

代码达到 MVP 并不替代生产平台能力。正式上线还需要：

- 使用 Secret Manager 注入数据库、Redis、API Key 和下游凭证；
- MySQL 高可用、备份和时间同步；
- Redis 持久化/高可用与内存策略；
- 入口网关 TLS、鉴权、限流和运维端点访问控制；
- 基于真实容量的压测和故障演练；
- 回调方确认幂等协议；
- 告警接收渠道和值班流程；
- 灰度迁移、回滚脚本和数据归档确认。

## 7. 后续演进

按优先级建议：

1. 为回调自动注入稳定的 Execution ID 幂等头并形成公开协议；
2. 增加手动重试、取消和审计事件；
3. 增加租户级限流、Worker 队列隔离和回调域名策略；
4. 引入 OpenTelemetry Trace；
5. 当吞吐或保留需求超过 Redis Streams 边界时，再评估 Kafka；
6. DAG、任务分片和资源编排作为独立产品能力设计，不塞入当前 Scheduler。

Kafka 不是当前可靠性的前提。Outbox 使投递介质可以替换；在 MVP 阶段使用 Redis Streams 能减少运维复杂度，同时保留清晰的迁移边界。
