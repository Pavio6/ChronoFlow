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

// ValidateTimerTransition 验证 Timer 状态转换是否合法
func ValidateTimerTransition(from, to TimerStatus) error {
	targets, ok := timerTransitions[from]
	if !ok {
		return fmt.Errorf("invalid status transition: %s -> %s", from, to)
	}
	for _, target := range targets {
		if target == to {
			return nil
		}
	}
	return fmt.Errorf("invalid status transition: %s -> %s", from, to)
}
