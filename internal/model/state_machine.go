package model

import (
	"fmt"
)

// StateTransition 状态转换定义
type StateTransition struct {
	From TaskStatus
	To   TaskStatus
}

// 状态转换规则表
// 定义了允许的状态转换，保证任务状态流转的合法性
var allowedTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusINIT:     {TaskStatusENABLED, TaskStatusDELETED},
	TaskStatusENABLED:  {TaskStatusDISABLED, TaskStatusRUNNING, TaskStatusDELETED},
	TaskStatusDISABLED: {TaskStatusENABLED, TaskStatusDELETED},
	TaskStatusRUNNING:  {TaskStatusSUCCESS, TaskStatusFAILED, TaskStatusTIMEOUT},
	TaskStatusSUCCESS:  {TaskStatusENABLED, TaskStatusRUNNING},
	TaskStatusFAILED:   {TaskStatusENABLED, TaskStatusRUNNING},
}

// ValidateTransition 验证状态转换是否合法
// from: 当前状态
// to: 目标状态
// 返回错误表示不允许的状态转换
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
