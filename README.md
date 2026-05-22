# ChronoFlow - Go 分布式定时任务调度系统

基于 xTimer 架构实现的分布式定时任务调度系统，采用**时间分片 + 分桶并发 + 三级存储**设计，支持 Cron 表达式定时、HTTP 回调、失败重试、幂等去重、分布式锁等能力。

## 核心特性

| 特性 | 说明 |
|------|------|
| **时间分片** | 按分钟级时间范围切分 Redis ZSet，减少单次查询量 |
| **分桶并发** | 按 `timer_id % bucket_num` 分桶，每桶独立 goroutine 并发处理 |
| **三级存储** | MySQL → Redis ZSet → 节点内存缓存，逐级加速 |
| **三层幂等** | Bloom Filter → Redis SETNX → MySQL 查重，防止重复执行 |
| **分布式锁** | Scheduler 对每个分片 SETNX 抢锁，多实例互斥 |
| **指数退避** | 失败重试 10s → 30s → 60s，避免无效重试风暴 |
| **协程池** | ants 协程池控制并发数，防止资源耗尽 |
| **Prometheus** | 内置指标采集（执行次数/耗时/成功率/重试/队列深度） |

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.26.2+ |
| Web 框架 | Gin |
| ORM | GORM |
| 数据库 | MySQL 8.0 |
| 缓存与队列 | Redis 7（ZSet + Lua 脚本） |
| Cron 解析 | robfig/cron/v3 |
| 日志 | zap |
| 配置 | viper |
| 协程池 | panjf2000/ants/v2 |
| 指标 | prometheus/client_golang |
| 前端 | React + TypeScript + Ant Design |
| 容器化 | Docker / Docker Compose |

## 系统架构

```
┌─────────────────────────────────────────────────────────┐
│                    ChronoFlow                            │
│                                                         │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐          │
│  │ Migrator │    │Scheduler │    │  Trigger │          │
│  │(批量打点)│    │(抢锁+分发)│    │(轮询ZSet)│          │
│  └────┬─────┘    └────┬─────┘    └────┬─────┘          │
│       │               │               │                 │
│       │          [goroutine pool]      │                 │
│       │               │               │                 │
│       ▼               ▼               ▼                 │
│  ┌──────────────────────────────────────────┐           │
│  │              Executor                     │           │
│  │  (幂等去重 + HTTP回调 + 更新记录)          │           │
│  └──────────────────────────────────────────┘           │
│                                                         │
│  ┌──────────────────────────────────────────┐           │
│  │           内存缓存层 (Memory Cache)       │           │
│  └──────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────┘
         │                │                │
    ┌────▼────┐     ┌────▼────┐     ┌────▼────┐
    │  MySQL  │     │  Redis  │     │  Memory │
    │(持久化) │     │(ZSet队列)│     │(本地缓存)│
    └─────────┘     └─────────┘     └─────────┘
```

### 核心流程

```
定时器创建 → 定义存 MySQL（status=INACTIVE）
     │
定时器激活 → Migrator 批量预创建未来 step1 的定时任务
     │         → 定时任务存 MySQL + Redis ZSet（按分片分桶）
     │
每 1s → Scheduler 轮询
     │   → 计算当前 time_range
     │   → 遍历每个桶，抢分布式锁
     │   → 抢到锁 → 从协程池提交 Trigger
     │
Trigger → 在时间片内轮询 Redis ZSet {time_range}:{bucket}
     │    → ZRANGEBYSCORE 取到期任务
     │    → 从协程池提交 Executor
     │    → 完成后更新锁 TTL
     │
Executor → bloom filter 查重
     │    → Redis 幂等键检查（bloom 命中时）
     │    → MySQL 查重（Redis 也命中时）
     │    → 查定时器定义（先内存，miss 再 MySQL）
     │    → 执行 HTTP 回调
     │    → bloom filter 打点 + Redis 幂等键设置
     │    → 更新 MySQL 记录状态
```

## 项目结构

```
ChronoFlow/
├── cmd/server/main.go                    # 程序入口
├── config/config.yaml                    # 配置文件
├── internal/
│   ├── config/config.go                  # Viper 配置管理
│   ├── model/
│   │   ├── timer_definition.go           # 定时器定义模型
│   │   ├── timer_record.go               # 执行记录模型
│   │   └── state_machine.go              # 状态机定义
│   ├── repository/
│   │   ├── database.go                   # GORM MySQL 初始化
│   │   ├── timer_definition_repo.go      # 定时器定义 CRUD
│   │   └── timer_record_repo.go          # 执行记录 CRUD
│   ├── service/
│   │   ├── migrator.go                   # 一级迁移（MySQL → Redis）
│   │   ├── scheduler.go                  # 调度器（抢锁 + 分发）
│   │   ├── trigger.go                    # 触发器（轮询 ZSet）
│   │   ├── executor.go                   # 执行器（幂等 + 回调）
│   │   └── timer_service.go              # 定时器 CRUD 业务逻辑
│   ├── handler/
│   │   └── timer_handler.go              # HTTP API 处理器
│   ├── middleware/
│   │   ├── cors.go                       # CORS 中间件
│   │   ├── logger.go                     # 请求日志中间件
│   │   └── recovery.go                   # Panic 恢复中间件
│   └── pkg/
│       ├── bloom/filter.go               # Redis 布隆过滤器
│       ├── cron/parser.go                # Cron 表达式解析器
│       ├── memory/cache.go               # 内存定时器缓存
│       ├── metrics/reporter.go           # Prometheus 指标上报
│       ├── pool/pool.go                  # ants 协程池
│       ├── redis/queue.go                # Redis ZSet 队列 + Lua + 锁
│       └── retry/strategy.go             # 指数退避重试策略
├── pkg/logger/logger.go                  # zap 日志封装
├── web/                                  # React 前端
├── tests/callback-server/main.go         # 测试回调服务
├── migrations/001_init.sql               # 数据库初始化脚本
├── docker-compose.yml                    # Docker 编排
├── Dockerfile                            # 多阶段构建
├── Makefile                              # 开发命令
└── docs/                                 # 项目文档
```

## 快速开始

### 环境要求

- Go 1.26.2+
- Node.js 18+（前端开发）
- MySQL 8.0+
- Redis 7.0+

### 1. 启动基础设施

```bash
# 启动 MySQL + Redis
make docker-up
```

### 2. 初始化数据库

```bash
# 方式一：手动执行 SQL
mysql -u root -p123456 < migrations/001_init.sql

# 方式二：程序启动时自动迁移（GORM AutoMigrate）
# 无需手动执行，程序会自动创建表结构
```

### 3. 配置

编辑 `config/config.yaml`，确认数据库和 Redis 连接信息：

```yaml
database:
  dsn: "root:123456@tcp(127.0.0.1:3306)/chronoflow?charset=utf8mb4&parseTime=True&loc=Local"

redis:
  addr: "127.0.0.1:6379"
```

### 4. 启动后端

```bash
make dev-backend
# 或
go run cmd/server/main.go
```

服务启动后：
- API: http://localhost:8080/api/v1/timers
- Prometheus: http://localhost:8080/metrics
- 健康检查: http://localhost:8080/health

### 5. 启动前端

```bash
make dev-frontend
# 或
cd web && npm install && npm run dev
```

前端访问: http://localhost:3000

## 测试步骤

### 步骤一：启动测试回调服务

```bash
make test-callback
# 或
go run tests/callback-server/main.go
```

测试回调服务监听在 `http://localhost:9090`，提供以下接口：

| 接口 | 说明 |
|------|------|
| `POST /callback/success` | 立即返回成功 |
| `POST /callback/slow` | 延迟 10 秒后返回（测试超时） |
| `POST /callback/error` | 返回 500 错误（测试重试） |
| `GET /stats` | 查看调用统计 |

### 步骤二：创建定时器

```bash
# 创建一个每 30 秒触发的定时器
curl -X POST http://localhost:8080/api/v1/timers \
  -H "Content-Type: application/json" \
  -d '{
    "app": "test",
    "name": "测试定时器-30秒",
    "cron_expr": "*/30 * * * * *",
    "callback_url": "http://localhost:9090/callback/success",
    "callback_method": "POST",
    "callback_body": "{\"msg\": \"hello\"}",
    "timeout": 10,
    "max_retries": 3
  }'
```

预期响应：

```json
{
  "code": 201,
  "message": "创建成功",
  "data": {
    "id": 1,
    "app": "test",
    "name": "测试定时器-30秒",
    "status": "INACTIVE",
    ...
  }
}
```

### 步骤三：激活定时器

```bash
# 激活定时器（ID=1）
curl -X POST http://localhost:8080/api/v1/timers/1/activate
```

激活后，Migrator 会自动预创建执行记录并推入 Redis 队列。

### 步骤四：观察执行结果

```bash
# 查看定时器列表
curl http://localhost:8080/api/v1/timers

# 查看执行记录
curl http://localhost:8080/api/v1/records

# 查看指定定时器的执行记录
curl http://localhost:8080/api/v1/timers/1/records

# 查看测试回调服务统计
curl http://localhost:9090/stats
```

### 步骤五：测试重试机制

```bash
# 创建一个指向错误接口的定时器
curl -X POST http://localhost:8080/api/v1/timers \
  -H "Content-Type: application/json" \
  -d '{
    "app": "test",
    "name": "测试重试",
    "cron_expr": "*/30 * * * * *",
    "callback_url": "http://localhost:9090/callback/error",
    "callback_method": "POST",
    "timeout": 10,
    "max_retries": 3
  }'

# 激活后观察执行记录中的 retry_count 和 status 变化
curl http://localhost:8080/api/v1/records | jq
```

### 步骤六：测试超时

```bash
# 创建一个指向慢接口的定时器（超时设为 5 秒，但接口需要 10 秒）
curl -X POST http://localhost:8080/api/v1/timers \
  -H "Content-Type: application/json" \
  -d '{
    "app": "test",
    "name": "测试超时",
    "cron_expr": "*/30 * * * * *",
    "callback_url": "http://localhost:9090/callback/slow",
    "callback_method": "POST",
    "timeout": 5,
    "max_retries": 2
  }'
```

### 步骤七：查看 Prometheus 指标

```bash
# 访问 Prometheus 指标端点
curl http://localhost:8080/metrics

# 关键指标：
# chronoflow_timer_exec_total - 执行总次数
# chronoflow_timer_exec_duration_ms - 执行耗时分布
# chronoflow_timer_exec_success_total - 成功次数
# chronoflow_timer_exec_failed_total - 失败次数
# chronoflow_timer_exec_retry_total - 重试次数
```

### 步骤八：多实例部署测试

```bash
# 启动第二个实例（不同端口）
PORT=8081 go run cmd/server/main.go

# 两个实例会通过分布式锁自动协调，不会重复执行
```

## 配置说明

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `scheduler.step1_duration` | 3600 | 一级迁移时间步（秒），Migrator 预创建范围 |
| `scheduler.step2_duration` | 300 | 二级迁移时间步（秒），内存缓存刷新间隔 |
| `scheduler.bucket_num` | 3 | 分桶数量 |
| `scheduler.scan_interval` | 1 | Scheduler 轮询间隔（秒） |
| `executor.timeout` | 30 | HTTP 回调超时（秒） |
| `executor.max_retries` | 3 | 最大重试次数 |
| `executor.worker_pool_size` | 100 | 协程池大小 |
| `retry.strategy` | exponential | 重试策略（exponential/fixed） |
| `retry.initial_interval` | 10 | 初始重试间隔（秒） |
| `retry.max_interval` | 60 | 最大重试间隔（秒） |
| `retry.multiplier` | 3.0 | 退避倍数 |

## Redis Key 结构

| 用途 | Key 格式 | 类型 |
|------|----------|------|
| 任务队列 | `chronoflow:timer:{YYYY-MM-DD-HH:mm}:{bucket}` | ZSet |
| Scheduler 锁 | `chronoflow:scheduler_lock:{time_range}:{bucket}` | String (SETNX) |
| 幂等键 | `chronoflow:idempotent:{timer_id}:{trigger_time_ms}` | String (SETNX) |
| 布隆过滤器 | `chronoflow:bloom:{date}` | String (bitmap) |

## 定时器状态流转

```
INACTIVE ──激活──→ ACTIVE ──停用──→ INACTIVE
    │                 │
    └────删除────────→ DELETED（终态）
```

## 文档

- [系统架构详解](docs/1-architecture.md)
- [xTimer 原理与实现](docs/2-xtimer-theory.md)
- [调度器模块详解](docs/3-scheduler.md)
- [分布式锁与幂等](docs/4-distributed-lock.md)
- [重试与恢复机制](docs/5-retry-and-recovery.md)
- [API 接口文档](docs/6-api-reference.md)
- [xTimer 架构分析](docs/7.xtimer-analysis.md)

## 许可证

MIT License
