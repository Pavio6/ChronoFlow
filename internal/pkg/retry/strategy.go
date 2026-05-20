package retry

import (
	"math"
	"time"
)

// Strategy 重试策略类型
type Strategy string

const (
	// StrategyExponential 指数退避策略
	StrategyExponential Strategy = "exponential"
	// StrategyFixed 固定间隔策略
	StrategyFixed Strategy = "fixed"
)

// Config 重试策略配置
type Config struct {
	Strategy        Strategy `json:"strategy"`         // 重试策略
	InitialInterval int      `json:"initial_interval"` // 初始重试间隔（秒）
	MaxInterval     int      `json:"max_interval"`     // 最大重试间隔（秒）
	Multiplier      float64  `json:"multiplier"`       // 指数退避倍数
}

// Calculator 重试时间计算器
type Calculator struct {
	config Config
}

// NewCalculator 创建重试计算器实例
func NewCalculator(config Config) *Calculator {
	return &Calculator{config: config}
}

// CalculateNextRetryTime 计算下次重试时间
// retryCount: 当前已重试次数（从 0 开始）
// 返回下次重试的时间点
func (c *Calculator) CalculateNextRetryTime(retryCount int) time.Time {
	var interval time.Duration

	switch c.config.Strategy {
	case StrategyExponential:
		interval = c.exponentialBackoff(retryCount)
	case StrategyFixed:
		interval = c.fixedInterval()
	default:
		interval = c.exponentialBackoff(retryCount)
	}

	return time.Now().Add(interval)
}

// exponentialBackoff 指数退避算法
// 第一次失败：10 秒后重试
// 第二次失败：30 秒后重试
// 第三次失败：60 秒后重试
func (c *Calculator) exponentialBackoff(retryCount int) time.Duration {
	// 计算指数退避间隔：initial_interval * multiplier^retryCount
	interval := float64(c.config.InitialInterval) * math.Pow(c.config.Multiplier, float64(retryCount))

	// 限制最大间隔
	if interval > float64(c.config.MaxInterval) {
		interval = float64(c.config.MaxInterval)
	}

	return time.Duration(interval) * time.Second
}

// fixedInterval 固定间隔策略
func (c *Calculator) fixedInterval() time.Duration {
	return time.Duration(c.config.InitialInterval) * time.Second
}

// DefaultConfig 返回默认重试配置
func DefaultConfig() Config {
	return Config{
		Strategy:        StrategyExponential,
		InitialInterval: 10,
		MaxInterval:     60,
		Multiplier:      3.0,
	}
}
