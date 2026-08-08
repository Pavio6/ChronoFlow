# ChronoFlow

ChronoFlow 是一个 Go 实现的分布式定时任务调度系统。MySQL 保存权威调度状态和执行状态，Transactional Outbox 保证任务不会因 MySQL/Redis 双写失败而丢失，Redis Streams 负责跨进程投递，Worker 使用 ants 控制单实例并发。

生产环境有四个独立进程和独立镜像：API、Scheduler、Dispatcher、Worker。它们共享同一代码仓库、领域模型和 MySQL/Redis 协作协议，但可单独发布、扩缩容和部署到不同机器。`all` 仅用于本地联调；数据库迁移是发布前命令，不是常驻服务。

## 架构

```mermaid
flowchart LR
    Client["Client"] --> API["API"]
    API --> MySQL[("MySQL")]
    Scheduler["Scheduler + Reconciler"] --> MySQL
    MySQL --> Dispatcher["Outbox Dispatcher"]
    Dispatcher --> Stream[("Redis Streams")]
    Stream --> Worker["Worker + ants"]
    Worker --> Callback["HTTP Callback"]
    Worker --> MySQL
```

| 角色 | 职责 | 依赖 | 扩容方式 |
| --- | --- | --- | --- |
| `api` | Timer 管理、执行查询、监控接口、静态前端 | MySQL | 无状态横向扩容 |
| `scheduler` | 领取到期 Timer、创建 Execution/Outbox、推进 `next_fire_at`、恢复异常执行 | MySQL | 多副本，MySQL `SKIP LOCKED` 协调 |
| `dispatcher` | 将已提交 Outbox 可靠发布到 Redis Streams | MySQL、Redis | 多副本，Outbox Lease 协调 |
| `worker` | Consumer Group 消费、MySQL Lease 抢占、ants 并发执行回调、重试 | MySQL、Redis | 按回调吞吐横向扩容 |
| `all` | 在一个进程中运行全部角色 | MySQL、Redis | 仅用于本地开发或小规模演示 |

生产构建产物：

```text
chronoflow-api
chronoflow-scheduler
chronoflow-dispatcher
chronoflow-worker
chronoflow-migrate            # 发布/运维工具，不是常驻服务
chronoflow-all                # 仅本地联调
```

核心语义：

- `(timer_id, scheduled_at)` 唯一键保证同一计划触发点只生成一条执行记录。
- Execution、`next_fire_at` 和 Outbox 在同一个 MySQL 事务内提交。
- Redis Streams 是至少一次投递；Worker 通过 MySQL Lease 和 `run_token` 抵御重复消息与过期结果。
- Redis 暂时不可用时，Scheduler 仍可继续写 Outbox；恢复后 Dispatcher 自动补投。
- API 和 Scheduler 不依赖 Redis，Dispatcher 和 Worker 才依赖 Redis。

## 快速启动

要求：Go 1.26.2+、Node.js 22+、Docker Compose。

本地首次启动时，先启动基础依赖并执行迁移：

```bash
make dev-start
make migrate-up
```

然后启动全部独立角色：

```bash
docker compose up -d --build
```

> Docker Compose 不会自动迁移数据库。生产发布也必须在发布 API、Scheduler、Dispatcher、Worker 前，由 CI/CD 或运维人员显式执行迁移。

服务地址：

- API/UI：`http://localhost:8080`
- Scheduler 健康与指标：`http://localhost:8081`
- Dispatcher 健康与指标：`http://localhost:8082`
- Worker 健康与指标：`http://localhost:8083`
- Prometheus：`http://localhost:9090`
- Grafana：`http://localhost:3001`

本地开发可先启动依赖，再运行组合角色：

```bash
make dev-start
make migrate-up
make dev-app
make dev-frontend
```

也可以在不同终端分别运行四个角色：

```bash
make dev-api
make dev-scheduler
make dev-dispatcher
make dev-worker
```

## 构建与测试

```bash
make build
make test
go vet ./...
docker compose config
```

`make build` 会在 `bin/` 中生成独立产物：

```bash
bin/chronoflow-api
bin/chronoflow-scheduler
bin/chronoflow-dispatcher
bin/chronoflow-worker
bin/chronoflow-migrate
bin/chronoflow-all
```

Dockerfile 提供 `api`、`scheduler`、`dispatcher`、`worker`、`migrate`、`all` 六个 build target。Compose 分别构建四个常驻角色镜像；Worker 和 Scheduler 的副本数可以独立调整。

## 创建任务

ChronoFlow 使用六字段 Cron：`秒 分 时 日 月 周`。创建后 Timer 默认为 `INACTIVE`，激活时才计算第一个 `next_fire_at`。

```bash
curl -X POST http://localhost:8080/api/v1/timers \
  -H 'Content-Type: application/json' \
  -d '{
    "app": "demo",
    "name": "heartbeat",
    "cron_expr": "0 * * * * *",
    "callback_url": "https://example.com/jobs/heartbeat",
    "callback_method": "POST",
    "callback_body": "{\"source\":\"chronoflow\"}",
    "timezone": "Asia/Shanghai",
    "misfire_policy": "FIRE_ONCE",
    "max_catch_up": 10
  }'

curl -X POST http://localhost:8080/api/v1/timers/1/activate
```

生产环境设置 `security.api_key` 后，请求还需携带：

```bash
-H 'X-API-Key: your-secret'
```

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/v1/timers` | 创建 Timer |
| `GET` | `/api/v1/timers` | 分页查询 Timer |
| `GET` | `/api/v1/timers/:id` | Timer 详情 |
| `DELETE` | `/api/v1/timers/:id` | 逻辑删除 Timer |
| `POST` | `/api/v1/timers/:id/activate` | 激活 |
| `POST` | `/api/v1/timers/:id/deactivate` | 停用 |
| `GET` | `/api/v1/executions` | 分页查询 Execution |
| `GET` | `/api/v1/executions/:id` | Execution 详情 |
| `GET` | `/api/v1/timers/:id/executions` | Timer 最近执行 |
| `GET` | `/api/v1/monitoring/summary` | 运行摘要 |
| `GET` | `/api/v1/monitoring/history` | Prometheus 历史数据 |
| `GET` | `/health` | 进程存活 |
| `GET` | `/ready` | 当前角色依赖就绪 |
| `GET` | `/metrics` | Prometheus 指标 |

## 配置

默认配置位于 [`config/config.yaml`](config/config.yaml)。任意配置都可以通过 `CHRONOFLOW_` 环境变量覆盖，例如：

```bash
CHRONOFLOW_SERVER_PORT=8081
CHRONOFLOW_DATABASE_DSN='user:pass@tcp(mysql:3306)/chronoflow?charset=utf8mb4&parseTime=True&loc=UTC'
CHRONOFLOW_REDIS_ADDR='redis:6379'
CHRONOFLOW_SECURITY_API_KEY='replace-me'
```

关键配置：

| 配置 | 含义 |
| --- | --- |
| `scheduler.poll_interval_ms` | 到期 Timer 扫描间隔 |
| `scheduler.batch_size` | 单事务领取上限 |
| `migrations.path` | 版本化 SQL 迁移目录，默认 `migrations` |
| `outbox.*` | Outbox 领取、重试与 Stream 名称 |
| `worker.pool_size` | 单 Worker 进程 ants 并发上限 |
| `worker.lease_ttl_seconds` | 执行所有权租约 |
| `recovery.*` | 异常恢复与历史保留 |
| `security.api_key` | API 访问密钥 |
| `security.allow_private_callbacks` | 是否允许内网回调；生产建议关闭 |

## 数据库迁移

迁移由项目内嵌的 `golang-migrate` 执行，并自动维护 MySQL 的 `schema_migrations` 版本表。迁移文件按版本顺序执行：

```text
migrations/
├── 00001_init.up.sql
└── 00001_init.down.sql
```

常用命令：

```bash
make migrate-up
make migrate-version
make migrate-down STEPS=1

# 等价的 Go 命令
go run ./cmd/migrate up
go run ./cmd/migrate version
go run ./cmd/migrate down 1
```

`down` 是破坏性回滚操作，必须人工确认并指定步数。`force` 仅用于人工核验后修复 dirty 状态。已有数据库不要重复执行基线迁移；生产升级应先备份，并且只允许 CI/CD 或单一运维操作执行迁移。

所有调度时间按 UTC 写入 MySQL，Timer 的 `timezone` 仅用于 Cron 计算和展示语义。

## 交付边界

当前实现提供生产级 MVP 所需的任务生成、可靠投递、重复防护、失败重试、崩溃恢复、健康检查、安全基线和监控。它不提供 DAG、分片计算任务、人工审批、多租户资源隔离或 Kafka 级别的超大吞吐；这些属于后续产品演进范围。
