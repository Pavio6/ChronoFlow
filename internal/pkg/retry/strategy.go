package retry

import (
	"math"
	"time"
)

// Strategy 重试策略类型
type Strategy string

const (
	// StrategyExponential 指数退避策略
	// 间隔公式: initial * multiplier^retryCount，上限为 maxInterval
	StrategyExponential Strategy = "exponential"
	// StrategyFixed 固定间隔策略
	// 每次重试间隔固定为 initialInterval
	StrategyFixed Strategy = "fixed"
)

// Config 重试策略配置
type Config struct {
	// Strategy 重试策略类型
	Strategy Strategy
	// InitialInterval 初始重试间隔
	InitialInterval time.Duration
	// MaxInterval 最大重试间隔（指数退避时的上限）
	MaxInterval time.Duration
	// Multiplier 退避倍数（仅指数退避策略使用）
	Multiplier float64
}

// Calculator 重试时间计算器
type Calculator struct {
	cfg Config
}

// NewCalculator 创建重试时间计算器
func NewCalculator(cfg Config) *Calculator {
	// 设置默认倍数
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	// 设置默认最大间隔
	if cfg.MaxInterval <= 0 {
		cfg.MaxInterval = 30 * time.Minute
	}

	return &Calculator{cfg: cfg}
}

// CalculateNextRetryTime 计算下一次重试时间
// retryCount: 当前已重试次数（从 0 开始）
func (c *Calculator) CalculateNextRetryTime(retryCount int) time.Time {
	var delay time.Duration

	switch c.cfg.Strategy {
	case StrategyExponential:
		delay = c.calculateExponential(retryCount)
	case StrategyFixed:
		delay = c.cfg.InitialInterval
	default:
		// 未知策略回退到固定间隔
		delay = c.cfg.InitialInterval
	}

	return time.Now().Add(delay)
}

// calculateExponential 计算指数退避间隔
// 公式: initial * multiplier^retryCount，上限为 maxInterval
func (c *Calculator) calculateExponential(retryCount int) time.Duration {
	// 计算 exponential backoff: initial * multiplier^retryCount
	backoff := float64(c.cfg.InitialInterval) * math.Pow(c.cfg.Multiplier, float64(retryCount))

	// 转换为 Duration
	delay := time.Duration(backoff)

	// 超过最大间隔时截断
	if delay > c.cfg.MaxInterval {
		delay = c.cfg.MaxInterval
	}

	return delay
}
