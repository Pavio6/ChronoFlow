# 状态机设计

## 概述

状态机是 ChronoFlow 任务管理的核心机制，保证任务状态流转的合法性和一致性。

## 任务状态

### 状态定义

```go
type TaskStatus string

const (
    TaskStatusINIT     TaskStatus = "INIT"     // 初始化
    TaskStatusENABLED  TaskStatus = "ENABLED"  // 启用
    TaskStatusDISABLED TaskStatus = "DISABLED" // 禁用
    TaskStatusRUNNING  TaskStatus = "RUNNING"  // 运行中
    TaskStatusSUCCESS  TaskStatus = "SUCCESS"  // 执行成功
    TaskStatusFAILED   TaskStatus = "FAILED"   // 执行失败
    TaskStatusDELETED  TaskStatus = "DELETED"  // 已删除
)
```

### 状态说明

| 状态 | 说明 | 可调度 |
|------|------|--------|
| INIT | 初始化状态，任务刚创建 | 否 |
| ENABLED | 启用状态，等待调度 | 是 |
| DISABLED | 禁用状态，暂停调度 | 否 |
| RUNNING | 运行中，正在执行 | 否 |
| SUCCESS | 执行成功 | 否 |
| FAILED | 执行失败 | 否 |
| DELETED | 已删除 | 否 |

## 状态转换规则

### 转换规则表

```go
var allowedTransitions = map[TaskStatus][]TaskStatus{
    TaskStatusINIT:     {TaskStatusENABLED, TaskStatusDELETED},
    TaskStatusENABLED:  {TaskStatusDISABLED, TaskStatusRUNNING, TaskStatusDELETED},
    TaskStatusDISABLED: {TaskStatusENABLED, TaskStatusDELETED},
    TaskStatusRUNNING:  {TaskStatusSUCCESS, TaskStatusFAILED, TaskStatusTIMEOUT},
    TaskStatusSUCCESS:  {TaskStatusENABLED, TaskStatusRUNNING},
    TaskStatusFAILED:   {TaskStatusENABLED, TaskStatusRUNNING},
}
```

### 状态转换图

```
                    ┌─────────────────────────────────────┐
                    │                                     │
                    ▼                                     │
              ┌─────────┐                                 │
              │  INIT   │                                 │
              └────┬────┘                                 │
                   │                                      │
       ┌───────────┼───────────┐                          │
       │           │           │                          │
       ▼           ▼           ▼                          │
 ┌─────────┐ ┌─────────┐ ┌─────────┐                     │
 │ ENABLED │ │DISABLED │ │ DELETED │◀────────────────────┐
 └────┬────┘ └────┬────┘ └─────────┘                    │
      │           │                                      │
      │     ┌─────┘                                      │
      │     │                                            │
      ▼     ▼                                            │
 ┌─────────┐                                             │
 │ RUNNING │                                             │
 └────┬────┘                                             │
      │                                                  │
      ├──────────────┬──────────────┐                    │
      │              │              │                    │
      ▼              ▼              ▼                    │
 ┌─────────┐  ┌─────────┐  ┌─────────┐                  │
 │ SUCCESS │  │ FAILED  │  │ TIMEOUT │                  │
 └────┬────┘  └────┬────┘  └────┬────┘                  │
      │           │              │                       │
      │           │              ▼                       │
      │           │        ┌─────────┐                  │
      │           │        │RETRYING │                  │
      │           │        └────┬────┘                  │
      │           │              │                       │
      └───────────┴──────────────┘                       │
              │                                          │
              └──────────────────────────────────────────┘
```

## 代码实现

### 状态转换验证

```go
// ValidateTransition 验证状态转换是否合法
func ValidateTransition(from, to TaskStatus) error {
    // 检查当前状态是否允许转换
    targets, exists := allowedTransitions[from]
    if !exists {
        return fmt.Errorf("invalid source status: %s", from)
    }

    // 检查目标状态是否在允许列表中
    for _, target := range targets {
        if target == to {
            return nil
        }
    }

    return fmt.Errorf("transition from %s to %s is not allowed", from, to)
}
```

### 状态检查

```go
// CanTransition 检查状态转换是否允许
func CanTransition(from, to TaskStatus) bool {
    return ValidateTransition(from, to) == nil
}

// GetNextStatuses 获取当前状态可转换的目标状态列表
func GetNextStatuses(current TaskStatus) []TaskStatus {
    if targets, exists := allowedTransitions[current]; exists {
        return targets
    }
    return nil
}

// IsTerminalStatus 判断是否为终态
func IsTerminalStatus(status TaskStatus) bool {
    return status == TaskStatusDELETED
}

// IsActiveStatus 判断是否为活跃状态（可被调度）
func IsActiveStatus(status TaskStatus) bool {
    return status == TaskStatusENABLED
}
```

### 任务状态检查

```go
// IsRunnable 检查任务是否可运行
func (t *Task) IsRunnable() bool {
    return t.Status == TaskStatusENABLED
}
```

## 状态转换场景

### 1. 任务创建

```
INIT → ENABLED
```

**场景**：用户创建新任务

**代码**：
```go
task.Status = model.TaskStatusINIT
// 创建后自动启用
task.Status = model.TaskStatusENABLED
```

### 2. 任务禁用

```
ENABLED → DISABLED
```

**场景**：用户禁用任务

**代码**：
```go
if err := model.ValidateTransition(task.Status, model.TaskStatusDISABLED); err != nil {
    return err
}
task.Status = model.TaskStatusDISABLED
```

### 3. 任务启用

```
DISABLED → ENABLED
```

**场景**：用户启用任务

**代码**：
```go
if err := model.ValidateTransition(task.Status, model.TaskStatusENABLED); err != nil {
    return err
}
task.Status = model.TaskStatusENABLED
```

### 4. 任务执行

```
ENABLED → RUNNING
```

**场景**：任务被触发执行

**代码**：
```go
if err := e.taskRepo.UpdateStatus(task.ID, model.TaskStatusRUNNING); err != nil {
    logger.Error("failed to update task status",
        zap.Int64("task_id", task.ID),
        zap.Error(err),
    )
}
```

### 5. 执行成功

```
RUNNING → SUCCESS
```

**场景**：任务执行成功

**代码**：
```go
if err := e.taskRepo.UpdateStatus(task.ID, model.TaskStatusSUCCESS); err != nil {
    logger.Error("failed to update task status",
        zap.Int64("task_id", task.ID),
        zap.Error(err),
    )
}
```

### 6. 执行失败

```
RUNNING → FAILED
```

**场景**：任务执行失败

**代码**：
```go
execution.Status = model.ExecutionStatusFAILED
e.taskRepo.UpdateStatus(task.ID, model.TaskStatusFAILED)
```

### 7. 任务删除

```
任意状态 → DELETED
```

**场景**：用户删除任务

**代码**：
```go
if err := model.ValidateTransition(task.Status, model.TaskStatusDELETED); err != nil {
    return err
}
task.Status = model.TaskStatusDELETED
```

## 执行记录状态

### 状态定义

```go
type ExecutionStatus string

const (
    ExecutionStatusPENDING  ExecutionStatus = "PENDING"   // 待执行
    ExecutionStatusRUNNING  ExecutionStatus = "RUNNING"   // 执行中
    ExecutionStatusSUCCESS  ExecutionStatus = "SUCCESS"   // 执行成功
    ExecutionStatusFAILED   ExecutionStatus = "FAILED"    // 执行失败
    ExecutionStatusRETRYING ExecutionStatus = "RETRYING"  // 重试中
    ExecutionStatusTIMEOUT  ExecutionStatus = "TIMEOUT"   // 超时
)
```

### 状态流转

```
PENDING → RUNNING → SUCCESS
                  → FAILED
                  → TIMEOUT → RETRYING → RUNNING
```

## 并发安全

### 数据库事务

状态转换使用数据库事务保证原子性：

```go
func (r *taskRepository) UpdateStatus(id int64, status model.TaskStatus) error {
    return r.db.Model(&model.Task{}).
        Where("id = ?", id).
        Update("status", status).Error
}
```

### 乐观锁

使用版本号实现乐观锁：

```go
type Task struct {
    // ...
    Version int `json:"version" gorm:"default:1;comment:版本号"`
    // ...
}
```

## 监控与告警

### 状态监控

- 监控各状态任务数量
- 监控状态转换频率
- 监控异常状态转换

### 告警规则

- 任务长时间处于 RUNNING 状态
- 任务频繁失败
- 状态转换异常

## 最佳实践

### 1. 状态设计

- 状态数量适中，不宜过多
- 状态含义清晰明确
- 状态转换规则简单

### 2. 状态转换

- 使用验证函数保证合法性
- 记录状态转换日志
- 处理异常状态转换

### 3. 并发控制

- 使用数据库事务
- 使用乐观锁
- 使用分布式锁

### 4. 监控告警

- 监控状态分布
- 监控转换频率
- 告警异常状态
