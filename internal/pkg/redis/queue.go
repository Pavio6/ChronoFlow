package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronoflow/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// TaskQueueKey 任务队列的 Redis ZSet 键名
	TaskQueueKey = "chronoflow:task_queue"
	// TaskLockPrefix 任务锁的 Redis 键前缀
	TaskLockPrefix = "chronoflow:task_lock:"
	// IdempotentPrefix 幂等键前缀
	IdempotentPrefix = "chronoflow:idempotent:"
)

// TaskTrigger 任务触发信息
type TaskTrigger struct {
	TaskID      int64     `json:"task_id"`
	TriggerTime time.Time `json:"trigger_time"`
}

// RedisQueue Redis 任务队列
type RedisQueue struct {
	client *redis.Client
}

// NewRedisQueue 创建 Redis 队列实例
func NewRedisQueue(client *redis.Client) *RedisQueue {
	return &RedisQueue{client: client}
}

// InitRedis 初始化 Redis 连接
func InitRedis(addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect redis: %w", err)
	}

	logger.Info("redis connected successfully", zap.String("addr", addr))
	return client, nil
}

// PushTask 将任务推入调度队列
// 使用 ZSet 存储，score 为触发时间的时间戳
func (q *RedisQueue) PushTask(ctx context.Context, trigger *TaskTrigger) error {
	data, err := json.Marshal(trigger)
	if err != nil {
		return fmt.Errorf("failed to marshal task trigger: %w", err)
	}

	// 使用触发时间作为 score
	score := float64(trigger.TriggerTime.Unix())
	return q.client.ZAdd(ctx, TaskQueueKey, redis.Z{
		Score:  score,
		Member: string(data),
	}).Err()
}

// PopDueTasks 取出到期的任务（原子操作）
// 使用 Lua 脚本保证取任务和删除任务的原子性
func (q *RedisQueue) PopDueTasks(ctx context.Context, count int64) ([]*TaskTrigger, error) {
	now := time.Now().Unix()

	// Lua 脚本：原子性地获取并删除到期任务
	script := redis.NewScript(`
		local tasks = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
		if #tasks > 0 then
			redis.call('ZREM', KEYS[1], unpack(tasks))
		end
		return tasks
	`)

	result, err := script.Run(ctx, q.client, []string{TaskQueueKey}, now, count).StringSlice()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to pop due tasks: %w", err)
	}

	var triggers []*TaskTrigger
	for _, data := range result {
		var trigger TaskTrigger
		if err := json.Unmarshal([]byte(data), &trigger); err != nil {
			logger.Error("failed to unmarshal task trigger",
				zap.String("data", data),
				zap.Error(err),
			)
			continue
		}
		triggers = append(triggers, &trigger)
	}

	return triggers, nil
}

// RemoveTask 从队列中移除任务
func (q *RedisQueue) RemoveTask(ctx context.Context, trigger *TaskTrigger) error {
	data, err := json.Marshal(trigger)
	if err != nil {
		return fmt.Errorf("failed to marshal task trigger: %w", err)
	}

	return q.client.ZRem(ctx, TaskQueueKey, string(data)).Err()
}

// AcquireTaskLock 获取任务执行锁
// 使用 SETNX 实现分布式锁，防止同一任务被多个实例同时执行
func (q *RedisQueue) AcquireTaskLock(ctx context.Context, taskID int64, triggerTime time.Time, expiration time.Duration) (bool, error) {
	key := fmt.Sprintf("%s%d:%s", TaskLockPrefix, taskID, triggerTime.Format(time.RFC3339))
	result, err := q.client.SetNX(ctx, key, "1", expiration).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire task lock: %w", err)
	}
	return result, nil
}

// ReleaseTaskLock 释放任务执行锁
func (q *RedisQueue) ReleaseTaskLock(ctx context.Context, taskID int64, triggerTime time.Time) error {
	key := fmt.Sprintf("%s%d:%s", TaskLockPrefix, taskID, triggerTime.Format(time.RFC3339))
	return q.client.Del(ctx, key).Err()
}

// SetIdempotentKey 设置幂等键
// 用于防止同一任务在同一触发时间被重复执行
func (q *RedisQueue) SetIdempotentKey(ctx context.Context, taskID int64, triggerTime time.Time, expiration time.Duration) (bool, error) {
	key := fmt.Sprintf("%s%d:%s", IdempotentPrefix, taskID, triggerTime.Format(time.RFC3339))
	result, err := q.client.SetNX(ctx, key, "1", expiration).Result()
	if err != nil {
		return false, fmt.Errorf("failed to set idempotent key: %w", err)
	}
	return result, nil
}

// IsIdempotent 检查是否已执行过
func (q *RedisQueue) IsIdempotent(ctx context.Context, taskID int64, triggerTime time.Time) (bool, error) {
	key := fmt.Sprintf("%s%d:%s", IdempotentPrefix, taskID, triggerTime.Format(time.RFC3339))
	exists, err := q.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check idempotent key: %w", err)
	}
	return exists > 0, nil
}

// QueueSize 获取队列中的任务数量
func (q *RedisQueue) QueueSize(ctx context.Context) (int64, error) {
	return q.client.ZCard(ctx, TaskQueueKey).Result()
}
