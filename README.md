# ChronoFlow - Go 分布式定时任务调度系统

采用**时间分片 + 分桶并发 + 三级存储**设计的分布式定时任务调度系统，支持 Cron 表达式定时、HTTP 回调、幂等去重、分布式锁等能力。

## 核心特性

| 特性 | 说明 |
|------|------|
| **时间分片** | 按分钟级时间范围切分 Redis ZSet，减少单次查询量 |
| **分桶并发** | 按 `timer_id % bucket_num` 分桶，每桶独立 goroutine 并发处理 |
| **三级存储** | MySQL → Redis ZSet → 节点内存缓存，逐级加速 |
| **执行幂等** | Bloom 快速过滤 + 唯一业务键 + MySQL 原子状态抢占 |
| **分布式锁** | Scheduler 对每个分片 SETNX 抢锁，多实例互斥 |
| **协程池** | ants 协程池控制并发数，防止资源耗尽 |
| **可观测性 MVP** | 低基数业务指标 + Prometheus 历史存储 + Grafana dashboard/基础告警 |

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.26.2+ |
| Web 框架 | Gin |
| ORM | GORM |
| 数据库 | MySQL 8.0 |
| 缓存与队列 | Redis 7（ZSet + SETNX 锁 + Bitmap） |
| Cron 解析 | robfig/cron/v3 |
| 日志 | zap |
| 配置 | viper |
| 协程池 | panjf2000/ants/v2 |
| 指标 | prometheus/client_golang |
| 前端 | React + TypeScript + Ant Design |
| 容器化 | Docker / Docker Compose |

## 系统架构

```mermaid
graph TB
    subgraph ChronoFlow
        Migrator["Migrator<br/>(批量打点)"]
        Scheduler["Scheduler<br/>(抢锁+分发)"]
        Trigger["Trigger<br/>(轮询ZSet)"]
        Pool["goroutine pool"]
        Executor["Executor<br/>(幂等去重 + HTTP回调 + 更新记录)"]
        Cache["内存缓存层 (Memory Cache)"]
        
        Migrator --> Pool
        Scheduler --> Pool
        Trigger --> Pool
        Pool --> Executor
        Executor --> Cache
    end
    
    subgraph 存储层
        MySQL["MySQL<br/>(持久化)"]
        Redis["Redis<br/>(ZSet队列)"]
        Memory["Memory<br/>(本地缓存)"]
    end
    
    Migrator --> MySQL
    Migrator --> Redis
    Executor --> MySQL
    Executor --> Redis
    Cache --> Memory
```

### 核心流程

```mermaid
graph TD
    A["定时器创建"] --> B["定义存 MySQL<br/>(status=INACTIVE)"]
    B --> B2["定义创建后不可修改<br/>变更需删除并新建"]
    B --> C["定时器激活"]
    C --> D["Migrator 批量预创建定时任务"]
    D --> E["定时任务存 MySQL + Redis ZSet<br/>(按分片分桶)"]
    
    F["每 1s Scheduler 轮询"] --> G["计算当前 time_range"]
    G --> G2["同时处理当前分钟和上一分钟"]
    G2 --> G3["分别获取各分钟的动态分桶数"]
    G3 --> H["遍历每个桶，提交分片处理 worker"]
    H --> I["worker 用 processID_goroutineID token<br/>抢锁 TTL = 70s"]
    I --> I2{"抢到锁?"}
    I2 -->|是| J["运行 Trigger"]
    
    J --> K["在时间片内轮询 Redis ZSet<br/>{time_range}:{bucket}"]
    K --> L["按不重叠时间窗口 ZRANGEBYSCORE<br/>取到期任务"]
    L --> L2["合并已有 MySQL PENDING 记录<br/>(Redis 部分投递补偿)"]
    L2 --> M["从协程池提交 Executor"]
    
    M --> N["Bloom Filter 快速查询"]
    N --> N2{"命中且 MySQL<br/>确认已处理?"}
    N2 -->|是| S
    N2 -->|否| N3{"读取本地定义缓存<br/>状态为 ACTIVE?"}
    N3 -->|否| S
    N3 -->|是| O["MySQL 原子抢占<br/>PENDING → RUNNING"]
    O --> P{"抢占成功?"}
    P -->|否| S["跳过（已被领取或完成）"]
    P -->|是| T["执行 HTTP 回调"]
    T --> U{"执行成功?"}
    U -->|是| W["bloom filter 打点"]
    W --> V["更新 MySQL 记录状态"]
    U -->|否| V
    K --> K2["分片扫描成功后<br/>Lua 校验 token 并保留锁 TTL (130s)"]
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
│   │   └── logger.go                     # 请求日志中间件
│   └── pkg/
│       ├── bloom/filter.go               # Redis 布隆过滤器
│       ├── cron/parser.go                # Cron 表达式解析器
│       ├── memory/cache.go               # 内存定时器缓存
│       ├── metrics/reporter.go           # Prometheus 指标上报
│       ├── pool/pool.go                  # ants 协程池
│       └── redis/queue.go                # Redis ZSet 队列 + SETNX 锁
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
- Node.js 20.19+ 或 22.12+（前端开发，Vite 8 要求）
- MySQL 8.0+
- Redis 7.0+

### 1. 启动基础依赖与监控栈

```bash
make dev-start
# 等价于：docker compose up -d mysql redis prometheus grafana
```

`dev-start` 不启动后端；Prometheus 从容器内通过 `host.docker.internal:8080` 抓取宿主机上运行的 ChronoFlow。

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
make dev-app
# 或
go run cmd/server/main.go
```

后端使用 `config/config.yaml` 连接由 `dev-start` 启动的 MySQL 与 Redis。需要将后端也放入 Compose 时，可运行 `docker compose up -d chronoflow`，该容器使用 `config/config.docker.yaml`。

服务启动后：
- API: http://localhost:8080/api/v1/timers
- Exporter: http://localhost:8080/metrics
- 健康检查: http://localhost:8080/health
- Prometheus 查询界面（Compose）: http://localhost:9090
- Grafana 告警管理（Compose）: http://localhost:3001

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

测试回调服务监听在 `http://localhost:9091`，避免与 Prometheus 的 `9090` 端口冲突，提供以下接口：

| 接口 | 说明 |
|------|------|
| `POST /callback/success` | 立即返回成功 |
| `POST /callback/slow` | 延迟 10 秒后返回 |
| `POST /callback/error` | 返回 500 错误 |
| `GET /stats` | 查看调用统计 |

以下 callback URL 适用于本地运行的后端；后端由 Compose 启动时，将 URL 中的 `localhost` 改为 `host.docker.internal`。

### 步骤二：创建定时器

```bash
# 创建一个每 30 秒触发的定时器
curl -X POST http://localhost:8080/api/v1/timers \
  -H "Content-Type: application/json" \
  -d '{
    "app": "test",
    "name": "测试定时器-30秒",
    "cron_expr": "*/30 * * * * *",
    "callback_url": "http://localhost:9091/callback/success",
    "callback_method": "POST",
    "callback_body": "{\"msg\": \"hello\"}"
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
curl http://localhost:9091/stats
```

### 步骤五：测试慢回调

```bash
# 创建一个指向慢接口的定时器
curl -X POST http://localhost:8080/api/v1/timers \
  -H "Content-Type: application/json" \
  -d '{
    "app": "test",
    "name": "测试超时",
    "cron_expr": "*/30 * * * * *",
    "callback_url": "http://localhost:9091/callback/slow",
    "callback_method": "POST"
  }'
```

### 步骤六：查看指标与历史趋势

```bash
# 应用原始指标
curl http://localhost:8080/metrics

# 关键指标：
# chronoflow_callback_requests_total{result="success|failed"}
# chronoflow_callback_duration_seconds{result="success|failed"}
# chronoflow_records{status="PENDING|RUNNING|SUCCESS|FAILED"}
# chronoflow_pending_overdue_records
# chronoflow_running_stale_records
# chronoflow_redis_queue_items
```

打开管理端的“监控面板”查看 Prometheus 持久化的历史曲线。Grafana 仍负责预置告警规则：服务不可采集、存在超期 `PENDING` 或存在 stale `RUNNING` 持续一分钟时进入告警状态。

### 步骤七：多实例部署测试

```bash
# 启动第二个实例（不同端口）
PORT=8081 go run cmd/server/main.go

# 两个实例会通过分布式锁自动协调，不会重复执行
```

## 配置说明

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `scheduler.migrate_step_minutes` | 60 | Migrator 执行间隔（分钟） |
| `scheduler.step2_duration` | 120 | 二级迁移时间步（秒），本地定义及状态缓存有效期（2 分钟） |
| `scheduler.base_bucket_num` | 1 | 单分钟基础桶数 |
| `scheduler.bucket_num` | 3 | 单分钟动态扩桶上限 |
| `scheduler.tasks_per_bucket` | 100 | 每桶目标投递任务数 |
| `scheduler.bucket_metadata_ttl` | 600 | 时间片结束后队列与分桶元数据保留秒数 |
| `scheduler.scan_interval` | 1 | Scheduler 轮询间隔（秒） |
| `scheduler.lock_expiration` | 70 | 分片锁初始 TTL（秒） |
| `scheduler.success_expiration` | 130 | 分片扫描成功后的锁保留 TTL（秒） |
| `executor.worker_pool_size` | 100 | 协程池大小 |
| `monitoring.collect_interval_seconds` | 10 | 状态 Gauge 指标采集周期（秒） |
| `monitoring.pending_overdue_seconds` | 120 | `PENDING` 记录超期阈值（秒） |
| `monitoring.running_stale_seconds` | 60 | `RUNNING` 记录卡住阈值（秒） |
| `monitoring.prometheus_url` | `http://localhost:9090` | 后端查询历史指标使用的 Prometheus 地址 |

## Redis Key 结构

| 用途 | Key 格式 | 类型 |
|------|----------|------|
| 任务队列 | `chronoflow:timer:{YYYY-MM-DD-HH:mm}:{bucket}` | ZSet |
| 分桶数量 | `chronoflow:bucket:{YYYY-MM-DD-HH:mm}` | String |
| 投递计数 | `chronoflow:task_count:{YYYY-MM-DD-HH:mm}` | String |
| Scheduler 锁 | `chronoflow:scheduler_lock:{time_range}:{bucket}` | String (owner token, SET NX TTL) |
| 布隆过滤器 | `chronoflow:bloom:{date}` | String (bitmap) |

## 定时器状态流转

```
INACTIVE ──激活──→ ACTIVE ──停用──→ INACTIVE
    │                 │
    └────删除────────→ DELETED（终态）
```

定时器定义创建后不可修改；需要变更 Cron 或回调配置时，应删除旧定时器并新建。停用或删除不会清理 Redis ZSet 中已经生成的任务点。与 xTimer 一致，Executor 使用包含状态的节点本地定义缓存判断是否执行，因此停用或删除可能在一个 `step2_duration` 周期内仍有回调；缓存失效后非 `ACTIVE` 任务会被跳过。

## 文档

- [系统架构详解](docs/1-architecture.md)
- [调度器模块详解](docs/3-scheduler.md)
- [分布式锁与幂等](docs/4-distributed-lock.md)
- [API 接口文档](docs/6-api-reference.md)

## 许可证

MIT License
