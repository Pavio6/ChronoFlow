# API 参考

基础路径：`/api/v1`。请求和响应均使用 JSON。

当 `security.api_key` 非空时，所有 `/api/*` 请求需要以下任一认证头：

```http
X-API-Key: <key>
Authorization: Bearer <key>
```

`/health`、`/ready`、`/metrics` 和静态页面不经过 API Key 中间件，应在网关层限制公开范围。

## 1. 通用响应

成功：

```json
{
  "code": 200,
  "data": {}
}
```

失败：

```json
{
  "code": 400,
  "message": "错误说明"
}
```

## 2. Timer

### 创建

`POST /api/v1/timers`

```json
{
  "app": "billing",
  "name": "hourly-summary",
  "cron_expr": "0 0 * * * *",
  "callback_url": "https://billing.example.com/internal/jobs/summary",
  "callback_method": "POST",
  "callback_body": "{\"kind\":\"hourly\"}",
  "callback_headers": {
    "X-Service-Token": "secret"
  },
  "timezone": "Asia/Shanghai",
  "misfire_policy": "FIRE_ONCE",
  "max_catch_up": 10
}
```

字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `app` | 是 | 应用标识，最长 128 |
| `name` | 是 | Timer 名称，最长 128 |
| `cron_expr` | 是 | 六字段 Cron |
| `callback_url` | 是 | HTTP/HTTPS URL，受 SSRF 策略校验 |
| `callback_method` | 是 | `GET/POST/PUT/DELETE/PATCH` |
| `callback_body` | 否 | 原样发送的请求体 |
| `callback_headers` | 否 | 回调头；不会在 Timer/Execution API 中回显 |
| `timezone` | 否 | IANA 时区，默认 `UTC` |
| `misfire_policy` | 否 | `SKIP/FIRE_ONCE/CATCH_UP`，默认 `FIRE_ONCE` |
| `max_catch_up` | 否 | 单批最多补偿次数，1–1000 |

创建成功返回 `201`，初始状态为 `INACTIVE`。

### 查询列表

`GET /api/v1/timers?page=1&page_size=20&app=billing&status=ACTIVE&keyword=summary`

支持 `page`、`page_size`、`app`、`status` 和 `keyword`。逻辑删除的 Timer 不出现在列表中。

### 查询详情

`GET /api/v1/timers/:id`

敏感的 `callback_headers` 不会返回。

### 激活

`POST /api/v1/timers/:id/activate`

只允许 `INACTIVE → ACTIVE`。成功时计算并持久化首个 `next_fire_at`。

### 停用

`POST /api/v1/timers/:id/deactivate`

只允许 `ACTIVE → INACTIVE`，并清空 `next_fire_at`。

### 删除

`DELETE /api/v1/timers/:id`

执行逻辑删除并停止未来调度。历史 Execution 保留到清理策略到期。

## 3. Execution

### 查询列表

`GET /api/v1/executions?page=1&page_size=20&timer_id=1&timer_name=summary&status=FAILED`

响应中的 `stats` 是当前筛选范围内按状态聚合的数量：

```json
{
  "code": 200,
  "data": {
    "total": 1,
    "page": 1,
    "page_size": 20,
    "items": [
      {
        "id": 42,
        "timer_id": 1,
        "timer_name": "hourly-summary",
        "scheduled_at": "2026-08-07T02:00:00Z",
        "status": "SUCCESS",
        "attempt": 1,
        "max_attempts": 3,
        "response_code": 200,
        "duration_ms": 83
      }
    ],
    "stats": {
      "SUCCESS": 1
    }
  }
}
```

Execution 状态：

- `PENDING`
- `RUNNING`
- `RETRY_WAIT`
- `SUCCESS`
- `FAILED`
- `CANCELLED`

请求快照和敏感回调头不通过 API 返回。响应体与错误信息可能包含下游数据，访问该接口应视为运维权限。

### 查询详情

`GET /api/v1/executions/:id`

### 查询 Timer 最近执行

`GET /api/v1/timers/:id/executions?limit=20`

`limit` 范围 1–100。

## 4. 监控

### 当前摘要

`GET /api/v1/monitoring/summary`

返回 Timer 与 Execution 的全量状态计数。

### 历史曲线

`GET /api/v1/monitoring/history?range_minutes=60`

支持 `15`、`60`、`360` 和 `1440` 分钟。API 代理查询配置的 Prometheus，返回成功率、回调 P95 和异常 Execution 数。

## 5. 运维端点

| 路径 | 含义 |
| --- | --- |
| `/health` | 进程已启动，不检查依赖 |
| `/ready` | 检查当前角色所需依赖 |
| `/metrics` | Prometheus 文本指标 |

每个角色都启动 HTTP 运维端口。非 API 角色访问业务路由会返回 `404`。

就绪依赖：

| 角色 | MySQL | Redis |
| --- | --- | --- |
| `api` | 是 | 否 |
| `scheduler` | 是 | 否 |
| `dispatcher` | 是 | 是 |
| `worker` | 是 | 是 |
| `all` | 是 | 是 |

## 6. 安全限制

- `security.max_request_bytes` 限制 API 请求体。
- CORS 只允许 `security.allowed_origins`。
- 默认拒绝内网、回环、链路本地和保留地址回调，防止 SSRF。
- 本地测试必须显式设置 `security.allow_private_callbacks=true`。
- 回调响应最多读取 `worker.max_response_bytes`。
- 日志不记录完整请求体、回调头或 API Key。
