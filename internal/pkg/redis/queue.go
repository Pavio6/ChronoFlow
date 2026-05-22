package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key 前缀常量
const (
	// TaskQueuePrefix 任务队列 ZSet key 前缀，完整 key: {prefix}{time_range}:{bucket}
	TaskQueuePrefix = "chronoflow:timer:"
	// SchedulerLockPrefix 调度器分布式锁前缀
	SchedulerLockPrefix = "chronoflow:scheduler_lock:"
	// IdempotentPrefix 幂等性 key 前缀
	IdempotentPrefix = "chronoflow:idempotent:"
	// BloomPrefix 布隆过滤器 key 前缀
	BloomPrefix = "chronoflow:bloom:"
)

// popDueTasksLua 原子弹出到期任务的 Lua 脚本
// 1. ZRANGEBYSCORE 获取 score <= now 的成员
// 2. 逐个 ZREM 删除
// 3. 返回被弹出的成员列表
var popDueTasksLua = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local count = tonumber(ARGV[2])
local members = redis.call('ZRANGEBYSCORE', key, '-inf', now, 'LIMIT', 0, count)
if #members > 0 then
    for i, member in ipairs(members) do
        redis.call('ZREM', key, member)
    end
end
return members
`)

// TaskTrigger 任务触发信息
type TaskTrigger struct {
	// TimerID 定时器 ID
	TimerID int64
	// TriggerTime 触发时间
	TriggerTime time.Time
}

// RedisQueue 基于 Redis ZSet 的时间片任务队列
type RedisQueue struct {
	client *redis.Client
}

// InitRedis 初始化 Redis 客户端连接
func InitRedis(addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 连接健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis 连接失败: %w", err)
	}

	return client, nil
}

// NewRedisQueue 创建任务队列实例
func NewRedisQueue(client *redis.Client) *RedisQueue {
	return &RedisQueue{client: client}
}

// buildQueueKey 构建队列 ZSet 的完整 key
// 格式: {TaskQueuePrefix}{time_range}:{bucket}
func buildQueueKey(timeRange string, bucket int) string {
	return fmt.Sprintf("%s%s:%d", TaskQueuePrefix, timeRange, bucket)
}

// buildLockKey 构建分布式锁的完整 key
func buildLockKey(timeRange string, bucket int) string {
	return fmt.Sprintf("%s%s:%d", SchedulerLockPrefix, timeRange, bucket)
}

// PushTask 推送单个任务到指定时间片桶
// 使用 ZAdd 以 triggerTime 的 Unix 时间戳作为 score
func (q *RedisQueue) PushTask(ctx context.Context, timeRange string, bucket int, trigger *TaskTrigger) error {
	key := buildQueueKey(timeRange, bucket)
	member := fmt.Sprintf("%d:%d", trigger.TimerID, trigger.TriggerTime.UnixMilli())
	score := float64(trigger.TriggerTime.Unix())

	return q.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// BatchPushTasks 批量推送任务到指定时间片桶（Pipeline 模式）
func (q *RedisQueue) BatchPushTasks(ctx context.Context, timeRange string, bucket int, triggers []*TaskTrigger) error {
	if len(triggers) == 0 {
		return nil
	}

	key := buildQueueKey(timeRange, bucket)
	pipe := q.client.Pipeline()

	for _, trigger := range triggers {
		member := fmt.Sprintf("%d:%d", trigger.TimerID, trigger.TriggerTime.UnixMilli())
		score := float64(trigger.TriggerTime.Unix())
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  score,
			Member: member,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("批量推送任务失败: %w", err)
	}

	return nil
}

// PopDueTasks 原子弹出指定时间片桶中的到期任务
// 通过 Lua 脚本保证 ZRANGEBYSCORE + ZREM 的原子性
func (q *RedisQueue) PopDueTasks(ctx context.Context, timeRange string, bucket int, count int64) ([]*TaskTrigger, error) {
	key := buildQueueKey(timeRange, bucket)
	now := time.Now().Unix()

	// 执行 Lua 脚本，原子获取并删除到期任务
	result, err := popDueTasksLua.Run(ctx, q.client, []string{key}, now, count).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("弹出到期任务失败: %w", err)
	}

	// 解析返回的成员列表
	members, ok := result.([]interface{})
	if !ok || len(members) == 0 {
		return nil, nil
	}

	triggers := make([]*TaskTrigger, 0, len(members))
	for _, m := range members {
		memberStr, ok := m.(string)
		if !ok {
			continue
		}

		// 解析 member 格式: {timerID}:{triggerTimeMs}
		var timerID int64
		var triggerTimeMs int64
		if _, err := fmt.Sscanf(memberStr, "%d:%d", &timerID, &triggerTimeMs); err != nil {
			continue
		}

		triggers = append(triggers, &TaskTrigger{
			TimerID:     timerID,
			TriggerTime: time.UnixMilli(triggerTimeMs),
		})
	}

	return triggers, nil
}

// AcquireSchedulerLock 获取调度器分布式锁（SETNX）
func (q *RedisQueue) AcquireSchedulerLock(ctx context.Context, timeRange string, bucket int, expiration time.Duration) (bool, error) {
	key := buildLockKey(timeRange, bucket)
	ok, err := q.client.SetNX(ctx, key, "1", expiration).Result()
	if err != nil {
		return false, fmt.Errorf("获取调度器锁失败: %w", err)
	}
	return ok, nil
}

// ReleaseSchedulerLock 释放调度器分布式锁
func (q *RedisQueue) ReleaseSchedulerLock(ctx context.Context, timeRange string, bucket int) error {
	key := buildLockKey(timeRange, bucket)
	if err := q.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("释放调度器锁失败: %w", err)
	}
	return nil
}

// ExtendSchedulerLock 续期调度器分布式锁
func (q *RedisQueue) ExtendSchedulerLock(ctx context.Context, timeRange string, bucket int, expiration time.Duration) error {
	key := buildLockKey(timeRange, bucket)
	if err := q.client.Expire(ctx, key, expiration).Err(); err != nil {
		return fmt.Errorf("续期调度器锁失败: %w", err)
	}
	return nil
}

// buildIdempotentKey 构建幂等性检查的完整 key
func buildIdempotentKey(timerID int64, triggerTime time.Time) string {
	return fmt.Sprintf("%s%d:%d", IdempotentPrefix, timerID, triggerTime.UnixMilli())
}

// SetIdempotentKey 设置幂等性 key（SETNX）
// 用于防止同一任务在同一触发时间被重复执行
func (q *RedisQueue) SetIdempotentKey(ctx context.Context, timerID int64, triggerTime time.Time, expiration time.Duration) (bool, error) {
	key := buildIdempotentKey(timerID, triggerTime)
	ok, err := q.client.SetNX(ctx, key, "1", expiration).Result()
	if err != nil {
		return false, fmt.Errorf("设置幂等 key 失败: %w", err)
	}
	return ok, nil
}

// IsIdempotent 检查幂等性 key 是否已存在
func (q *RedisQueue) IsIdempotent(ctx context.Context, timerID int64, triggerTime time.Time) (bool, error) {
	key := buildIdempotentKey(timerID, triggerTime)
	exists, err := q.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("检查幂等 key 失败: %w", err)
	}
	return exists > 0, nil
}
