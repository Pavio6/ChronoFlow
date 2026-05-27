# ChronoFlow

ChronoFlow 是一个使用 Go 实现的定时任务调度服务。用户定义 Cron 规则和 HTTP 回调，系统在触发时间生成并执行回调任务，同时通过管理端查看任务、执行记录和运行状态。

## 重要功能

- **预生成式时间调度**：任务激活时立即创建近期执行记录，`Migrator` 持续补充后续窗口；MySQL 保存可靠执行依据，Redis ZSet 按分钟承载到期任务索引，避免调度阶段反复扫描全部任务定义。
- **动态分桶与资源隔离**：通过 Redis Lua 原子登记分钟任务量，并在配置上限内只增不减地扩展 ZSet 分桶；调度扫描与 HTTP 回调分别运行在独立的 `ants` 协程池中，避免慢回调直接挤占分片扫描资源。
- **多实例协调与任务补偿**：以分钟分片和桶为调度单元，通过带所有权校验的租约机制协调多节点处理；同时合并 MySQL 待执行记录并补扫上一分钟，覆盖 Redis 投递不完整和时间边界异常。
- **幂等执行与路径优化**：通过执行记录唯一约束和原子执行权竞争避免重复回调；使用本地定义缓存与 Bloom Filter 预筛已成功任务，降低重复派发场景下的额外处理成本。
- **监控与执行追踪**：记录回调请求、响应、错误与耗时；管理端嵌入 Grafana dashboard，展示执行状态趋势、真实成功回调速率、P95 回调耗时和 Redis 待触发队列，时序数据由 Prometheus 提供。

## 核心设计

### 调度执行引擎架构

ChronoFlow 的调度执行引擎由一个独立的 `Migrator`、`Scheduler`、`Trigger`、`Executor` 三个执行模块，以及两个独立的 `ants` 协程池组成：

```text
Migrator

Scheduler --[schedulerPool]--> Trigger --[triggerPool]--> Executor --[timeout control]--> HTTP Callback
```

| 组件 | 功能 |
| --- | --- |
| `Migrator` | 独立扫描激活任务，预生成未来窗口内的 `PENDING` 执行记录，并投递到 Redis 分片队列。 |
| `Scheduler` | 扫描当前分钟和上一分钟分片，竞争 Redis 分片锁，并在取得锁后运行 `Trigger`。 |
| `schedulerPool` | 承载分片扫描、锁竞争与 `Trigger.Run`。 |
| `Trigger` | 在时间片内读取 Redis 到期任务，合并 MySQL 待执行记录补偿结果，并将任务提交执行。 |
| `triggerPool` | 承载 `Executor.Execute` 与 HTTP 回调期间的并发占用。 |
| `Executor` | 原子抢占执行权，执行带超时控制的 HTTP 回调，随后更新执行结果和指标。 |

### 调度模型

1. 新建任务默认处于 `INACTIVE` 状态；激活后，服务根据 Cron 表达式立即创建未来时间窗口内的 `PENDING` 执行记录并写入 Redis。
2. Migrator 周期性扫描 `ACTIVE` 任务，为后续窗口创建执行记录并投递到 Redis。
3. Redis 使用分钟级 ZSet 队列，按投递规模在配置上限内动态增加桶数；Scheduler 扫描当前分钟和上一分钟的各个桶。
4. 多个服务实例通过 Redis `SET NX` 分片锁竞争调度权；Trigger 同时合并 MySQL 中的 `PENDING` 记录，补偿 Redis 投递不完整的情况。
5. Scheduler 的分片扫描池与 Trigger 的回调执行池彼此隔离，慢回调不占用调度扫描 worker。
6. Executor 只有在原子抢占执行记录成功后才发送 HTTP 请求；回调请求具备超时控制，并将结果更新为 `SUCCESS` 或 `FAILED`。

### 状态模型

任务状态：

```text
INACTIVE --激活--> ACTIVE --停用--> INACTIVE
    |                 |
    +------删除-------+----> DELETED
```

执行记录状态：

```text
PENDING --> RUNNING --> SUCCESS | FAILED
```

任务定义创建后没有编辑接口；需要调整 Cron 或回调参数时，创建新的任务定义。

## 界面展示

### 任务管理

[![任务管理界面](./images/任务管理.png)](./images/任务管理.png)

### 执行记录

[![执行记录界面](./images/执行记录.png)](./images/执行记录.png)

### 监控面板

[![监控面板界面](./images/监控面板.png)](./images/监控面板.png)

## 技术栈

| 模块 | 实现 |
| --- | --- |
| 后端 | Go、Gin、GORM、Viper、Zap |
| 数据存储 | MySQL 8.0、Redis 7 |
| 调度组件 | robfig/cron、ants 协程池 |
| 指标与历史 | prometheus/client_golang、Prometheus、Grafana |
| 管理端 | React、TypeScript、Vite、Ant Design |
| 本地运行环境 | Docker Compose |

## 快速开始

### 环境要求

- Go 1.26.2+
- Node.js 20.19+
- Docker 与 Docker Compose

### 1. 启动基础服务

```bash
make dev-start
```

该命令启动 MySQL、Redis、Prometheus 和 Grafana。


### 2. 启动后端

```bash
make dev-app
```

服务地址：

| 服务 | 地址 |
| --- | --- |
| API | `http://localhost:8080/api/v1` |
| Prometheus 指标 | `http://localhost:8080/metrics` |
| 健康检查 | `http://localhost:8080/health` |
| Prometheus | `http://localhost:9090` |
| Grafana Dashboard | `http://localhost:8080/grafana/` |

### 3. 启动管理端

```bash
cd web
npm install
npm run dev
```

管理端访问地址为 `http://localhost:3000`，开发服务器会将 `/api` 与 `/grafana` 请求代理到后端服务。监控页直接嵌入 Grafana dashboard。

## Grafana 监控

管理端 `/monitoring` 页面通过站内 `/grafana` 路径嵌入 Grafana dashboard。图表渲染由 Grafana 完成，Prometheus 采集的数据来自调度器真实执行的回调请求。

本地开发中，后端将 `/grafana` 代理至 `http://localhost:3001`；Compose 后端配置使用容器内地址 `http://grafana:3000`。为支持管理端 iframe，当前 Compose Grafana 配置启用了匿名只读访问和嵌入能力，生产部署不应直接沿用该认证配置。

当前 dashboard 包含执行状态趋势、真实成功回调请求速率、真实回调 P95 耗时和 Redis 待触发队列；执行状态中的失败序列仅在存在失败记录时显示。

## API 概览

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/v1/timers` | 创建任务 |
| `GET` | `/api/v1/timers` | 分页查询任务 |
| `GET` | `/api/v1/timers/:id` | 查询任务详情 |
| `DELETE` | `/api/v1/timers/:id` | 逻辑删除任务 |
| `POST` | `/api/v1/timers/:id/activate` | 激活任务 |
| `POST` | `/api/v1/timers/:id/deactivate` | 停用任务 |
| `GET` | `/api/v1/timers/:id/records` | 查询指定任务的执行记录 |
| `GET` | `/api/v1/records` | 分页查询执行记录 |
| `GET` | `/api/v1/records/:id` | 查询执行记录详情 |
| `GET` | `/api/v1/monitoring/summary` | 查询当前监控摘要 |
| `GET` | `/api/v1/monitoring/history` | 查询 Prometheus 历史趋势 |

## 关键配置

配置文件为 `config/config.yaml`：

| 配置项 | 默认值 | 作用 |
| --- | --- | --- |
| `scheduler.migrate_step_minutes` | `60` | 后续执行记录生成周期 |
| `scheduler.base_bucket_num` | `1` | 每分钟初始桶数 |
| `scheduler.bucket_num` | `3` | 每分钟动态扩桶上限 |
| `scheduler.tasks_per_bucket` | `100` | 每桶目标投递数量 |
| `scheduler.scan_interval` | `1` | Scheduler 扫描间隔（秒） |
| `scheduler.worker_pool_size` | `16` | 分片扫描协程池大小 |
| `trigger.worker_pool_size` | `100` | HTTP 回调执行协程池大小 |
| `monitoring.collect_interval_seconds` | `10` | 当前状态指标采集间隔（秒） |
| `monitoring.pending_overdue_seconds` | `120` | 等待执行异常阈值（秒） |
| `monitoring.running_stale_seconds` | `60` | 执行中异常阈值（秒） |
| `monitoring.prometheus_url` | `http://localhost:9090` | 历史趋势查询目标 |
| `monitoring.grafana_url` | `http://localhost:3001` | `/grafana` 同源代理目标 |

## 构建与验证

```bash
go test ./...
cd web && npm run lint && npm run build
```

构建后的前端资源位于 `web/dist`，后端会提供该目录的静态页面服务。
