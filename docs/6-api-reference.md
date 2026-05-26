# API 接口文档

## 基础信息

- Base URL: `http://localhost:8080/api/v1`
- Content-Type: `application/json`

## 定时器管理

### 创建定时器

```
POST /api/v1/timers
```

**请求体：**

```json
{
  "app": "order-service",
  "name": "每分钟检查订单",
  "cron_expr": "0 * * * * *",
  "callback_url": "http://localhost:9091/callback/success",
  "callback_method": "POST",
  "callback_body": "{\"action\": \"check_orders\"}",
  "callback_headers": {"Authorization": "Bearer token123"}
}
```

**响应：**

```json
{
  "code": 201,
  "message": "创建成功",
  "data": {
    "id": 1,
    "app": "order-service",
    "name": "每分钟检查订单",
    "cron_expr": "0 * * * * *",
    "callback_url": "http://localhost:9091/callback/success",
    "callback_method": "POST",
    "callback_body": "{\"action\": \"check_orders\"}",
    "callback_headers": "{\"Authorization\":\"Bearer token123\"}",
    "status": "INACTIVE",
    "created_at": "2026-05-22T10:00:00Z",
    "updated_at": "2026-05-22T10:00:00Z"
  }
}
```

### 查询定时器列表

```
GET /api/v1/timers?page=1&page_size=10&app=order-service&status=ACTIVE&keyword=订单
```

**查询参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| page | int | 页码（默认 1） |
| page_size | int | 每页数量（默认 10，最大 100） |
| app | string | 应用名过滤 |
| status | string | 状态过滤（ACTIVE/INACTIVE） |
| keyword | string | 关键字搜索（匹配名称和 URL） |

**响应：**

```json
{
  "code": 200,
  "data": {
    "total": 50,
    "page": 1,
    "page_size": 10,
    "items": [...],
    "stats": {
      "total": 50,
      "active": 32,
      "inactive": 18
    }
  }
}
```

`stats` 按当前列表筛选条件聚合，不受分页影响。

### 获取定时器详情

```
GET /api/v1/timers/:id
```

### 定义不可修改

定时器定义创建后不可修改，包括 Cron 和回调参数。系统不提供 `PUT /api/v1/timers/:id` 接口；需要修改定义时，应删除旧定时器并创建新定时器。

### 删除定时器

```
DELETE /api/v1/timers/:id
```

逻辑删除，将状态设置为 DELETED。

### 激活定时器

```
POST /api/v1/timers/:id/activate
```

状态转换：INACTIVE → ACTIVE。激活后 Migrator 会自动预创建执行记录。

### 停用定时器

```
POST /api/v1/timers/:id/deactivate
```

状态转换：ACTIVE → INACTIVE。停用后不再创建新的执行记录，也不会删除已经写入 Redis ZSet 的点；Executor 使用节点本地定义缓存中的状态执行判断，因此持有旧 `ACTIVE` 缓存的节点可能在一个 `step2_duration` 周期内继续回调，缓存过期后会跳过非 `ACTIVE` 定时器。

## 执行记录

### 查询执行记录列表

```
GET /api/v1/records?page=1&page_size=10&timer_name=订单&status=SUCCESS
```

**查询参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| page | int | 页码 |
| page_size | int | 每页数量 |
| timer_name | string | 定时器名称模糊过滤 |
| timer_id | int | 定时器 ID 过滤（兼容保留） |
| status | string | 状态过滤 |

响应条目包含 `timer_name`；`data.stats` 按当前列表筛选条件返回 `total`、`pending`、`running`、`success`、`failed` 聚合值，不受分页影响。列表默认按记录 ID 倒序返回，优先展示最新写入记录。

### 获取指定定时器的执行记录

```
GET /api/v1/timers/:id/records?limit=20
```

### 获取执行记录详情

```
GET /api/v1/records/:id
```

## 系统端点

### 健康检查

```
GET /health
```

**响应：**

```json
{
  "status": "ok",
  "time": "2026-05-22T10:00:00+08:00"
}
```

### Prometheus 指标

```
GET /metrics
```

返回 Prometheus 格式的指标数据。

### 监控历史趋势

```
GET /api/v1/monitoring/history?range_minutes=60
```

后端代理查询 Prometheus 并返回管理端图表使用的历史序列。`range_minutes` 支持
`15`、`60`、`360`、`1440`，其他值按 `60` 分钟处理。返回的序列包括服务可用性、
成功率、P95 延迟与异常任务（超期待执行和卡住执行的合计数量）。

## 错误响应

所有错误响应格式：

```json
{
  "code": 400,
  "message": "错误描述"
}
```

| HTTP 状态码 | 说明 |
|------------|------|
| 200 | 成功 |
| 201 | 创建成功 |
| 400 | 请求参数错误 |
| 404 | 资源不存在 |
| 500 | 服务器内部错误 |
