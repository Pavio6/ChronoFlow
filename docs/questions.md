1. Redis 任务“只读不删”会导致同一时间片内重复提交 Executor
   internal/pkg/redis/queue.go:118 用 ZRANGEBYSCORE 只读任务，internal/service/trigger.go:103 每 500ms 继续轮询。执行成功前 Bloom 还没打点，同一个 Trigger 可能在同一分钟内多次取
   到同一 ZSet member 并提交执行。
   建议优先改为 Redis 原子取并删除，或在 MySQL 用条件更新 PENDING -> RUNNING 做抢占，更新失败则跳过。
2. 幂等检查逻辑对 PENDING 不防重复创建
   internal/repository/timer_record_repo.go:176 只统计 status != PENDING。Migrator/Activate 遇到已有 PENDING 时会认为不存在，可能重复插入相同触发记录。
   建议加唯一索引 (timer_id, trigger_time)，查询也应检查任何状态的记录是否存在；执行时再用状态流转控制是否可执行。
3. 任务级 timeout 字段没有真正生效
   模型和前端都有 timeout，但 Executor 的 HTTP client 在初始化时只用全局配置 internal/service/executor.go:50。def.Timeout 没参与单次请求。
   建议二选一：删除任务级 timeout，只保留全局；或按 def.Timeout 给每次请求创建 context/client。
4. 自定义 callback headers 没有实际解析
   internal/service/executor.go:288 只判断非空，然后设置 X-ChronoFlow-Timer-ID，没有 json.Unmarshal 用户配置的 headers。
   这会让前端“请求头 JSON”配置看起来可用，实际无效。建议修成解析 JSON 并写入 request header。
