package cron

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// CronParser Cron 表达式解析器
// 包装 robfig/cron/v3，支持 6 字段格式（秒 分 时 日 月 周）
type CronParser struct {
	parser cron.Parser
}

// NewCronParser 创建 Cron 解析器实例
// 使用 6 字段格式：秒 分 时 日 月 周
func NewCronParser() *CronParser {
	return &CronParser{
		parser: cron.NewParser(
			cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		),
	}
}

// Parse 解析 Cron 表达式，返回调度器
func (p *CronParser) Parse(expr string) (cron.Schedule, error) {
	schedule, err := p.parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("解析 cron 表达式失败 [%s]: %w", expr, err)
	}
	return schedule, nil
}

// NextTriggerTime 计算指定时间之后的下一次触发时间
func (p *CronParser) NextTriggerTime(expr string, from time.Time) (time.Time, error) {
	schedule, err := p.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(from), nil
}

// NextNTriggerTimes 计算指定时间之后的 N 次触发时间
func (p *CronParser) NextNTriggerTimes(expr string, from time.Time, n int) ([]time.Time, error) {
	if n <= 0 {
		return nil, fmt.Errorf("n 必须大于 0，当前值: %d", n)
	}

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

// NextTriggerTimesBefore 计算 from 到 end 之间的所有触发时间
func (p *CronParser) NextTriggerTimesBefore(expr string, from time.Time, end time.Time) ([]time.Time, error) {
	schedule, err := p.Parse(expr)
	if err != nil {
		return nil, err
	}

	var times []time.Time
	current := from
	for {
		next := schedule.Next(current)
		if next.After(end) {
			break
		}
		times = append(times, next)
		current = next
	}

	return times, nil
}

// ValidateCronExpr 验证 Cron 表达式是否合法
func (p *CronParser) ValidateCronExpr(expr string) error {
	_, err := p.Parse(expr)
	return err
}
