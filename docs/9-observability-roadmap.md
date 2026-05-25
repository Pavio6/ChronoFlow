# 可观测性 MVP 接入计划

## 1. MVP 目标

当前系统已经通过 Go Prometheus client 暴露 `GET /metrics`，管理端监控页则读取
`GET /api/v1/monitoring/summary` 并在浏览器内临时绘制趋势。该方式能展示当前状态，
但刷新页面或后端重启后没有历史数据，也不能可靠发现任务滞留。

MVP 只解决三个问题：

1. 保存可查询的指标历史。
2. 能看到调度系统最关键的健康状态与失败趋势。
3. 对任务滞留和服务不可用提供最基本的告警。

MVP 架构保持简单：

```text
ChronoFlow /metrics --> Prometheus --> Grafana
```

暂不接入：

- Loki / Promtail 日志平台。
- OpenTelemetry / Tempo 链路追踪。
- Alertmanager 独立通知路由。
- 前端调用 Prometheus HTTP API 或自行保存趋势数据。

MVP 告警先使用 Grafana Alerting；需要邮件、Webhook 分组路由或生产值班体系时，再
单独增加 Alertmanager。

## 2. 页面与组件边界

| 组件 | MVP 职责 |
| --- | --- |
| ChronoFlow | 暴露低基数业务指标和即时业务摘要 |
| Prometheus | 每 10 秒抓取 `/metrics`，保存历史时序 |
| Grafana | dashboard、简单告警、历史趋势查询 |
| 现有管理端监控页 | 保留当前摘要，并增加“查看 Grafana”入口 |

管理端不直接读取 `/metrics`。`/metrics` 是机器采集接口，不是业务页面数据接口。

## 3. MVP 只保留的指标

现有 [reporter.go](../internal/pkg/metrics/reporter.go) 将 `timer_id` 作为 label，
容易产生高基数时序；`chronoflow_timer_queue_size` 当前固定为 `0`，没有监控价值。
MVP 应替换为以下六项指标：

| 指标 | 类型 | Labels | 用途 |
| --- | --- | --- | --- |
| `chronoflow_callback_requests_total` | Counter | `result` | 回调成功/失败计数 |
| `chronoflow_callback_duration_seconds` | Histogram | `result` | 回调延迟与 P95 |
| `chronoflow_records` | Gauge | `status` | PENDING/RUNNING/SUCCESS/FAILED 当前量 |
| `chronoflow_pending_overdue_records` | Gauge | 无 | 已超期但仍未执行的任务数 |
| `chronoflow_running_stale_records` | Gauge | 无 | 执行时间异常过长的任务数 |
| `chronoflow_redis_queue_items` | Gauge | 无 | Redis 当前队列任务总量 |

MVP 约束：

- 不使用 `timer_id`、`record_id`、URL 作为指标 label。
- 暂不使用 `app` label，避免应用名由用户输入导致时序膨胀。
- callback Histogram 单位使用秒。
- 单个任务排查仍使用执行记录列表和现有 JSON 日志。

## 4. 实施步骤

### Step 1：改造后端指标

修改 `internal/pkg/metrics/reporter.go`：

1. 将执行成功/失败统一记录到 `chronoflow_callback_requests_total{result=...}`。
2. 将执行耗时改为 `chronoflow_callback_duration_seconds{result=...}`。
3. 删除或停止暴露带 `timer_id` label 的新指标写入。
4. 删除固定返回 `0` 的队列 Gauge，改为可由采集任务写入的 Gauge。

修改 `internal/service/executor.go`：

1. 回调完成后上报 `result="success"` 或 `result="failed"`。
2. 以秒记录 callback 执行耗时。

### Step 2：增加一个轻量状态采集循环

新增 `internal/service/monitor_collector.go`，启动时由 `cmd/server/main.go` 启动。

配置增加：

```yaml
monitoring:
  collect_interval_seconds: 10
  pending_overdue_seconds: 120
  running_stale_seconds: 60
```

每 10 秒执行：

```text
1. 按状态查询 timer_records 数量，更新 chronoflow_records{status=...}。
2. 统计 trigger_time 早于 now - 120s 且 status=PENDING 的记录数。
3. 统计 started_at 早于 now - 60s 且 status=RUNNING 的记录数。
4. 查询 Redis 队列成员总量，更新 chronoflow_redis_queue_items。
```

需要在 `TimerRecordRepository` 增加两个查询方法：

```go
CountPendingOverdue(before time.Time) (int64, error)
CountRunningStale(before time.Time) (int64, error)
```

注意：collector 只负责暴露异常，不负责恢复任务。任务补偿与 `RUNNING` 回收仍是
调度可靠性修复项，应另行实现。

### Step 3：增加 Prometheus 服务

新增目录：

```text
observability/
└── prometheus/
    └── prometheus.yml
```

配置文件内容：

```yaml
global:
  scrape_interval: 10s

scrape_configs:
  - job_name: chronoflow
    metrics_path: /metrics
    static_configs:
      - targets: ["chronoflow:8080"]
```

在 `docker-compose.yml` 增加：

```yaml
prometheus:
  image: prom/prometheus
  container_name: chronoflow-prometheus
  ports:
    - "9090:9090"
  volumes:
    - ./observability/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    - prometheus-data:/prometheus
  networks:
    - chronoflow-network
  restart: unless-stopped
```

并增加持久卷：

```yaml
volumes:
  prometheus-data:
```

注意：容器方式启动 ChronoFlow 时，还需要把应用访问依赖的地址改为容器网络地址：

```yaml
database:
  dsn: "root:123456@tcp(mysql:3306)/chronoflow?charset=utf8mb4&parseTime=True&loc=Local"
redis:
  addr: "redis:6379"
```

建议增加 `config/config.docker.yaml` 供 Compose 使用，保留当前本地开发配置。

### Step 4：增加 Grafana 服务与一个 dashboard

新增目录：

```text
observability/
└── grafana/
    ├── dashboards/
    │   └── chronoflow.json
    └── provisioning/
        ├── dashboards/
        │   └── dashboards.yml
        └── datasources/
            └── datasources.yml
```

在 `docker-compose.yml` 增加 Grafana：

```yaml
grafana:
  image: grafana/grafana
  container_name: chronoflow-grafana
  ports:
    - "3001:3000"
  volumes:
    - ./observability/grafana/provisioning:/etc/grafana/provisioning:ro
    - ./observability/grafana/dashboards:/var/lib/grafana/dashboards:ro
    - grafana-data:/var/lib/grafana
  depends_on:
    - prometheus
  networks:
    - chronoflow-network
  restart: unless-stopped
```

dashboard 只做六个面板：

| 面板 | PromQL |
| --- | --- |
| 服务是否可用 | `up{job="chronoflow"}` |
| 最近 5 分钟回调速率 | `sum(rate(chronoflow_callback_requests_total[5m]))` |
| 最近 5 分钟成功率 | `sum(rate(chronoflow_callback_requests_total{result="success"}[5m])) / clamp_min(sum(rate(chronoflow_callback_requests_total[5m])), 1)` |
| Callback P95 | `histogram_quantile(0.95, sum by (le) (rate(chronoflow_callback_duration_seconds_bucket[5m])))` |
| 超期与卡住任务 | `chronoflow_pending_overdue_records`、`chronoflow_running_stale_records` |
| Redis 队列任务量 | `chronoflow_redis_queue_items` |

### Step 5：配置三条基础告警

MVP 使用 Grafana Alerting 创建三条规则：

| 告警 | 表达式 | 持续时间 | 目的 |
| --- | --- | ---: | --- |
| 服务不可采集 | `up{job="chronoflow"} == 0` | `1m` | 服务或采集异常 |
| 出现超期 PENDING | `chronoflow_pending_overdue_records > 0` | `1m` | 发现漏执行风险 |
| 出现 stale RUNNING | `chronoflow_running_stale_records > 0` | `1m` | 发现卡死任务 |

初期允许仅在 Grafana UI 中看到告警状态。需要消息通知时，再给 Grafana 配置一个
Webhook contact point；暂不为 MVP 部署 Alertmanager。

### Step 6：调整现有监控页文案

修改 `web/src/pages/Monitoring.tsx`：

1. 当前卡片继续读取 `/api/v1/monitoring/summary`。
2. 顶部增加 `打开 Grafana` 按钮，链接配置为 `http://localhost:3001`。
3. 将“长期趋势请接入 Prometheus”改为“长期趋势与告警请查看 Grafana”。
4. 可暂时保留页面内即时曲线，并标明“仅当前页面会话数据”；无需在 MVP 中改造后端查询 Prometheus。

## 5. 交付文件清单

MVP 预计修改或新增：

```text
internal/config/config.go
internal/pkg/metrics/reporter.go
internal/repository/timer_record_repo.go
internal/service/executor.go
internal/service/monitor_collector.go
cmd/server/main.go
config/config.yaml
config/config.docker.yaml
docker-compose.yml
observability/prometheus/prometheus.yml
observability/grafana/provisioning/datasources/datasources.yml
observability/grafana/provisioning/dashboards/dashboards.yml
observability/grafana/dashboards/chronoflow.json
web/src/pages/Monitoring.tsx
README.md
```

## 6. 验收标准

### 本地运行

```bash
docker compose up -d mysql redis chronoflow prometheus grafana
```

访问入口：

| 服务 | 地址 |
| --- | --- |
| ChronoFlow | `http://localhost:8080` |
| Exporter | `http://localhost:8080/metrics` |
| Prometheus | `http://localhost:9090` |
| Grafana | `http://localhost:3001` |

### 必须通过的检查

1. Prometheus `Targets` 页面显示 `chronoflow` 为 `UP`。
2. 触发一次成功和一次失败回调后，Grafana 能显示速率、成功率与延迟数据。
3. 创建超期 `PENDING` 记录后，dashboard 显示非零值并触发告警。
4. 创建超时 `RUNNING` 记录后，dashboard 显示非零值并触发告警。
5. 刷新 ChronoFlow 管理页面或重启后端后，Grafana 历史曲线仍保留。

## 7. MVP 之后再考虑的内容

以下项目不进入本轮开发：

| 能力 | 何时需要 |
| --- | --- |
| Alertmanager | 告警需要复杂分组、静默、多个通知渠道时 |
| Loki | 单靠执行记录和本地日志不足以排查线上问题时 |
| OpenTelemetry + Tempo | 需要跨服务 callback 链路定位时 |
| 管理端内嵌 Prometheus 历史趋势 | 明确要求业务页面替代 Grafana 时 |
| Recording rules | dashboard 查询量或 PromQL 复杂度明显上升时 |

## 8. 参考资料

- Prometheus instrumentation practices: <https://prometheus.io/docs/practices/instrumentation/>
- Prometheus metric and label naming: <https://prometheus.io/docs/practices/naming/>
- Prometheus configuration: <https://prometheus.io/docs/prometheus/latest/configuration/configuration/>
- Grafana Prometheus datasource: <https://grafana.com/docs/grafana/latest/datasources/prometheus/>
- Grafana provisioning: <https://grafana.com/docs/grafana/latest/administration/provisioning/>
