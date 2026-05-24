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
	// BloomPrefix 布隆过滤器 key 前缀
	BloomPrefix = "chronoflow:bloom:"
	// BucketMapPrefix 分桶映射 key 前缀，完整 key: {prefix}{time_range}
	BucketMapPrefix = "chronoflow:bucket:"
)

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

// QueueStats Redis 队列与锁的聚合统计
type QueueStats struct {
	QueueKeys  int64 `json:"queue_keys"`
	QueueItems int64 `json:"queue_items"`
	LockKeys   int64 `json:"lock_keys"`
	BucketKeys int64 `json:"bucket_keys"`
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

// GetDueTasks 获取指定时间片桶中的到期任务（只读不删）
// 使用 ZRANGEBYSCORE 获取 score <= now 的成员，任务保留在 ZSet 中
// 防重复依赖分布式锁（同一分片同一时刻只有一个节点处理）
func (q *RedisQueue) GetDueTasks(ctx context.Context, timeRange string, bucket int, count int64) ([]*TaskTrigger, error) {
	key := buildQueueKey(timeRange, bucket)
	now := time.Now().Unix()

	// ZRANGEBYSCORE 获取到期任务，只读不删
	members, err := q.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%d", now),
		Count: count,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("获取到期任务失败: %w", err)
	}

	if len(members) == 0 {
		return nil, nil
	}

	triggers := make([]*TaskTrigger, 0, len(members))
	for _, memberStr := range members {
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

// QueueExists 判断指定时间片桶的任务队列是否存在
func (q *RedisQueue) QueueExists(ctx context.Context, timeRange string, bucket int) (bool, error) {
	key := buildQueueKey(timeRange, bucket)
	count, err := q.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("检查任务队列是否存在失败: %w", err)
	}
	return count > 0, nil
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

// buildBucketMapKey 构建分桶映射的完整 key
func buildBucketMapKey(timeRange string) string {
	return fmt.Sprintf("%s%s", BucketMapPrefix, timeRange)
}

// SetBucketNum 设置指定时间范围的分桶数量
func (q *RedisQueue) SetBucketNum(ctx context.Context, timeRange string, bucketNum int, expiration time.Duration) error {
	key := buildBucketMapKey(timeRange)
	return q.client.Set(ctx, key, bucketNum, expiration).Err()
}

// GetBucketNum 获取指定时间范围的分桶数量
// 如果不存在，返回默认值 defaultBucketNum
func (q *RedisQueue) GetBucketNum(ctx context.Context, timeRange string, defaultBucketNum int) (int, error) {
	key := buildBucketMapKey(timeRange)
	val, err := q.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return defaultBucketNum, nil
	}
	if err != nil {
		return defaultBucketNum, fmt.Errorf("获取分桶数量失败: %w", err)
	}
	return val, nil
}

// Stats 汇总 ChronoFlow 在 Redis 中的队列、锁和分桶 key 数量
func (q *RedisQueue) Stats(ctx context.Context) (QueueStats, error) {
	var stats QueueStats
	if err := q.scanKeys(ctx, TaskQueuePrefix+"*", func(key string) error {
		stats.QueueKeys++
		count, err := q.client.ZCard(ctx, key).Result()
		if err != nil {
			return fmt.Errorf("统计队列长度失败: %w", err)
		}
		stats.QueueItems += count
		return nil
	}); err != nil {
		return stats, err
	}

	if err := q.scanKeys(ctx, SchedulerLockPrefix+"*", func(key string) error {
		stats.LockKeys++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := q.scanKeys(ctx, BucketMapPrefix+"*", func(key string) error {
		stats.BucketKeys++
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}

func (q *RedisQueue) scanKeys(ctx context.Context, pattern string, visit func(key string) error) error {
	var cursor uint64
	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("扫描 Redis key 失败: %w", err)
		}
		for _, key := range keys {
			if err := visit(key); err != nil {
				return err
			}
		}
		if nextCursor == 0 {
			return nil
		}
		cursor = nextCursor
	}
}
