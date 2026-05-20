package cron

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// CronParser Cron 表达式解析器
type CronParser struct {
	parser cron.Parser
}

// NewCronParser 创建 Cron 解析器实例
func NewCronParser() *CronParser {
	return &CronParser{
		parser: cron.NewParser(
			cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		),
	}
}

// Parse 解析 Cron 表达式
func (p *CronParser) Parse(expr string) (cron.Schedule, error) {
	schedule, err := p.parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression '%s': %w", expr, err)
	}
	return schedule, nil
}

// NextTriggerTime 计算下一次触发时间
// expr: Cron 表达式
// from: 计算起点时间
// 返回下一次触发时间
func (p *CronParser) NextTriggerTime(expr string, from time.Time) (time.Time, error) {
	schedule, err := p.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}

	next := schedule.Next(from)
	return next, nil
}

// NextNTriggerTimes 计算接下来 N 次触发时间
func (p *CronParser) NextNTriggerTimes(expr string, from time.Time, n int) ([]time.Time, error) {
	schedule, err := p.Parse(expr)
	if err != nil {
		return nil, err
	}

	times := make([]time.Time, 0, n)
	current := from
	for i := 0; i < n; i++ {
		current = schedule.Next(current)
		times = append(times, current)
	}

	return times, nil
}

// ValidateCronExpr 验证 Cron 表达式是否有效
func ValidateCronExpr(expr string) error {
	parser := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	_, err := parser.Parse(expr)
	return err
}
