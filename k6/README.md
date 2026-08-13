# ChronoFlow 单实例压测

## 测试目标

首轮压测固定为 API、Scheduler、Dispatcher、Worker 各 1 个实例，验证单实例吞吐上限、端到端延迟、积压恢复能力和执行正确性

本机同时运行 k6、ChronoFlow、MySQL、Redis 和监控组件，结果只作为开发机基线，不能直接代表生产容量

## 当前测试机器

| 项目 | 配置 |
| --- | --- |
| 机型 | MacBook Air（Mac16,13） |
| 芯片 | Apple M4 |
| CPU | 10 核（4 个性能核、6 个能效核） |
| 内存 | 24 GB |
| 架构 | ARM64 |
| 系统 | macOS，Darwin 25.6.0 |

每次测试还需记录 Docker Desktop 实际分配的 CPU 和内存，因为容器可用资源可能小于整机资源

## 如何压测

安装 k6：

```bash
brew install k6
k6 version
```

启动基础设施并执行迁移：

```bash
make dev-start
make migrate-up
```

分别打开 4 个终端。API 和 Worker 需要通过测试环境变量允许本地回调：

```bash
CHRONOFLOW_SECURITY_ALLOW_PRIVATE_CALLBACKS=true make dev-api
make dev-scheduler
make dev-dispatcher
CHRONOFLOW_SECURITY_ALLOW_PRIVATE_CALLBACKS=true make dev-worker
```

不要使用 `make dev-backend`，该命令会通过 `cmd/all` 在一个进程中启动全部角色，不符合本轮独立单实例压测模型

启动固定返回 HTTP 204 的轻量回调接收器：

```bash
go run ./k6/callback
```

首轮按以下阶梯执行，每档持续 5～10 分钟：

1. 预热：10 个每秒触发的 Timer，持续 2 分钟
2. 阶梯：10、50、100、200 个每秒触发的 Timer
3. 每档结束后停止新增负载，等待 Outbox、Redis Pending 和 Execution 全部排空
4. 上一档满足验收线后再提高负载；首次不满足时即为当前单实例容量边界

先单独测试 API：

```bash
mkdir -p k6/results
VUS=10 DURATION=5m k6 run \
  --summary-export=k6/results/api-summary.json \
  k6/api.js
```

再测试完整调度链路。`TIMER_COUNT` 就是每秒任务数：

```bash
TIMER_COUNT=10 DURATION=5m k6 run \
  --summary-export=k6/results/flow-10-summary.json \
  k6/timer-flow.js
```

依次将 `TIMER_COUNT` 改为 50、100、200。脚本会创建和激活 Timer，测试结束后自动停用、等待排空并逻辑删除本轮 Timer

可通过 `BASE_URL`、`CALLBACK_BASE_URL`、`API_KEY`、`DRAIN_TIMEOUT_SECONDS` 覆盖默认参数。`CALLBACK_STATUS` 和 `CALLBACK_DELAY_MS` 可用于构造失败或慢回调，但首轮容量测试保持默认的 204 和 0 延迟

## 核心指标与首轮验收线

| 指标 | 首轮验收线 | 数据来源 |
| --- | --- | --- |
| API 请求错误率 | `< 0.1%` | k6 |
| API 延迟 | `p95 < 200ms`，`p99 < 500ms` | k6 |
| 调度延迟 | `p95 < 1s`，`p99 < 3s` | `timer_executions.created_at - scheduled_at` |
| 端到端延迟 | `p95 < 2s`，`p99 < 5s` | `timer_executions.finished_at - scheduled_at` |
| 正常回调成功率 | `>= 99.9%` | `chronoflow_worker_executions_total` |
| Execution 漏创建 | `0` | 预期触发数与 `timer_executions` 数量对比 |
| Execution 重复记录 | `0` | `(timer_id, scheduled_at)` 唯一性与重复指标 |
| 健康场景重复回调 | `0` | 回调服务按幂等标识计数 |
| Outbox 积压 | 停止施压后 30 秒内归零 | `chronoflow_outbox_unpublished_count` |
| Redis Pending 积压 | 停止施压后 30 秒内归零 | `chronoflow_worker_pending_messages` |
| Lease 丢失 | 健康场景为 `0` | `chronoflow_worker_lease_lost_total` |

重点观察 `chronoflow_scheduler_batch_duration_seconds`、`chronoflow_worker_execution_duration_seconds`、CPU、内存、MySQL 连接数和 Redis 内存。达到资源瓶颈、积压无法回落或延迟持续越线时，记录该档负载与瓶颈组件
