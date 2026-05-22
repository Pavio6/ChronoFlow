package model

import "fmt"

// timerTransitions 定时器状态转换规则
// ACTIVE -> INACTIVE, DELETED
// INACTIVE -> ACTIVE, DELETED
// DELETED -> (终态，不可转换)
var timerTransitions = map[TimerStatus][]TimerStatus{
	TimerStatusActive:   {TimerStatusInactive, TimerStatusDeleted},
	TimerStatusInactive: {TimerStatusActive, TimerStatusDeleted},
	TimerStatusDeleted:  {},
}

// ValidateTransition 验证状态转换是否合法，不合法返回错误
func ValidateTransition(from, to TimerStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid status transition: %s -> %s", from, to)
	}
	return nil
}

// CanTransition 判断状态转换是否合法
func CanTransition(from, to TimerStatus) bool {
	targets, ok := timerTransitions[from]
	if !ok {
		return false
	}
	for _, s := range targets {
		if s == to {
			return true
		}
	}
	return false
}

// GetNextStatuses 获取当前状态可达的下一状态列表
func GetNextStatuses(from TimerStatus) []TimerStatus {
	targets, ok := timerTransitions[from]
	if !ok {
		return nil
	}
	return targets
}

// IsTerminalStatus 判断是否为终态（不可再转换）
func IsTerminalStatus(status TimerStatus) bool {
	targets, ok := timerTransitions[status]
	return ok && len(targets) == 0
}

// IsActiveStatus 判断是否为激活状态
func IsActiveStatus(status TimerStatus) bool {
	return status == TimerStatusActive
}
