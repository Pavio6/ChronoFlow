# ChronoFlow E2E 测试

该模块启动隔离的 MySQL 与 Redis，并运行真实的 API、Scheduler、Dispatcher、Worker 进程。它验证跨进程完整链路，而不是使用 mock：

```text
API 创建并激活 Timer
  → Scheduler 创建 Execution + Outbox
  → Dispatcher 写入 Redis Stream
  → Worker 消费消息并执行 HTTP Callback
  → MySQL 持久化执行结果并 XACK
```

## 运行

要求：Docker Desktop 正在运行，主机端口 `3307`、`6380`、`18080`–`18083` 可用。

```bash
make e2e-test
```

该命令会启动 E2E 专用依赖、执行测试，并保留容器以便排查失败日志。完成后清理：

```bash
make e2e-down
```

E2E 使用数据库 `chronoflow_e2e`、Redis 端口 `6380`，与本地开发默认的 MySQL `3306`、Redis `6379` 隔离。每次测试运行都会重建 E2E 数据库并执行项目迁移。

## 覆盖场景

- 创建并激活 Timer 后，完整链路最终调用一次 HTTP Callback，Execution 变为 `SUCCESS`；
- 首次回调返回 `500` 后，Worker 创建重试 Outbox，任务经 Dispatcher 再次投递，Execution 在第二次尝试后变为 `SUCCESS`。

测试源码使用 `e2e` build tag，因此日常的 `go test ./...` 不会启动 Docker 或执行 E2E：

```bash
CHRONOFLOW_E2E=1 go test -tags=e2e -count=1 -v ./e2e
```
