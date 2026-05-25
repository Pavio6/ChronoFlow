1. [已处理] Redis 只读任务造成同一时间片重复提交
   Trigger 已改为按不重叠 `[cursor, dueEnd)` 窗口扫描；即使异常重跑产生重复派发，Executor 也必须成功原子抢占 `PENDING -> RUNNING` 后才能回调。
2. [已处理] `PENDING` 记录重复创建
   `timer_records` 已增加唯一约束 `(timer_id, trigger_time)`，幂等查询检查任意状态的既有记录。
3. 任务级 timeout 字段没有真正生效
   模型和前端都有 timeout，但 Executor 的 HTTP client 在初始化时只用全局配置 internal/service/executor.go:50。def.Timeout 没参与单次请求。
   建议二选一：删除任务级 timeout，只保留全局；或按 def.Timeout 给每次请求创建 context/client。
4. 自定义 callback headers 没有实际解析
   internal/service/executor.go:288 只判断非空，然后设置 X-ChronoFlow-Timer-ID，没有 json.Unmarshal 用户配置的 headers。
   这会让前端“请求头 JSON”配置看起来可用，实际无效。建议修成解析 JSON 并写入 request header。
