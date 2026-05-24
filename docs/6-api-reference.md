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
  "callback_url": "http://localhost:9090/callback/success",
  "callback_method": "POST",
  "callback_body": "{\"action\": \"check_orders\"}",
  "callback_headers": {"Authorization": "Bearer token123"},
  "timeout": 30
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
    "callback_url": "http://localhost:9090/callback/success",
    "callback_method": "POST",
    "callback_body": "{\"action\": \"check_orders\"}",
    "callback_headers": "{\"Authorization\":\"Bearer token123\"}",
    "status": "INACTIVE",
    "timeout": 30,
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
    "items": [...]
  }
}
```

### 获取定时器详情

```
GET /api/v1/timers/:id
```

### 更新定时器

```
PUT /api/v1/timers/:id
```

**请求体（部分更新）：**

```json
{
  "name": "新名称",
  "timeout": 60
}
```

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

状态转换：ACTIVE → INACTIVE。停用后不再创建新的执行记录。

## 执行记录

### 查询执行记录列表

```
GET /api/v1/records?page=1&page_size=10&timer_id=1&status=SUCCESS
```

**查询参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| page | int | 页码 |
| page_size | int | 每页数量 |
| timer_id | int | 定时器 ID 过滤 |
| status | string | 状态过滤 |

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
